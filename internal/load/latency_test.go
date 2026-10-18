package load

import (
	"testing"
	"time"
)

func TestRecorderClampsAndKeepsUnits(t *testing.T) {
	rec := NewRecorder()
	rec.Record(time.Millisecond)
	rec.Record(2 * time.Millisecond)
	rec.Record(75 * time.Second) // above maxLatency

	s := rec.Summary()
	if s.Count != 3 {
		t.Errorf("Count = %d, want 3", s.Count)
	}
	if s.P50 != 2000 {
		t.Errorf("P50 = %d µs, want 2000", s.P50)
	}
	if want := maxLatency.Microseconds(); s.Max < want || s.Max > want*1001/1000 {
		t.Errorf("Max = %d µs, want ~%d", s.Max, want)
	}
}
