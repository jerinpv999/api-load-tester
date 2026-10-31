package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type result struct {
	ep     int
	ms     float64
	status int
	ok     bool
	errKey string
	bytes  int64
}

type epAgg struct {
	hist     *Histogram
	requests int64
	errors   int64
	codes    map[string]int64
}

// Engine executes one test run.
type Engine struct {
	run     *Run
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	results chan result
	client  *http.Client
	eps     []preparedEP
	weights []int // cumulative
	vus     atomic.Int64
	limiter *limiter
	stopped atomic.Bool

	// aggregation (owned by aggregator goroutine; read under mu)
	total    *Histogram
	window   *Histogram
	epStats  []*epAgg
	codes    map[string]int64
	errTypes map[string]int64
	bytesIn  int64
	winReq   int64
	winErr   int64
	winBytes int64

	subMu sync.Mutex
	subs  map[chan []byte]struct{}
	done  chan struct{}
	onEnd func(*Run)
}

type preparedEP struct {
	Endpoint
	dynamic   bool
	staticURL string
	headers   map[string]string
	expect    map[int]bool
}

var varRe = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:3])
}

func uuid4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func substitute(in string, vars map[string]string) string {
	if !strings.Contains(in, "{{") {
		return in
	}
	return varRe.ReplaceAllStringFunc(in, func(tok string) string {
		k := varRe.FindStringSubmatch(tok)[1]
		switch k {
		case "$uuid", "$guid", "$randomUUID":
			return uuid4()
		case "$timestamp":
			return strconv.FormatInt(time.Now().Unix(), 10)
		case "$isoTimestamp":
			return time.Now().UTC().Format(time.RFC3339)
		case "$randomInt":
			return strconv.Itoa(mrand.IntN(1000))
		case "$randomEmail":
			return fmt.Sprintf("user%d@example.com", mrand.IntN(1_000_000))
		case "$randomBoolean":
			return strconv.FormatBool(mrand.IntN(2) == 1)
		}
		if v, ok := vars[k]; ok {
			return v
		}
		return tok
	})
}

func joinURL(base, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if base == "" {
		return path
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func validateConfig(cfg *TestConfig) error {
	if cfg.Name == "" {
		cfg.Name = "Untitled test"
	}
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = 30000
	}
	var enabled []Endpoint
	for _, e := range cfg.Endpoints {
		if e.Enabled {
			if e.Weight <= 0 {
				e.Weight = 1
			}
			if e.Method == "" {
				e.Method = "GET"
			}
			enabled = append(enabled, e)
		}
	}
	if len(enabled) == 0 {
		return errors.New("select at least one endpoint")
	}
	cfg.Endpoints = enabled
	if len(cfg.Stages) == 0 {
		return errors.New("add at least one stage")
	}
	total := 0
	for _, st := range cfg.Stages {
		if st.DurationSec <= 0 || st.TargetVUs < 0 {
			return errors.New("stages need a positive duration and non-negative VUs")
		}
		if st.TargetVUs > 20000 {
			return errors.New("a single runner is capped at 20,000 virtual users")
		}
		total += st.DurationSec
	}
	if total > 24*3600 {
		return errors.New("total duration is capped at 24h")
	}
	for _, e := range cfg.Endpoints {
		u := substitute(joinURL(substitute(cfg.BaseURL, cfg.Variables), e.Path), cfg.Variables)
		if strings.Contains(u, "{{$") {
			continue
		}
		pu, err := url.Parse(varRe.ReplaceAllString(u, "x"))
		if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
			return fmt.Errorf("endpoint %q resolves to an invalid URL %q — check the Base URL", e.Name, u)
		}
	}
	return nil
}

func NewEngine(cfg TestConfig, onEnd func(*Run)) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		Proxy:               nil, // never route load through a proxy
		DialContext:         dialer.DialContext,
		MaxIdleConns:        0,
		MaxIdleConnsPerHost: 100000,
		MaxConnsPerHost:     0,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   !cfg.KeepAlive,
		DisableCompression:  false,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: cfg.InsecureTLS}, //nolint:gosec // user opt-in for self-signed test envs
		ForceAttemptHTTP2:   true,
	}
	e := &Engine{
		run: &Run{ID: newID(), Status: "running", Config: cfg, StartedAt: time.Now().UTC(), Series: []Point{}},
		ctx: ctx, cancel: cancel,
		results:  make(chan result, 200000),
		client:   &http.Client{Transport: tr, Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		total:    NewHistogram(),
		window:   NewHistogram(),
		codes:    map[string]int64{},
		errTypes: map[string]int64{},
		subs:     map[chan []byte]struct{}{},
		done:     make(chan struct{}),
		onEnd:    onEnd,
	}
	if cfg.MaxRPS > 0 {
		e.limiter = &limiter{interval: time.Second / time.Duration(cfg.MaxRPS)}
	}
	base := substitute(cfg.BaseURL, cfg.Variables)
	cum := 0
	for _, ep := range cfg.Endpoints {
		p := preparedEP{Endpoint: ep, headers: map[string]string{}}
		for k, v := range cfg.Headers {
			p.headers[k] = v
		}
		for k, v := range ep.Headers {
			p.headers[k] = v
		}
		// Resolve static variables once; keep dynamic ($...) for per-request substitution.
		for k, v := range p.headers {
			v = strings.TrimSpace(substitute(v, cfg.Variables))
			// drop auth placeholders the user left empty (e.g. "Bearer ")
			if v == "" || v == "Bearer" || v == "Basic" {
				delete(p.headers, k)
				continue
			}
			p.headers[k] = v
		}
		p.Path = substitute(p.Path, cfg.Variables)
		p.Body = substitute(p.Body, cfg.Variables)
		p.staticURL = joinURL(base, p.Path)
		p.dynamic = strings.Contains(p.staticURL, "{{") || strings.Contains(p.Body, "{{")
		for _, v := range p.headers {
			if strings.Contains(v, "{{") {
				p.dynamic = true
			}
		}
		if len(ep.ExpectStatus) > 0 {
			p.expect = map[int]bool{}
			for _, c := range ep.ExpectStatus {
				p.expect[c] = true
			}
		}
		e.eps = append(e.eps, p)
		cum += ep.Weight
		e.weights = append(e.weights, cum)
		e.epStats = append(e.epStats, &epAgg{hist: NewHistogram(), codes: map[string]int64{}})
	}
	return e
}

func (e *Engine) Start() {
	go e.aggregate()
	go e.control()
}

func (e *Engine) Stop() {
	e.stopped.Store(true)
	e.cancel()
}

// control spawns / retires virtual users following the stage profile.
func (e *Engine) control() {
	type vu struct{ cancel context.CancelFunc }
	var workers []vu
	var wg sync.WaitGroup
	start := time.Now()
	stages := e.run.Config.Stages
	totalDur := 0
	for _, st := range stages {
		totalDur += st.DurationSec
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()

	target := func(elapsed float64) int {
		prev := 0.0
		t := 0.0
		for _, st := range stages {
			d := float64(st.DurationSec)
			if elapsed < t+d {
				frac := (elapsed - t) / d
				return int(math.Round(prev + (float64(st.TargetVUs)-prev)*frac))
			}
			t += d
			prev = float64(st.TargetVUs)
		}
		return int(prev)
	}
	// Ramps start from 0 unless the first stage is "flat" — treat first stage as instant start
	// when its duration is short relative to target (handled by UI presets).
loop:
	for {
		elapsed := time.Since(start).Seconds()
		if elapsed >= float64(totalDur) {
			break
		}
		want := target(elapsed)
		if len(stages) > 0 && elapsed < float64(stages[0].DurationSec) && e.run.Config.Type == "load" && len(stages) == 1 {
			want = stages[0].TargetVUs // constant load: start all VUs at once
		}
		for len(workers) < want {
			ctx, cancel := context.WithCancel(e.ctx)
			workers = append(workers, vu{cancel})
			wg.Add(1)
			go func() { defer wg.Done(); e.worker(ctx) }()
		}
		for len(workers) > want {
			workers[len(workers)-1].cancel()
			workers = workers[:len(workers)-1]
		}
		e.vus.Store(int64(len(workers)))
		select {
		case <-e.ctx.Done():
			break loop
		case <-tick.C:
		}
	}
	for _, w := range workers {
		w.cancel()
	}
	e.vus.Store(0)
	wg.Wait()
	close(e.results)
	<-e.done
	e.finish()
}

func (e *Engine) pick() int {
	if len(e.eps) == 1 {
		return 0
	}
	r := mrand.IntN(e.weights[len(e.weights)-1])
	return sort.SearchInts(e.weights, r+1)
}

func (e *Engine) worker(ctx context.Context) {
	think := time.Duration(e.run.Config.ThinkTimeMs) * time.Millisecond
	for ctx.Err() == nil {
		if e.limiter != nil && !e.limiter.Wait(ctx) {
			return
		}
		i := e.pick()
		res := e.do(ctx, i)
		if ctx.Err() != nil && res.errKey == "cancelled" {
			return
		}
		select {
		case e.results <- res:
		default: // aggregator saturated: drop sample rather than block load generation
		}
		if think > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(think):
			}
		}
	}
}

func (e *Engine) do(ctx context.Context, i int) result {
	p := &e.eps[i]
	u, body := p.staticURL, p.Body
	headers := p.headers
	if p.dynamic {
		vars := e.run.Config.Variables
		u, body = substitute(u, vars), substitute(body, vars)
		headers = make(map[string]string, len(p.headers))
		for k, v := range p.headers {
			headers[k] = substitute(v, vars)
		}
	}
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, p.Method, u, rdr)
	if err != nil {
		return result{ep: i, errKey: "invalid request: " + trimErr(err)}
	}
	for k, v := range headers {
		if strings.EqualFold(k, "host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "LoadForge/1.0")
	}
	t0 := time.Now()
	resp, err := e.client.Do(req)
	if err != nil {
		ms := float64(time.Since(t0).Microseconds()) / 1000
		if ctx.Err() != nil {
			return result{ep: i, errKey: "cancelled"}
		}
		return result{ep: i, ms: ms, errKey: classifyErr(err)}
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	ms := float64(time.Since(t0).Microseconds()) / 1000
	ok := resp.StatusCode < 400
	if p.expect != nil {
		ok = p.expect[resp.StatusCode]
	}
	r := result{ep: i, ms: ms, status: resp.StatusCode, ok: ok, bytes: n}
	if !ok {
		r.errKey = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return r
}

func trimErr(err error) string {
	s := err.Error()
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

func classifyErr(err error) string {
	var ne net.Error
	msg := err.Error()
	switch {
	case errors.As(err, &ne) && ne.Timeout(), strings.Contains(msg, "Timeout"), strings.Contains(msg, "deadline exceeded"):
		return "timeout"
	case strings.Contains(msg, "connection refused"):
		return "connection refused"
	case strings.Contains(msg, "connection reset"):
		return "connection reset"
	case strings.Contains(msg, "no such host"):
		return "DNS lookup failed"
	case strings.Contains(msg, "EOF"):
		return "connection closed (EOF)"
	case strings.Contains(msg, "tls:") || strings.Contains(msg, "x509"):
		return "TLS error"
	case strings.Contains(msg, "too many open files"):
		return "too many open files (raise ulimit)"
	}
	return trimErr(err)
}

// aggregate consumes results and emits one Point per second.
func (e *Engine) aggregate() {
	defer close(e.done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	sec := 0
	for {
		select {
		case r, ok := <-e.results:
			if !ok {
				e.mu.Lock()
				if e.winReq > 0 || sec == 0 {
					sec++
					e.flushLocked(sec)
				}
				e.mu.Unlock()
				return
			}
			e.mu.Lock()
			e.record(r)
			e.mu.Unlock()
		case <-tick.C:
			sec++
			e.mu.Lock()
			e.flushLocked(sec)
			e.mu.Unlock()
			e.broadcast()
		}
	}
}

func (e *Engine) record(r result) {
	st := e.epStats[r.ep]
	st.requests++
	e.winReq++
	e.winBytes += r.bytes
	e.bytesIn += r.bytes
	if r.ms > 0 {
		e.window.Add(r.ms)
		e.total.Add(r.ms)
		st.hist.Add(r.ms)
	}
	key := "ERR"
	if r.status > 0 {
		key = strconv.Itoa(r.status)
	}
	e.codes[key]++
	st.codes[key]++
	if !r.ok {
		st.errors++
		e.winErr++
		e.errTypes[r.errKey]++
	}
}

func (e *Engine) flushLocked(sec int) {
	w := e.window
	p := Point{T: sec, VUs: int(e.vus.Load()), Requests: e.winReq, Errors: e.winErr, RPS: float64(e.winReq), BytesIn: e.winBytes}
	if w.n > 0 {
		p.P50, p.P95, p.P99 = r2(w.Quantile(.5)), r2(w.Quantile(.95)), r2(w.Quantile(.99))
		p.Avg, p.Max = r2(w.sum/float64(w.n)), r2(w.max)
	}
	e.run.Series = append(e.run.Series, p)
	e.window = NewHistogram()
	e.winReq, e.winErr, e.winBytes = 0, 0, 0
	e.buildSummaryLocked()
}

func (e *Engine) buildSummaryLocked() {
	var req, errs int64
	eps := make([]EndpointSummary, len(e.eps))
	elapsed := float64(len(e.run.Series))
	if elapsed < 1 {
		elapsed = 1
	}
	for i, st := range e.epStats {
		codes := make(map[string]int64, len(st.codes))
		for k, v := range st.codes {
			codes[k] = v
		}
		eps[i] = EndpointSummary{
			ID: e.eps[i].ID, Name: e.eps[i].Name, Method: e.eps[i].Method, Path: e.eps[i].Endpoint.Path,
			Requests: st.requests, Errors: st.errors, RPS: r2(float64(st.requests) / elapsed),
			Latency: st.hist.Stats(), StatusCodes: codes,
		}
		if st.requests > 0 {
			eps[i].ErrorRate = r2(float64(st.errors) / float64(st.requests) * 100)
		}
		req += st.requests
		errs += st.errors
	}
	sum := Summary{Requests: req, Errors: errs, BytesIn: e.bytesIn, DurationSec: elapsed,
		Latency: e.total.Stats(), StatusCodes: map[string]int64{}, ErrorsByType: map[string]int64{},
		Histogram: e.total.Buckets()}
	for k, v := range e.codes {
		sum.StatusCodes[k] = v
	}
	for k, v := range e.errTypes {
		sum.ErrorsByType[k] = v
	}
	if req > 0 {
		sum.ErrorRate = r2(float64(errs) / float64(req) * 100)
	}
	sum.AvgRPS = r2(float64(req) / elapsed)
	for _, p := range e.run.Series {
		sum.PeakRPS = math.Max(sum.PeakRPS, p.RPS)
		if p.VUs > sum.PeakVUs {
			sum.PeakVUs = p.VUs
		}
	}
	e.run.Endpoints = eps
	e.run.Summary = sum
}

func (e *Engine) finish() {
	e.mu.Lock()
	now := time.Now().UTC()
	e.run.FinishedAt = &now
	if e.stopped.Load() {
		e.run.Status = "stopped"
	} else {
		e.run.Status = "completed"
	}
	e.buildSummaryLocked()
	evaluate(e.run)
	snap := *e.run
	e.mu.Unlock()
	e.broadcast()
	e.subMu.Lock()
	for ch := range e.subs {
		close(ch)
	}
	e.subs = map[chan []byte]struct{}{}
	e.subMu.Unlock()
	if e.onEnd != nil {
		e.onEnd(&snap)
	}
}

// evaluate applies thresholds and detects the breaking point.
func evaluate(r *Run) {
	s := &r.Summary
	t := r.Config.Thresholds
	s.Thresholds = nil
	add := func(name string, limit, actual float64, pass bool) {
		s.Thresholds = append(s.Thresholds, ThresholdResult{Name: name, Limit: limit, Actual: actual, Passed: pass})
	}
	if t.P95Ms > 0 {
		add("p95 latency (ms)", t.P95Ms, s.Latency.P95, s.Latency.P95 <= t.P95Ms)
	}
	if t.P99Ms > 0 {
		add("p99 latency (ms)", t.P99Ms, s.Latency.P99, s.Latency.P99 <= t.P99Ms)
	}
	if t.ErrorRatePct > 0 {
		add("error rate (%)", t.ErrorRatePct, s.ErrorRate, s.ErrorRate <= t.ErrorRatePct)
	}
	if t.MinRPS > 0 {
		add("throughput (req/s)", t.MinRPS, s.AvgRPS, s.AvgRPS >= t.MinRPS)
	}
	s.Passed = true
	for _, th := range s.Thresholds {
		s.Passed = s.Passed && th.Passed
	}
	// Breaking point: baseline p95 from the first 15% of active seconds.
	var active []Point
	for _, p := range r.Series {
		if p.Requests > 0 {
			active = append(active, p)
		}
	}
	if len(active) >= 10 {
		nb := len(active) * 15 / 100
		if nb < 3 {
			nb = 3
		}
		vals := make([]float64, 0, nb)
		for _, p := range active[:nb] {
			vals = append(vals, p.P95)
		}
		sort.Float64s(vals)
		base := vals[len(vals)/2]
		for _, p := range active[nb:] {
			er := float64(p.Errors) / float64(p.Requests) * 100
			if er > 5 || (base > 0 && p.P95 > base*3 && p.P95 > 50) {
				bp := p
				s.BreakingPoint = &bp
				break
			}
		}
	}
}

// Snapshot returns a deep-enough copy for JSON encoding.
func (e *Engine) Snapshot() Run {
	e.mu.RLock()
	defer e.mu.RUnlock()
	r := *e.run
	r.Series = append(make([]Point, 0, len(e.run.Series)), e.run.Series...)
	return r
}

func (e *Engine) Subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 16)
	e.subMu.Lock()
	e.subs[ch] = struct{}{}
	e.subMu.Unlock()
	return ch, func() {
		e.subMu.Lock()
		if _, ok := e.subs[ch]; ok {
			delete(e.subs, ch)
			close(ch)
		}
		e.subMu.Unlock()
	}
}

type tickMsg struct {
	Status    string            `json:"status"`
	Point     *Point            `json:"point,omitempty"`
	Summary   Summary           `json:"summary"`
	Endpoints []EndpointSummary `json:"endpoints"`
	Elapsed   int               `json:"elapsed"`
}

func (e *Engine) broadcast() {
	e.mu.RLock()
	msg := tickMsg{Status: e.run.Status, Summary: e.run.Summary, Endpoints: e.run.Endpoints, Elapsed: len(e.run.Series)}
	if n := len(e.run.Series); n > 0 {
		p := e.run.Series[n-1]
		msg.Point = &p
	}
	e.mu.RUnlock()
	b, _ := json.Marshal(msg)
	e.subMu.Lock()
	for ch := range e.subs {
		select {
		case ch <- b:
		default:
		}
	}
	e.subMu.Unlock()
}

// limiter is a simple pacing limiter shared by all VUs.
type limiter struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func (l *limiter) Wait(ctx context.Context) bool {
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	at := l.next
	l.next = l.next.Add(l.interval)
	l.mu.Unlock()
	d := time.Until(at)
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
