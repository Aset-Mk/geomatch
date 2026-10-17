package load

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

// NewDrivers places drivers at random locations within region b, assigning them random directions of movement
func NewDrivers(n int, b sim.Bounds, r *rand.Rand) []sim.Driver {
	drivers := make([]sim.Driver, n)
	for i := range drivers {
		drivers[i] = sim.Driver{
			ID:      index.DriverID(i),
			Pos:     b.RandomPoint(r),
			Heading: r.Float64() * 2 * math.Pi,
			Speed:   10,
		}
	}
	return drivers
}

// RunDrivers executes each driver once per period and adds it to the index for the duration
func RunDrivers(idx index.Index, drivers []sim.Driver, b sim.Bounds, r *rand.Rand, period, dur time.Duration) {
	interval := period / time.Duration(len(drivers))
	n := int(dur / interval)
	start := time.Now()

	for i := range n {
		scheduled := start.Add(time.Duration(i) * interval)
		if wait := time.Until(scheduled); wait > 0 {
			time.Sleep(wait)
		}

		d := &drivers[i%len(drivers)]
		d.Step(period.Seconds(), b, r)
		idx.Upsert(d.ID, d.Pos)
	}
}
