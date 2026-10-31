package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

type server struct {
	store  *Store
	mu     sync.RWMutex
	active map[string]*Engine
	maxRun int
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	port := env("PORT", "8080")
	store, err := NewStore(env("DATA_DIR", "./data"))
	if err != nil {
		log.Fatalf("data dir: %v", err)
	}
	srv := &server{store: store, active: map[string]*Engine{}, maxRun: 3}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "time": time.Now().UTC()})
	})
	mux.HandleFunc("POST /api/import", srv.handleImport)
	mux.HandleFunc("POST /api/fetch-spec", srv.handleFetchSpec)
	mux.HandleFunc("GET /api/runs", srv.handleList)
	mux.HandleFunc("POST /api/runs", srv.handleStart)
	mux.HandleFunc("GET /api/runs/{id}", srv.handleGet)
	mux.HandleFunc("DELETE /api/runs/{id}", srv.handleDelete)
	mux.HandleFunc("POST /api/runs/{id}/stop", srv.handleStop)
	mux.HandleFunc("GET /api/runs/{id}/stream", srv.handleStream)
	mux.HandleFunc("GET /api/runs/{id}/report/{ext}", srv.handleReport)
	registerDemo(mux, port)

	// Optional: serve the built UI when STATIC_DIR is set (single-container mode).
	if dir := os.Getenv("STATIC_DIR"); dir != "" {
		fs := http.FileServer(http.Dir(dir))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if _, err := os.Stat(dir + r.URL.Path); err != nil {
				r.URL.Path = "/"
			}
			fs.ServeHTTP(w, r)
		})
	}

	hs := &http.Server{Addr: ":" + port, Handler: cors(logReq(mux)), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("LoadForge API listening on :%s", port)
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down: stopping active runs")
	srv.mu.RLock()
	for _, e := range srv.active {
		e.Stop()
	}
	srv.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = hs.Shutdown(ctx)
}

func cors(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func logReq(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasSuffix(r.URL.Path, "/stream") {
			log.Printf("%s %s", r.Method, r.URL.Path)
		}
		h.ServeHTTP(w, r)
	})
}

func apiErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *server) handleImport(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		apiErr(w, 400, err.Error())
		return
	}
	col, err := ImportDocument(raw)
	if err != nil {
		apiErr(w, 422, err.Error())
		return
	}
	writeJSON(w, 200, col)
}

func (s *server) handleFetchSpec(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !(strings.HasPrefix(req.URL, "http://") || strings.HasPrefix(req.URL, "https://")) {
		apiErr(w, 400, "provide an http(s) URL")
		return
	}
	c := &http.Client{Timeout: 20 * time.Second}
	resp, err := c.Get(req.URL)
	if err != nil {
		apiErr(w, 502, "could not fetch spec: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		apiErr(w, 502, fmt.Sprintf("spec URL returned HTTP %d", resp.StatusCode))
		return
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	writeJSON(w, 200, map[string]string{"content": string(b)})
}

func (s *server) handleStart(w http.ResponseWriter, r *http.Request) {
	var cfg TestConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&cfg); err != nil {
		apiErr(w, 400, "invalid config: "+err.Error())
		return
	}
	if err := validateConfig(&cfg); err != nil {
		apiErr(w, 422, err.Error())
		return
	}
	s.mu.Lock()
	if len(s.active) >= s.maxRun {
		s.mu.Unlock()
		apiErr(w, 429, fmt.Sprintf("%d tests are already running — stop one first", s.maxRun))
		return
	}
	var eng *Engine
	eng = NewEngine(cfg, func(run *Run) {
		if err := s.store.Save(run); err != nil {
			log.Printf("save run %s: %v", run.ID, err)
		}
		s.mu.Lock()
		delete(s.active, run.ID)
		s.mu.Unlock()
		log.Printf("run %s %s: %d requests, %.2f%% errors, p95 %.1fms", run.ID, run.Status, run.Summary.Requests, run.Summary.ErrorRate, run.Summary.Latency.P95)
	})
	s.active[eng.run.ID] = eng
	s.mu.Unlock()
	eng.Start()
	log.Printf("run %s started: %s (%s)", eng.run.ID, cfg.Name, cfg.Type)
	writeJSON(w, 201, map[string]string{"id": eng.run.ID})
}

func (s *server) handleList(w http.ResponseWriter, r *http.Request) {
	list := s.store.List()
	s.mu.RLock()
	var live []RunListItem
	for _, e := range s.active {
		snap := e.Snapshot()
		live = append(live, listItem(&snap))
	}
	s.mu.RUnlock()
	writeJSON(w, 200, append(live, list...))
}

func (s *server) getRun(id string) (*Run, bool) {
	s.mu.RLock()
	e, ok := s.active[id]
	s.mu.RUnlock()
	if ok {
		snap := e.Snapshot()
		return &snap, true
	}
	run, err := s.store.Get(id)
	if err != nil {
		return nil, false
	}
	return run, true
}

func (s *server) handleGet(w http.ResponseWriter, r *http.Request) {
	run, ok := s.getRun(r.PathValue("id"))
	if !ok {
		apiErr(w, 404, "run not found")
		return
	}
	writeJSON(w, 200, run)
}

func (s *server) handleStop(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	e, ok := s.active[r.PathValue("id")]
	s.mu.RUnlock()
	if !ok {
		apiErr(w, 404, "run is not active")
		return
	}
	e.Stop()
	writeJSON(w, 202, map[string]bool{"stopping": true})
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.RLock()
	_, running := s.active[id]
	s.mu.RUnlock()
	if running {
		apiErr(w, 409, "stop the run before deleting it")
		return
	}
	if err := s.store.Delete(id); err != nil {
		apiErr(w, 404, "run not found")
		return
	}
	w.WriteHeader(204)
}

func (s *server) handleStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	fl, ok := w.(http.Flusher)
	if !ok {
		apiErr(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	send := func(event string, data []byte) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		fl.Flush()
	}
	s.mu.RLock()
	e, active := s.active[id]
	s.mu.RUnlock()
	if !active {
		run, ok := s.getRun(id)
		if !ok {
			apiErr(w, 404, "run not found")
			return
		}
		b, _ := json.Marshal(run)
		send("done", b)
		return
	}
	ch, unsub := e.Subscribe()
	defer unsub()
	snap := e.Snapshot()
	b, _ := json.Marshal(snap)
	send("init", b)
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case msg, ok := <-ch:
			if !ok {
				run, found := s.getRun(id)
				if !found {
					return
				}
				b, _ := json.Marshal(run)
				send("done", b)
				return
			}
			send("tick", msg)
		}
	}
}

func (s *server) handleReport(w http.ResponseWriter, r *http.Request) {
	run, ok := s.getRun(r.PathValue("id"))
	if !ok {
		apiErr(w, 404, "run not found")
		return
	}
	ext := r.PathValue("ext")
	var body []byte
	switch ext {
	case "html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		body = RenderHTML(run)
	case "json":
		w.Header().Set("Content-Type", "application/json")
		body, _ = json.MarshalIndent(run, "", "  ")
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		body = RenderCSV(run)
	default:
		apiErr(w, 404, "unknown format (use html, json or csv)")
		return
	}
	if r.URL.Query().Get("download") != "" {
		name := safeName(run.Config.Name) + "-" + run.ID + "." + ext
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}
	_, _ = w.Write(body)
}

func safeName(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		case c == ' ' || c == '-' || c == '_':
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "loadtest"
	}
	return b.String()
}
