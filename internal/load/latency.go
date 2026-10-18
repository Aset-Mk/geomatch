package load

import (
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

// maxLatency is the maximum latency tracked by the histogram; values greater than this are capped at this limit
const maxLatency = time.Minute

// Recorder collects latency values in microseconds. It is not thread-safe
type Recorder struct {
	h *hdrhistogram.Histogram
}

// Summary contains latency percentiles in microseconds
type Summary struct {
	Count int64 `json:"count"`
	P50   int64 `json:"p50_us"`
	P99   int64 `json:"p99_us"`
	P999  int64 `json:"p999_us"`
	Max   int64 `json:"max_us"`
}

// NewRecorder returns a Recorder with a precision of 3 significant digits
func NewRecorder() *Recorder {
	return &Recorder{h: hdrhistogram.New(1, maxLatency.Microseconds(), 3)}
}

// Record adds one latency
func (r *Recorder) Record(d time.Duration) {
	// Cannot fail: d is clamped to the histogram range
	_ = r.h.RecordValue(min(d, maxLatency).Microseconds())
}

// Summary returns the count and percentiles recorded up to this point
func (r *Recorder) Summary() Summary {
	return Summary{
		Count: r.h.TotalCount(),
		P50:   r.h.ValueAtQuantile(50),
		P99:   r.h.ValueAtQuantile(99),
		P999:  r.h.ValueAtQuantile(99.9),
		Max:   r.h.Max(),
	}
}
