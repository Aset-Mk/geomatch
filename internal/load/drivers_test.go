package load

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

type upsertCounter struct {
	counts map[index.DriverID]int
	last   map[index.DriverID]geo.Point
}

func (c *upsertCounter) Upsert(id index.DriverID, p geo.Point) {
	c.counts[id]++
	c.last[id] = p
}
func (c *upsertCounter) Remove(index.DriverID)                   {}
func (c *upsertCounter) Nearest(geo.Point, int) []index.Neighbor { return nil }

func TestRunDriversUpdatesEachTwice(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 1))
	drivers := NewDrivers(10, sim.MoscowMKAD, r)
	start := make([]geo.Point, len(drivers))
	for i, d := range drivers {
		start[i] = d.Pos
	}

	c := &upsertCounter{counts: map[index.DriverID]int{}, last: map[index.DriverID]geo.Point{}}
	RunDrivers(c, drivers, sim.MoscowMKAD, r, 100*time.Millisecond, 200*time.Millisecond)

	for i, d := range drivers {
		if got := c.counts[d.ID]; got != 2 {
			t.Errorf("driver %d: %d upserts, want 2", d.ID, got)
		}
		if c.last[d.ID] == start[i] {
			t.Errorf("driver %d did not move", d.ID)
		}
	}
}
