package main

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// TryRequest sends a single request so users can verify URL, token, headers and
// payload before starting a load test. It runs from the backend container, i.e.
// exactly where the load will come from.
type TryRequest struct {
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	Variables   map[string]string `json:"variables"`
	Body        string            `json:"body"`
	TimeoutMs   int               `json:"timeoutMs"`
	InsecureTLS bool              `json:"insecureTls"`
}

type TryResponse struct {
	OK          bool              `json:"ok"`
	Status      int               `json:"status"`
	StatusText  string            `json:"statusText"`
	DurationMs  float64           `json:"durationMs"`
	Size        int64             `json:"size"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body"`
	Truncated   bool              `json:"truncated"`
	Error       string            `json:"error,omitempty"`
	ResolvedURL string            `json:"resolvedUrl"`
}

const tryBodyLimit = 256 << 10

func (s *server) handleTry(w http.ResponseWriter, r *http.Request) {
	var req TryRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&req); err != nil {
		apiErr(w, 400, "invalid request: "+err.Error())
		return
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	u := strings.TrimSpace(substitute(req.URL, req.Variables))
	out := TryResponse{ResolvedURL: u, Headers: map[string]string{}}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		out.Error = "URL must start with http:// or https://"
		writeJSON(w, 200, out)
		return
	}
	if req.TimeoutMs <= 0 {
		req.TimeoutMs = 30000
	}
	var body io.Reader
	if b := substitute(req.Body, req.Variables); b != "" {
		body = strings.NewReader(b)
	}
	hr, err := http.NewRequestWithContext(r.Context(), method, u, body)
	if err != nil {
		out.Error = err.Error()
		writeJSON(w, 200, out)
		return
	}
	for k, v := range req.Headers {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		v = substitute(v, req.Variables)
		if strings.EqualFold(k, "host") {
			hr.Host = v
			continue
		}
		hr.Header.Set(k, v)
	}
	if hr.Header.Get("User-Agent") == "" {
		hr.Header.Set("User-Agent", "LoadForge/1.0")
	}
	client := &http.Client{
		Timeout: time.Duration(req.TimeoutMs) * time.Millisecond,
		Transport: &http.Transport{
			Proxy:           nil,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: req.InsecureTLS}, //nolint:gosec // user opt-in
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	t0 := time.Now()
	resp, err := client.Do(hr)
	if err != nil {
		out.DurationMs = r2(float64(time.Since(t0).Microseconds()) / 1000)
		out.Error = classifyErr(err) + " — " + trimErr(err)
		if strings.Contains(u, "://localhost") || strings.Contains(u, "://127.0.0.1") {
			out.Error += ". Note: inside Docker, localhost is the LoadForge container — use host.docker.internal to reach your machine."
		}
		writeJSON(w, 200, out)
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, tryBodyLimit+1))
	rest, _ := io.Copy(io.Discard, resp.Body)
	out.DurationMs = r2(float64(time.Since(t0).Microseconds()) / 1000)
	out.Status = resp.StatusCode
	out.StatusText = http.StatusText(resp.StatusCode)
	out.OK = resp.StatusCode < 400
	out.Size = int64(len(data)) + rest
	if len(data) > tryBodyLimit {
		data = data[:tryBodyLimit]
		out.Truncated = true
	}
	if rest > 0 {
		out.Truncated = true
	}
	for k, v := range resp.Header {
		out.Headers[k] = strings.Join(v, ", ")
	}
	if utf8.Valid(data) {
		out.Body = string(data)
	} else {
		out.Body = "[binary response body, " + fmtBytes(out.Size) + "]"
	}
	writeJSON(w, 200, out)
}
