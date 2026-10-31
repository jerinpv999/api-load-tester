package main

import "time"

// Endpoint is a single request template produced by an importer or edited in the UI.
type Endpoint struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Group   string            `json:"group,omitempty"`
	Method  string            `json:"method"`
	Path    string            `json:"path"` // relative path or absolute URL; may contain {{vars}}
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Weight  int               `json:"weight"`
	Enabled bool              `json:"enabled"`
	// ExpectStatus: empty = any 2xx/3xx counts as success
	ExpectStatus []int `json:"expectStatus,omitempty"`
}

// Collection is the normalised result of importing an OpenAPI spec or Postman collection.
type Collection struct {
	Name      string            `json:"name"`
	Source    string            `json:"source"` // openapi3 | swagger2 | postman | har
	BaseURL   string            `json:"baseUrl"`
	Headers   map[string]string `json:"headers,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`
	Endpoints []Endpoint        `json:"endpoints"`
	Warnings  []string          `json:"warnings,omitempty"`
}

// Stage ramps linearly to TargetVUs over DurationSec.
type Stage struct {
	DurationSec int `json:"durationSec"`
	TargetVUs   int `json:"targetVus"`
}

type Thresholds struct {
	P95Ms        float64 `json:"p95Ms,omitempty"`
	P99Ms        float64 `json:"p99Ms,omitempty"`
	ErrorRatePct float64 `json:"errorRatePct,omitempty"`
	MinRPS       float64 `json:"minRps,omitempty"`
}

type TestConfig struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"` // load | stress | spike | soak | custom
	BaseURL     string            `json:"baseUrl"`
	Headers     map[string]string `json:"headers,omitempty"`
	Variables   map[string]string `json:"variables,omitempty"`
	Endpoints   []Endpoint        `json:"endpoints"`
	Stages      []Stage           `json:"stages"`
	MaxRPS      int               `json:"maxRps"`      // 0 = unlimited
	TimeoutMs   int               `json:"timeoutMs"`   // per request
	ThinkTimeMs int               `json:"thinkTimeMs"` // pause between requests per VU
	InsecureTLS bool              `json:"insecureTls"`
	KeepAlive   bool              `json:"keepAlive"`
	Thresholds  Thresholds        `json:"thresholds"`
}

// LatencyStats summarises a distribution in milliseconds.
type LatencyStats struct {
	Min float64 `json:"min"`
	Avg float64 `json:"avg"`
	P50 float64 `json:"p50"`
	P75 float64 `json:"p75"`
	P90 float64 `json:"p90"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

// Point is one second of the time series.
type Point struct {
	T        int     `json:"t"` // seconds since start
	VUs      int     `json:"vus"`
	Requests int64   `json:"requests"`
	Errors   int64   `json:"errors"`
	RPS      float64 `json:"rps"`
	P50      float64 `json:"p50"`
	P95      float64 `json:"p95"`
	P99      float64 `json:"p99"`
	Avg      float64 `json:"avg"`
	Max      float64 `json:"max"`
	BytesIn  int64   `json:"bytesIn"`
}

type EndpointSummary struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Method      string           `json:"method"`
	Path        string           `json:"path"`
	Requests    int64            `json:"requests"`
	Errors      int64            `json:"errors"`
	ErrorRate   float64          `json:"errorRate"`
	RPS         float64          `json:"rps"`
	Latency     LatencyStats     `json:"latency"`
	StatusCodes map[string]int64 `json:"statusCodes"`
}

type HistBucket struct {
	LeMs  float64 `json:"leMs"`
	Count int64   `json:"count"`
}

type ThresholdResult struct {
	Name   string  `json:"name"`
	Limit  float64 `json:"limit"`
	Actual float64 `json:"actual"`
	Passed bool    `json:"passed"`
}

type Summary struct {
	Requests     int64             `json:"requests"`
	Errors       int64             `json:"errors"`
	ErrorRate    float64           `json:"errorRate"`
	AvgRPS       float64           `json:"avgRps"`
	PeakRPS      float64           `json:"peakRps"`
	PeakVUs      int               `json:"peakVus"`
	BytesIn      int64             `json:"bytesIn"`
	DurationSec  float64           `json:"durationSec"`
	Latency      LatencyStats      `json:"latency"`
	StatusCodes  map[string]int64  `json:"statusCodes"`
	ErrorsByType map[string]int64  `json:"errorsByType"`
	Histogram    []HistBucket      `json:"histogram"`
	Thresholds   []ThresholdResult `json:"thresholds"`
	Passed       bool              `json:"passed"`
	// BreakingPoint: first second where error rate > 5% or p95 > 3x baseline (stress tests)
	BreakingPoint *Point `json:"breakingPoint,omitempty"`
}

type Run struct {
	ID         string            `json:"id"`
	Status     string            `json:"status"` // running | completed | stopped | failed
	Config     TestConfig        `json:"config"`
	StartedAt  time.Time         `json:"startedAt"`
	FinishedAt *time.Time        `json:"finishedAt,omitempty"`
	Series     []Point           `json:"series"`
	Endpoints  []EndpointSummary `json:"endpoints"`
	Summary    Summary           `json:"summary"`
	Error      string            `json:"error,omitempty"`
}

// RunListItem is a light row for the history table.
type RunListItem struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Requests   int64      `json:"requests"`
	ErrorRate  float64    `json:"errorRate"`
	AvgRPS     float64    `json:"avgRps"`
	P95        float64    `json:"p95"`
	Passed     bool       `json:"passed"`
}
