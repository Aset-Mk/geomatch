package load

import (
	"math/rand/v2"
	"time"

	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

// RunOrders calls idx.Nearest rate times per second for dur
func RunOrders(idx index.Index, b sim.Bounds, r *rand.Rand, rate int, dur time.Duration, record func(time.Duration)) {
	interval := time.Second / time.Duration(rate)
	n := int(dur / interval)
	start := time.Now()

	for i := range n {
		scheduled := start.Add(time.Duration(i) * interval)
		waitUntil(scheduled)

		idx.Nearest(b.RandomPoint(r), 5)
		record(time.Since(scheduled))
	}
}

// time.Sleep can wake up 1ms late, so the last spinWindow before t is busy-waited
const spinWindow = 2 * time.Millisecond

func waitUntil(t time.Time) {
	if d := time.Until(t) - spinWindow; d > 0 {
		time.Sleep(d)
	}
	for time.Now().Before(t) {
	}
}
