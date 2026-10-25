package load

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

// stallOnce is an index for which the first call to the Nearest method is stalled
type stallOnce struct {
	stall time.Duration
	calls int
}

func (s *stallOnce) Upsert(index.DriverID, geo.Point) {}
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
	RunOrders(idx, sim.CityBounds, r, 1000, 100*time.Millisecond, func(d time.Duration) {
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

	// a delay of 50 ms with a flow rate of 1 request/ms results in requests 0 through 40 being delayed by 10 ms or more
	if slow < 40 {
		t.Fatalf("got %d orders slower than 10ms, want >= 30", slow)
	}
}

type noop struct{}

func (noop) Upsert(index.DriverID, geo.Point)        {}
func (noop) Remove(index.DriverID)                   {}
func (noop) Nearest(geo.Point, int) []index.Neighbor { return nil }

func TestRunOrdersKeepsSchedule(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 1))
	var lat []time.Duration
	RunOrders(noop{}, sim.CityBounds, r, 200, 500*time.Millisecond, func(d time.Duration) {
		lat = append(lat, d)
	})

	slices.Sort(lat)
	if p50 := lat[len(lat)/2]; p50 > 50*time.Microsecond {
		t.Fatalf("p50 = %v for an index that does nothing, want <= 50µs", p50)
	}
}
