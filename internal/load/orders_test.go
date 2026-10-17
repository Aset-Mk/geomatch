package load

import (
	"math/rand/v2"
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
	RunOrders(idx, sim.MoscowMKAD, r, 1000, 100*time.Millisecond, func(d time.Duration) {
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
