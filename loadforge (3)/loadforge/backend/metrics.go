package main

import (
	"math"
	"sort"
)

// Histogram is a log-bucketed latency histogram (≈1% relative precision)
// covering 10µs .. ~10min. Memory is constant regardless of request count.
type Histogram struct {
	counts []int64
	n      int64
	sum    float64
	min    float64
	max    float64
}

const (
	histMinMs  = 0.01
	histGrowth = 1.02
	histSize   = 1100
)

var logGrowth = math.Log(histGrowth)

func NewHistogram() *Histogram {
	return &Histogram{counts: make([]int64, histSize), min: math.MaxFloat64}
}

func bucketOf(ms float64) int {
	if ms <= histMinMs {
		return 0
	}
	i := int(math.Log(ms/histMinMs)/logGrowth) + 1
	if i >= histSize {
		i = histSize - 1
	}
	return i
}

func bucketUpper(i int) float64 { return histMinMs * math.Pow(histGrowth, float64(i)) }

func (h *Histogram) Add(ms float64) {
	h.counts[bucketOf(ms)]++
	h.n++
	h.sum += ms
	if ms < h.min {
		h.min = ms
	}
	if ms > h.max {
		h.max = ms
	}
}

func (h *Histogram) Merge(o *Histogram) {
	for i, c := range o.counts {
		h.counts[i] += c
	}
	h.n += o.n
	h.sum += o.sum
	if o.min < h.min {
		h.min = o.min
	}
	if o.max > h.max {
		h.max = o.max
	}
}

func (h *Histogram) Quantile(q float64) float64 {
	if h.n == 0 {
		return 0
	}
	target := int64(math.Ceil(q * float64(h.n)))
	if target < 1 {
		target = 1
	}
	var acc int64
	for i, c := range h.counts {
		acc += c
		if acc >= target {
			v := bucketUpper(i)
			if v > h.max {
				v = h.max
			}
			if v < h.min {
				v = h.min
			}
			return v
		}
	}
	return h.max
}

func (h *Histogram) Stats() LatencyStats {
	if h.n == 0 {
		return LatencyStats{}
	}
	return LatencyStats{
		Min: r2(h.min), Avg: r2(h.sum / float64(h.n)),
		P50: r2(h.Quantile(.50)), P75: r2(h.Quantile(.75)), P90: r2(h.Quantile(.90)),
		P95: r2(h.Quantile(.95)), P99: r2(h.Quantile(.99)), Max: r2(h.max),
	}
}

// Buckets collapses the fine histogram into ~human-friendly bins for charting.
func (h *Histogram) Buckets() []HistBucket {
	edges := []float64{1, 2, 5, 10, 20, 50, 100, 200, 300, 500, 750, 1000, 1500, 2000, 3000, 5000, 10000, 30000, math.Inf(1)}
	out := make([]HistBucket, len(edges))
	for i := range edges {
		out[i].LeMs = edges[i]
	}
	for i, c := range h.counts {
		if c == 0 {
			continue
		}
		up := bucketUpper(i)
		j := sort.SearchFloat64s(edges, up)
		if j >= len(edges) {
			j = len(edges) - 1
		}
		out[j].Count += c
	}
	// trim empty tail but keep at least the populated range
	last := 0
	for i, b := range out {
		if b.Count > 0 {
			last = i
		}
	}
	out = out[:last+1]
	for i := range out {
		if math.IsInf(out[i].LeMs, 1) {
			out[i].LeMs = -1 // JSON-safe marker for "+Inf"
		}
	}
	return out
}

func r2(v float64) float64 { return math.Round(v*100) / 100 }
