package load

import (
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

// stallOnce is an index whose first Nearest call stalls
type stallOnce struct {
	stall time.Duration
	calls int
}

func (s *stallOnce) Upsert(index.DriverID, geo.Point) {}
func (s *stallOnce) UpsertBatch([]index.Update)       {}
func (s *stallOnce) Remove(index.DriverID)            {}
func (s *stallOnce) Nearest(geo.Point, int) []index.Neighbor {
	s.calls++
	if s.calls == 1 {
		time.Sleep(s.stall)
	}
	return nil
}

func TestRunOrdersCountsStall(t *testing.T) {
	idx := &stallOnce{stall: 50 * time.Millisecond}
	r := rand.New(rand.NewPCG(1, 1))

	var lat []time.Duration
	RunOrders(t.Context(), idx, sim.CityBounds, r, 1000, 100*time.Millisecond, func(d time.Duration) {
		lat = append(lat, d)
	})

	if len(lat) != 100 {
		t.Fatalf("got %d orders, want 100", len(lat))
	}

	slow := 0
	for _, d := range lat {
		if d >= 10*time.Millisecond {
			slow++
		}
	}

	// a 50ms stall at one order per ms delays orders 0 to 40 by 10ms or more
	if slow < 40 {
		t.Fatalf("got %d orders slower than 10ms, want >= 30", slow)
	}
}

type noop struct{}

func (noop) Upsert(index.DriverID, geo.Point)        {}
func (noop) UpsertBatch([]index.Update)              {}
func (noop) Remove(index.DriverID)                   {}
func (noop) Nearest(geo.Point, int) []index.Neighbor { return nil }

func TestRunOrdersKeepsSchedule(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 1))
	var lat []time.Duration
	RunOrders(t.Context(), noop{}, sim.CityBounds, r, 200, 500*time.Millisecond, func(d time.Duration) {
		lat = append(lat, d)
	})

	slices.Sort(lat)
	if p50 := lat[len(lat)/2]; p50 > 50*time.Microsecond {
		t.Fatalf("p50 = %v for an index that does nothing, want <= 50µs", p50)
	}
}

// busy is an index whose Nearest takes a while and counts calls running at once
type busy struct {
	noop
	mu            sync.Mutex
	running, peak int
}

func (b *busy) Nearest(geo.Point, int) []index.Neighbor {
	b.mu.Lock()
	b.running++
	b.peak = max(b.peak, b.running)
	b.mu.Unlock()

	time.Sleep(20 * time.Millisecond)

	b.mu.Lock()
	b.running--
	b.mu.Unlock()
	return nil
}

func TestRunOrdersAsyncLimitsInflight(t *testing.T) {
	idx := &busy{}
	r := rand.New(rand.NewPCG(1, 1))

	n := 0
	RunOrdersAsync(t.Context(), idx, sim.CityBounds, r, 1000, 100*time.Millisecond, 5, func(time.Duration) { n++ })

	if n != 100 {
		t.Errorf("recorded %d orders, want 100", n)
	}
	// one order per ms against a 20ms answer would reach 20 at once without the limit
	if idx.peak != 5 {
		t.Errorf("%d orders waited for an answer at once, want 5", idx.peak)
	}
}
