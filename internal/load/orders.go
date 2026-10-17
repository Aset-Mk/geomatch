package load

import (
	"math/rand/v2"
	"time"

	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

// RunOrders calls idx.Nearest at a specified frequency over a duration of dur
func RunOrders(idx index.Index, b sim.Bounds, r *rand.Rand, rate int, dur time.Duration, record func(time.Duration)) {
	interval := time.Second / time.Duration(rate)
	n := int(dur / interval)
	start := time.Now()

	for i := range n {
		scheduled := start.Add(time.Duration(i) * interval)
		if wait := time.Until(scheduled); wait > 0 {
			time.Sleep(wait)
		}

		idx.Nearest(b.RandomPoint(r), 5)
		record(time.Since(scheduled))
	}
}
