package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"html"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------- CSV ----------------

func RenderCSV(r *Run) []byte {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"second", "vus", "requests", "errors", "rps", "avg_ms", "p50_ms", "p95_ms", "p99_ms", "max_ms", "bytes_in"})
	for _, p := range r.Series {
		_ = w.Write([]string{itoa(p.T), itoa(p.VUs), i64(p.Requests), i64(p.Errors), ff(p.RPS), ff(p.Avg), ff(p.P50), ff(p.P95), ff(p.P99), ff(p.Max), i64(p.BytesIn)})
	}
	_ = w.Write(nil)
	_ = w.Write([]string{"endpoint", "method", "path", "requests", "errors", "error_rate_pct", "rps", "avg_ms", "p50_ms", "p90_ms", "p95_ms", "p99_ms", "max_ms"})
	for _, e := range r.Endpoints {
		l := e.Latency
		_ = w.Write([]string{e.Name, e.Method, e.Path, i64(e.Requests), i64(e.Errors), ff(e.ErrorRate), ff(e.RPS), ff(l.Avg), ff(l.P50), ff(l.P90), ff(l.P95), ff(l.P99), ff(l.Max)})
	}
	w.Flush()
	return b.Bytes()
}

func itoa(v int) string   { return strconv.Itoa(v) }
func i64(v int64) string  { return strconv.FormatInt(v, 10) }
func ff(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// ---------------- HTML report ----------------

var palette = struct{ blue, amber, red, green, violet, slate, grid, ink, muted string }{
	"#2563eb", "#d97706", "#dc2626", "#059669", "#7c3aed", "#64748b", "#e5e7eb", "#0f172a", "#64748b",
}

type series struct {
	name   string
	color  string
	values []float64
	area   bool
	dash   bool
	right  bool // use right axis
	bars   bool
}

func fmtNum(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.1fB", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1e4:
		return fmt.Sprintf("%.1fk", v/1e3)
	case v == math.Trunc(v):
		return fmt.Sprintf("%.0f", v)
	case v >= 100:
		return fmt.Sprintf("%.0f", v)
	case v >= 10:
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

func fmtMs(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("%.2f s", v/1000)
	}
	if v >= 100 {
		return fmt.Sprintf("%.0f ms", v)
	}
	return fmt.Sprintf("%.1f ms", v)
}

func fmtBytes(v int64) string {
	f := float64(v)
	for _, u := range []string{"B", "KB", "MB", "GB", "TB"} {
		if f < 1024 {
			return fmt.Sprintf("%.1f %s", f, u)
		}
		f /= 1024
	}
	return fmt.Sprintf("%.1f PB", f)
}

func fmtDur(sec float64) string {
	d := time.Duration(sec) * time.Second
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm %02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func niceMax(v float64) float64 {
	if v <= 0 {
		return 1
	}
	exp := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10} {
		if m*exp >= v {
			return m * exp
		}
	}
	return 10 * exp
}

// downsample keeps charts readable for long soak tests (max points ≈ 600).
func downsample(points []Point, max int) []Point {
	if len(points) <= max {
		return points
	}
	step := int(math.Ceil(float64(len(points)) / float64(max)))
	var out []Point
	for i := 0; i < len(points); i += step {
		end := i + step
		if end > len(points) {
			end = len(points)
		}
		agg := Point{T: points[i].T}
		n := float64(end - i)
		for _, p := range points[i:end] {
			agg.RPS += p.RPS / n
			agg.Requests += p.Requests
			agg.Errors += p.Errors
			agg.P50 += p.P50 / n
			agg.Avg += p.Avg / n
			agg.P95 = math.Max(agg.P95, p.P95)
			agg.P99 = math.Max(agg.P99, p.P99)
			agg.Max = math.Max(agg.Max, p.Max)
			if p.VUs > agg.VUs {
				agg.VUs = p.VUs
			}
		}
		out = append(out, agg)
	}
	return out
}

// timeChart draws an SVG time-series with optional right axis.
func timeChart(xs []int, ss []series, leftLabel, rightLabel string, marker int) string {
	const W, H = 900, 260
	const pl, pr, pt, pb = 56, 56, 16, 34
	cw, ch := float64(W-pl-pr), float64(H-pt-pb)
	n := len(xs)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %d %d" class="chart" role="img" xmlns="http://www.w3.org/2000/svg">`, W, H)
	if n == 0 {
		b.WriteString(`<text x="450" y="130" text-anchor="middle" class="ax">No data</text></svg>`)
		return b.String()
	}
	var lmax, rmax float64
	hasRight := false
	for _, s := range ss {
		for _, v := range s.values {
			if s.right {
				rmax = math.Max(rmax, v)
				hasRight = true
			} else {
				lmax = math.Max(lmax, v)
			}
		}
	}
	lmax, rmax = niceMax(lmax), niceMax(rmax)
	x := func(i int) float64 {
		if n == 1 {
			return float64(pl) + cw/2
		}
		return float64(pl) + cw*float64(i)/float64(n-1)
	}
	y := func(v, max float64) float64 { return float64(pt) + ch - ch*v/max }

	// grid & left axis
	for i := 0; i <= 4; i++ {
		v := lmax * float64(i) / 4
		yy := y(v, lmax)
		fmt.Fprintf(&b, `<line x1="%d" x2="%d" y1="%.1f" y2="%.1f" class="grid"/>`, pl, W-pr, yy, yy)
		fmt.Fprintf(&b, `<text x="%d" y="%.1f" text-anchor="end" class="ax">%s</text>`, pl-8, yy+4, fmtNum(v))
		if hasRight {
			fmt.Fprintf(&b, `<text x="%d" y="%.1f" class="ax">%s</text>`, W-pr+8, yy+4, fmtNum(rmax*float64(i)/4))
		}
	}
	// x ticks
	ticks := 6
	if n < ticks {
		ticks = n
	}
	for i := 0; i < ticks; i++ {
		idx := 0
		if ticks > 1 {
			idx = i * (n - 1) / (ticks - 1)
		}
		fmt.Fprintf(&b, `<text x="%.1f" y="%d" text-anchor="middle" class="ax">%s</text>`, x(idx), H-12, fmtDur(float64(xs[idx])))
	}
	if leftLabel != "" {
		fmt.Fprintf(&b, `<text x="14" y="%d" class="ax lbl" transform="rotate(-90 14 %d)" text-anchor="middle">%s</text>`, pt+int(ch)/2, pt+int(ch)/2, html.EscapeString(leftLabel))
	}
	if hasRight && rightLabel != "" {
		fmt.Fprintf(&b, `<text x="%d" y="%d" class="ax lbl" transform="rotate(90 %d %d)" text-anchor="middle">%s</text>`, W-12, pt+int(ch)/2, W-12, pt+int(ch)/2, html.EscapeString(rightLabel))
	}
	// marker (breaking point)
	if marker >= 0 && marker < n {
		mx := x(marker)
		fmt.Fprintf(&b, `<line x1="%.1f" x2="%.1f" y1="%d" y2="%.1f" stroke="%s" stroke-dasharray="4 4" stroke-width="1.5"/>`, mx, mx, pt, float64(pt)+ch, palette.red)
		fmt.Fprintf(&b, `<text x="%.1f" y="%d" class="ax" fill="%s" text-anchor="start">breaking point</text>`, mx+4, pt+12, palette.red)
	}
	for _, s := range ss {
		max := lmax
		if s.right {
			max = rmax
		}
		if s.bars {
			bw := math.Max(1, cw/float64(n)*0.7)
			for i, v := range s.values {
				if v <= 0 {
					continue
				}
				fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s" rx="1"/>`, x(i)-bw/2, y(v, max), bw, float64(pt)+ch-y(v, max), s.color)
			}
			continue
		}
		var pts strings.Builder
		for i, v := range s.values {
			fmt.Fprintf(&pts, "%.1f,%.1f ", x(i), y(v, max))
		}
		if s.area {
			fmt.Fprintf(&b, `<polygon points="%.1f,%.1f %s%.1f,%.1f" fill="%s" fill-opacity="0.14"/>`, x(0), float64(pt)+ch, pts.String(), x(n-1), float64(pt)+ch, s.color)
		}
		dash := ""
		if s.dash {
			dash = ` stroke-dasharray="5 4"`
		}
		fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="2" stroke-linejoin="round"%s/>`, pts.String(), s.color, dash)
	}
	b.WriteString(`</svg>`)
	// legend
	b.WriteString(`<div class="legend">`)
	for _, s := range ss {
		sw := "sw"
		if s.dash {
			sw = "sw dash"
		}
		fmt.Fprintf(&b, `<span><i class="%s" style="--c:%s"></i>%s</span>`, sw, s.color, html.EscapeString(s.name))
	}
	b.WriteString(`</div>`)
	return b.String()
}

// hbars draws labelled horizontal bars.
func hbars(labels []string, values []float64, colors []string, fmtV func(float64) string) string {
	if len(values) == 0 {
		return `<p class="muted">No data</p>`
	}
	max := 0.0
	for _, v := range values {
		max = math.Max(max, v)
	}
	if max == 0 {
		max = 1
	}
	var b strings.Builder
	b.WriteString(`<div class="hbars">`)
	for i, l := range labels {
		c := palette.blue
		if i < len(colors) && colors[i] != "" {
			c = colors[i]
		}
		fmt.Fprintf(&b, `<div class="hb"><span class="hbl">%s</span><span class="hbt"><span class="hbf" style="width:%.1f%%;background:%s"></span></span><span class="hbv">%s</span></div>`,
			html.EscapeString(l), values[i]/max*100, c, fmtV(values[i]))
	}
	b.WriteString(`</div>`)
	return b.String()
}

func histogramChart(h []HistBucket, total int64) string {
	const W, H = 480, 240
	const pl, pr, pt, pb = 8, 8, 18, 40
	if len(h) == 0 || total == 0 {
		return `<p class="muted">No data</p>`
	}
	cw, ch := float64(W-pl-pr), float64(H-pt-pb)
	var max int64
	for _, b := range h {
		if b.Count > max {
			max = b.Count
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %d %d" class="chart" xmlns="http://www.w3.org/2000/svg">`, W, H)
	bw := cw / float64(len(h))
	prev := 0.0
	for i, bk := range h {
		hh := ch * float64(bk.Count) / float64(max)
		xx := float64(pl) + bw*float64(i)
		pct := float64(bk.Count) / float64(total) * 100
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s" rx="3"><title>%.1f%%</title></rect>`, xx+3, float64(pt)+ch-hh, bw-6, hh, palette.violet, pct)
		if pct >= 0.5 {
			fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" text-anchor="middle" class="ax">%.0f%%</text>`, xx+bw/2, float64(pt)+ch-hh-4, pct)
		}
		label := ""
		if bk.LeMs < 0 {
			label = fmt.Sprintf(">%s", fmtMsShort(prev))
		} else {
			label = fmt.Sprintf("≤%s", fmtMsShort(bk.LeMs))
		}
		fmt.Fprintf(&b, `<text x="%.1f" y="%d" text-anchor="middle" class="ax">%s</text>`, xx+bw/2, H-20, label)
		if bk.LeMs > 0 {
			prev = bk.LeMs
		}
	}
	fmt.Fprintf(&b, `<line x1="%d" x2="%d" y1="%.1f" y2="%.1f" class="axis"/>`, pl, W-pr, float64(pt)+ch, float64(pt)+ch)
	fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" class="ax lbl">response time bucket</text></svg>`, W/2, H-3)
	return b.String()
}

func fmtMsShort(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("%gs", v/1000)
	}
	return fmt.Sprintf("%gms", v)
}

func statusColor(code string) string {
	switch {
	case strings.HasPrefix(code, "2"):
		return palette.green
	case strings.HasPrefix(code, "3"):
		return palette.blue
	case strings.HasPrefix(code, "4"):
		return palette.amber
	}
	return palette.red
}

func RenderHTML(r *Run) []byte {
	s := r.Summary
	pts := downsample(r.Series, 600)
	xs := make([]int, len(pts))
	rps, vus, p50, p95, p99, errs, errRate := make([]float64, len(pts)), make([]float64, len(pts)), make([]float64, len(pts)), make([]float64, len(pts)), make([]float64, len(pts)), make([]float64, len(pts)), make([]float64, len(pts))
	marker := -1
	for i, p := range pts {
		xs[i] = p.T
		rps[i], vus[i], p50[i], p95[i], p99[i] = p.RPS, float64(p.VUs), p.P50, p.P95, p.P99
		errs[i] = float64(p.Errors)
		if p.Requests > 0 {
			errRate[i] = float64(p.Errors) / float64(p.Requests) * 100
		}
		if s.BreakingPoint != nil && marker < 0 && p.T >= s.BreakingPoint.T {
			marker = i
		}
	}
	verdict, vclass := "PASSED", "pass"
	if len(s.Thresholds) == 0 {
		verdict, vclass = "NO THRESHOLDS", "neutral"
	} else if !s.Passed {
		verdict, vclass = "FAILED", "fail"
	}
	if r.Status == "stopped" {
		verdict += " · stopped early"
	}

	var b strings.Builder
	e := html.EscapeString
	fmt.Fprintf(&b, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s — Load test report</title><style>%s</style></head><body><main>`, e(r.Config.Name), reportCSS)

	// Header
	fmt.Fprintf(&b, `<header><div><div class="eyebrow">LoadForge · %s test report</div><h1>%s</h1>
<div class="meta">%s · %s → %s · target <code>%s</code></div></div><div class="verdict %s">%s</div></header>`,
		e(strings.ToUpper(r.Config.Type)), e(r.Config.Name), e(r.ID),
		r.StartedAt.Format("02 Jan 2006 15:04:05 MST"), finishedStr(r), e(r.Config.BaseURL), vclass, e(verdict))

	// KPIs
	kpis := []struct{ label, value, sub string }{
		{"Total requests", fmtNum(float64(s.Requests)), fmt.Sprintf("over %s", fmtDur(s.DurationSec))},
		{"Avg throughput", fmtNum(s.AvgRPS) + " req/s", "peak " + fmtNum(s.PeakRPS) + " req/s"},
		{"Error rate", fmt.Sprintf("%.2f%%", s.ErrorRate), fmtNum(float64(s.Errors)) + " failed"},
		{"Median (p50)", fmtMs(s.Latency.P50), "avg " + fmtMs(s.Latency.Avg)},
		{"p95 latency", fmtMs(s.Latency.P95), "p90 " + fmtMs(s.Latency.P90)},
		{"p99 latency", fmtMs(s.Latency.P99), "max " + fmtMs(s.Latency.Max)},
		{"Peak virtual users", fmtNum(float64(s.PeakVUs)), fmt.Sprintf("%d stage(s)", len(r.Config.Stages))},
		{"Data received", fmtBytes(s.BytesIn), fmtBytes(int64(float64(s.BytesIn)/math.Max(s.DurationSec, 1))) + "/s"},
	}
	b.WriteString(`<section class="kpis">`)
	for _, k := range kpis {
		cls := ""
		if k.label == "Error rate" && s.ErrorRate > 1 {
			cls = " bad"
		}
		fmt.Fprintf(&b, `<div class="kpi%s"><div class="kl">%s</div><div class="kv">%s</div><div class="ks">%s</div></div>`, cls, k.label, k.value, k.sub)
	}
	b.WriteString(`</section>`)

	// Findings
	b.WriteString(`<section class="card"><h2>Key findings</h2><ul class="findings">`)
	for _, f := range findings(r) {
		fmt.Fprintf(&b, `<li>%s</li>`, f)
	}
	b.WriteString(`</ul>`)
	if len(s.Thresholds) > 0 {
		b.WriteString(`<table class="tbl"><thead><tr><th>Threshold</th><th class="r">Limit</th><th class="r">Actual</th><th class="r">Result</th></tr></thead><tbody>`)
		for _, t := range s.Thresholds {
			res := `<span class="tag pass">PASS</span>`
			if !t.Passed {
				res = `<span class="tag fail">FAIL</span>`
			}
			fmt.Fprintf(&b, `<tr><td>%s</td><td class="r">%s</td><td class="r">%s</td><td class="r">%s</td></tr>`, e(t.Name), fmtNum(t.Limit), fmtNum(t.Actual), res)
		}
		b.WriteString(`</tbody></table>`)
	}
	b.WriteString(`</section>`)

	// Charts
	fmt.Fprintf(&b, `<section class="card"><h2>Throughput &amp; virtual users</h2><p class="muted">Requests completed per second against active virtual users.</p>%s</section>`,
		timeChart(xs, []series{{name: "Requests/s", color: palette.blue, values: rps, area: true}, {name: "Virtual users", color: palette.slate, values: vus, dash: true, right: true}}, "req/s", "VUs", marker))
	fmt.Fprintf(&b, `<section class="card"><h2>Response time over time</h2><p class="muted">Percentiles per second (ms).</p>%s</section>`,
		timeChart(xs, []series{{name: "p50", color: palette.green, values: p50}, {name: "p95", color: palette.amber, values: p95}, {name: "p99", color: palette.red, values: p99}}, "ms", "", marker))
	fmt.Fprintf(&b, `<section class="card"><h2>Errors over time</h2><p class="muted">Failed requests per second (bars) and error rate %% (line).</p>%s</section>`,
		timeChart(xs, []series{{name: "Errors/s", color: palette.red, values: errs, bars: true}, {name: "Error rate %", color: palette.ink, values: errRate, right: true}}, "errors/s", "%", marker))

	b.WriteString(`<div class="two">`)
	fmt.Fprintf(&b, `<section class="card"><h2>Latency distribution</h2>%s</section>`, histogramChart(s.Histogram, s.Requests))
	codes := make([]string, 0, len(s.StatusCodes))
	for k := range s.StatusCodes {
		codes = append(codes, k)
	}
	sort.Strings(codes)
	cv, cc, cl := []float64{}, []string{}, []string{}
	for _, c := range codes {
		cl = append(cl, c)
		cv = append(cv, float64(s.StatusCodes[c]))
		cc = append(cc, statusColor(c))
	}
	fmt.Fprintf(&b, `<section class="card"><h2>Status codes</h2>%s</section>`, hbars(cl, cv, cc, func(v float64) string {
		return fmt.Sprintf("%s · %.1f%%", fmtNum(v), v/math.Max(float64(s.Requests), 1)*100)
	}))
	b.WriteString(`</div>`)

	// Endpoints
	b.WriteString(`<section class="card"><h2>Endpoints</h2><div class="scroll"><table class="tbl"><thead><tr><th>Endpoint</th><th class="r">Requests</th><th class="r">req/s</th><th class="r">Errors</th><th class="r">avg</th><th class="r">p50</th><th class="r">p90</th><th class="r">p95</th><th class="r">p99</th><th class="r">max</th><th>p95 vs slowest</th></tr></thead><tbody>`)
	maxP95 := 0.0
	for _, ep := range r.Endpoints {
		maxP95 = math.Max(maxP95, ep.Latency.P95)
	}
	eps := append([]EndpointSummary(nil), r.Endpoints...)
	sort.SliceStable(eps, func(i, j int) bool { return eps[i].Latency.P95 > eps[j].Latency.P95 })
	for _, ep := range eps {
		l := ep.Latency
		errCls := ""
		if ep.ErrorRate > 1 {
			errCls = ` class="r bad"`
		} else {
			errCls = ` class="r"`
		}
		fmt.Fprintf(&b, `<tr><td><span class="m m-%s">%s</span> <b>%s</b><div class="path">%s</div></td><td class="r">%s</td><td class="r">%s</td><td%s>%.2f%%</td><td class="r">%s</td><td class="r">%s</td><td class="r">%s</td><td class="r"><b>%s</b></td><td class="r">%s</td><td class="r">%s</td><td><span class="mini"><span style="width:%.0f%%"></span></span></td></tr>`,
			strings.ToLower(ep.Method), e(ep.Method), e(ep.Name), e(ep.Path), fmtNum(float64(ep.Requests)), fmtNum(ep.RPS), errCls, ep.ErrorRate,
			fmtMs(l.Avg), fmtMs(l.P50), fmtMs(l.P90), fmtMs(l.P95), fmtMs(l.P99), fmtMs(l.Max), l.P95/math.Max(maxP95, 0.001)*100)
	}
	b.WriteString(`</tbody></table></div></section>`)

	// Errors by type
	if len(s.ErrorsByType) > 0 {
		type kv struct {
			k string
			v int64
		}
		var list []kv
		for k, v := range s.ErrorsByType {
			list = append(list, kv{k, v})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
		var ls []string
		var vs []float64
		for i, x := range list {
			if i >= 12 {
				break
			}
			ls = append(ls, x.k)
			vs = append(vs, float64(x.v))
		}
		cols := make([]string, len(ls))
		for i := range cols {
			cols[i] = palette.red
		}
		fmt.Fprintf(&b, `<section class="card"><h2>Errors by type</h2>%s</section>`, hbars(ls, vs, cols, fmtNum))
	}

	// Configuration
	c := r.Config
	b.WriteString(`<section class="card"><h2>Test configuration</h2><div class="two cfg"><div><table class="tbl"><tbody>`)
	rows := [][2]string{
		{"Test type", c.Type}, {"Base URL", c.BaseURL}, {"Endpoints", strconv.Itoa(len(c.Endpoints))},
		{"Rate limit", map[bool]string{true: "unlimited", false: fmtNum(float64(c.MaxRPS)) + " req/s"}[c.MaxRPS == 0]},
		{"Request timeout", fmtMs(float64(c.TimeoutMs))}, {"Think time", fmtMs(float64(c.ThinkTimeMs))},
		{"Keep-alive", strconv.FormatBool(c.KeepAlive)}, {"Skip TLS verify", strconv.FormatBool(c.InsecureTLS)},
	}
	for _, rr := range rows {
		fmt.Fprintf(&b, `<tr><td class="muted">%s</td><td>%s</td></tr>`, rr[0], e(rr[1]))
	}
	b.WriteString(`</tbody></table></div><div><table class="tbl"><thead><tr><th>#</th><th class="r">Duration</th><th class="r">Target VUs</th></tr></thead><tbody>`)
	for i, st := range c.Stages {
		fmt.Fprintf(&b, `<tr><td>Stage %d</td><td class="r">%s</td><td class="r">%d</td></tr>`, i+1, fmtDur(float64(st.DurationSec)), st.TargetVUs)
	}
	b.WriteString(`</tbody></table></div></div></section>`)
	fmt.Fprintf(&b, `<footer>Generated by LoadForge on %s · Tip: use your browser's Print → Save as PDF for a PDF copy.</footer></main></body></html>`, time.Now().UTC().Format(time.RFC1123))
	return []byte(b.String())
}

func finishedStr(r *Run) string {
	if r.FinishedAt == nil {
		return "in progress"
	}
	return r.FinishedAt.Format("15:04:05 MST")
}

func findings(r *Run) []string {
	s := r.Summary
	var out []string
	out = append(out, fmt.Sprintf("Served <b>%s requests</b> at an average of <b>%s req/s</b> (peak %s req/s) with up to %d virtual users.",
		fmtNum(float64(s.Requests)), fmtNum(s.AvgRPS), fmtNum(s.PeakRPS), s.PeakVUs))
	out = append(out, fmt.Sprintf("95%% of requests completed within <b>%s</b>; 99%% within <b>%s</b>.", fmtMs(s.Latency.P95), fmtMs(s.Latency.P99)))
	if s.ErrorRate == 0 {
		out = append(out, "No failed requests were recorded.")
	} else {
		top, tv := "", int64(0)
		for k, v := range s.ErrorsByType {
			if v > tv {
				top, tv = k, v
			}
		}
		out = append(out, fmt.Sprintf("<b>%.2f%%</b> of requests failed; the most common cause was <b>%s</b> (%s occurrences).", s.ErrorRate, html.EscapeString(top), fmtNum(float64(tv))))
	}
	if bp := s.BreakingPoint; bp != nil {
		out = append(out, fmt.Sprintf("<b>Breaking point</b> detected at %s with <b>%d VUs</b> (~%s req/s): p95 rose to %s and %d/%d requests failed in that second.",
			fmtDur(float64(bp.T)), bp.VUs, fmtNum(bp.RPS), fmtMs(bp.P95), bp.Errors, bp.Requests))
	} else if r.Config.Type == "stress" || r.Config.Type == "spike" {
		out = append(out, "No breaking point detected — the system kept latency and errors stable across the full ramp.")
	}
	var slowest *EndpointSummary
	for i := range r.Endpoints {
		if slowest == nil || r.Endpoints[i].Latency.P95 > slowest.Latency.P95 {
			slowest = &r.Endpoints[i]
		}
	}
	if slowest != nil && len(r.Endpoints) > 1 {
		out = append(out, fmt.Sprintf("Slowest endpoint: <b>%s %s</b> (p95 %s).", html.EscapeString(slowest.Method), html.EscapeString(slowest.Name), fmtMs(slowest.Latency.P95)))
	}
	return out
}

const reportCSS = `
:root{--ink:#0f172a;--muted:#64748b;--line:#e5e7eb;--bg:#f6f7f9;--card:#fff;--blue:#2563eb}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Inter,Roboto,Helvetica,Arial,sans-serif}
main{max-width:1080px;margin:0 auto;padding:32px 20px 48px}
header{display:flex;justify-content:space-between;align-items:flex-start;gap:16px;margin-bottom:20px}
.eyebrow{font-size:12px;letter-spacing:.08em;text-transform:uppercase;color:var(--blue);font-weight:600}
h1{margin:4px 0 6px;font-size:28px;letter-spacing:-.02em}h2{font-size:16px;margin:0 0 4px}
.meta{color:var(--muted);font-size:13px}code{background:#eef2ff;padding:1px 6px;border-radius:4px;font-size:12px}
.verdict{padding:10px 16px;border-radius:10px;font-weight:700;letter-spacing:.04em;white-space:nowrap}
.verdict.pass{background:#dcfce7;color:#166534}.verdict.fail{background:#fee2e2;color:#991b1b}.verdict.neutral{background:#e2e8f0;color:#334155}
.kpis{display:grid;grid-template-columns:repeat(4,1fr);gap:12px;margin-bottom:16px}
.kpi{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:14px 16px}
.kl{font-size:12px;color:var(--muted)}.kv{font-size:22px;font-weight:700;letter-spacing:-.01em;margin:2px 0;font-variant-numeric:tabular-nums}.ks{font-size:12px;color:var(--muted)}
.kpi.bad .kv{color:#dc2626}
.card{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:18px 20px;margin-bottom:16px;break-inside:avoid}
.muted{color:var(--muted);margin:0 0 8px;font-size:13px}
.chart{width:100%;height:auto;display:block}.chart .grid{stroke:#eef0f3}.chart .axis{stroke:#cbd5e1}
.chart .ax{font-size:11px;fill:#64748b;font-family:inherit}.chart .lbl{font-size:11px}
.legend{display:flex;gap:16px;flex-wrap:wrap;font-size:12px;color:var(--muted);margin-top:4px}
.legend .sw{display:inline-block;width:14px;height:3px;background:var(--c);margin-right:6px;vertical-align:middle;border-radius:2px}
.legend .sw.dash{background:repeating-linear-gradient(90deg,var(--c) 0 4px,transparent 4px 7px)}
.two{display:grid;grid-template-columns:1fr 1fr;gap:16px}.two>.card{margin-bottom:16px}
.hbars{display:flex;flex-direction:column;gap:8px;margin-top:8px}.hb{display:grid;grid-template-columns:minmax(80px,38%) 1fr auto;gap:10px;align-items:center;font-size:13px}
.hbl{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.hbt{background:#f1f5f9;border-radius:4px;height:12px;overflow:hidden}.hbf{display:block;height:100%;border-radius:4px}
.hbv{font-variant-numeric:tabular-nums;color:var(--muted);font-size:12px;white-space:nowrap}
.tbl{width:100%;border-collapse:collapse;font-size:13px;margin-top:8px}.tbl th{text-align:left;font-weight:600;color:var(--muted);font-size:12px;border-bottom:1px solid var(--line);padding:8px 6px}
.tbl td{border-bottom:1px solid #f1f5f9;padding:8px 6px;vertical-align:top;font-variant-numeric:tabular-nums}.r{text-align:right}.bad{color:#dc2626;font-weight:600}
.path{color:var(--muted);font-family:ui-monospace,Menlo,monospace;font-size:11px;word-break:break-all}
.m{display:inline-block;font:700 10px ui-monospace,monospace;padding:2px 5px;border-radius:4px;background:#e2e8f0;color:#334155}
.m-get{background:#dbeafe;color:#1d4ed8}.m-post{background:#dcfce7;color:#15803d}.m-put{background:#fef3c7;color:#b45309}.m-patch{background:#ede9fe;color:#6d28d9}.m-delete{background:#fee2e2;color:#b91c1c}
.mini{display:block;width:90px;height:8px;background:#f1f5f9;border-radius:4px;overflow:hidden;margin-top:5px}.mini span{display:block;height:100%;background:#d97706}
.tag{font-size:11px;font-weight:700;padding:2px 8px;border-radius:999px}.tag.pass{background:#dcfce7;color:#166534}.tag.fail{background:#fee2e2;color:#991b1b}
.findings{margin:8px 0 0;padding-left:18px}.findings li{margin:4px 0}
.scroll{overflow-x:auto}footer{color:var(--muted);font-size:12px;text-align:center;margin-top:24px}
@media (max-width:760px){.kpis{grid-template-columns:repeat(2,1fr)}.two{grid-template-columns:1fr}header{flex-direction:column}}
@media print{body{background:#fff}main{padding:0}.card,.kpi{border-color:#ddd}.kpis{grid-template-columns:repeat(4,1fr)}.two{grid-template-columns:1fr 1fr}@page{margin:14mm}}
`
