package load

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

// NewDrivers places n drivers at random points in b with random headings
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

// RunDrivers steps each driver once per period for dur and sends updates in batches of up to batch, waiting no longer than linger
func RunDrivers(idx index.Index, drivers []sim.Driver, b sim.Bounds, r *rand.Rand, period, dur time.Duration, batch int, linger time.Duration) {
	interval := period / time.Duration(len(drivers))
	n := int(dur / interval)
	start := time.Now()
	buf := make([]index.Update, 0, batch)
	var first time.Time
	flush := func() {
		idx.UpsertBatch(buf)
		buf = buf[:0]
	}

	for i := range n {
		scheduled := start.Add(time.Duration(i) * interval)
		if len(buf) > 0 && first.Add(linger).Before(scheduled) {
			sleepUntil(first.Add(linger))
			flush()
		}
		sleepUntil(scheduled)

		d := &drivers[i%len(drivers)]
		d.Step(period.Seconds(), b, r)
		if len(buf) == 0 {
			first = scheduled
		}
		buf = append(buf, index.Update{ID: d.ID, Pos: d.Pos})
		if len(buf) == batch || time.Since(first) >= linger {
			flush()
		}
	}
	if len(buf) > 0 {
		flush()
	}
}

func sleepUntil(t time.Time) {
	if d := time.Until(t); d > 0 {
		time.Sleep(d)
	}
}
