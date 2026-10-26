package load

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

type upsertCounter struct {
	counts  map[index.DriverID]int
	last    map[index.DriverID]geo.Point
	batches int
}

func (c *upsertCounter) Upsert(id index.DriverID, p geo.Point) {
	c.counts[id]++
	c.last[id] = p
}
func (c *upsertCounter) UpsertBatch(us []index.Update) {
	c.batches++
	for _, u := range us {
		c.Upsert(u.ID, u.Pos)
	}
}
func (c *upsertCounter) Remove(index.DriverID)                   {}
func (c *upsertCounter) Nearest(geo.Point, int) []index.Neighbor { return nil }

func TestRunDriversUpdatesEachTwice(t *testing.T) {
	for _, batch := range []int{1, 3} {
		t.Run(fmt.Sprintf("batch=%d", batch), func(t *testing.T) {
			testRunDriversUpdatesEachTwice(t, batch)
		})
	}
}

func testRunDriversUpdatesEachTwice(t *testing.T, batch int) {
	r := rand.New(rand.NewPCG(1, 1))
	drivers := NewDrivers(10, sim.CityBounds, r)
	start := make([]geo.Point, len(drivers))
	for i, d := range drivers {
		start[i] = d.Pos
	}

	c := &upsertCounter{counts: map[index.DriverID]int{}, last: map[index.DriverID]geo.Point{}}
	RunDrivers(c, drivers, sim.CityBounds, r, 100*time.Millisecond, 200*time.Millisecond, batch, time.Hour)

	for i, d := range drivers {
		if got := c.counts[d.ID]; got != 2 {
			t.Errorf("driver %d: %d upserts, want 2", d.ID, got)
		}
		if c.last[d.ID] == start[i] {
			t.Errorf("driver %d did not move", d.ID)
		}
	}
}

func TestRunDriversLingerFlushesSparseUpdates(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 1))
	drivers := NewDrivers(3, sim.CityBounds, r)
	c := &upsertCounter{counts: map[index.DriverID]int{}, last: map[index.DriverID]geo.Point{}}

	// one update every 100ms, a batch of 100 never fills, so each update goes alone after linger
	RunDrivers(c, drivers, sim.CityBounds, r, 300*time.Millisecond, 300*time.Millisecond, 100, 20*time.Millisecond)

	if c.batches != 3 {
		t.Fatalf("got %d batches, want 3", c.batches)
	}
}
