package server

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

func fill(s *Server, now time.Time) {
	r := rand.New(rand.NewPCG(1, 1))
	for i := range 100_000 {
		id := index.DriverID(i)
		s.idx.Upsert(id, sim.CityBounds.RandomPoint(r))
		s.seen[id] = now
	}
}

func BenchmarkDropSilent(b *testing.B) {
	lat := (sim.CityBounds.Min.Lat + sim.CityBounds.Max.Lat) / 2
	now := time.Now()

	b.Run("silent=0", func(b *testing.B) {
		s := New(index.NewGrid(200, lat))
		fill(s, now)
		for b.Loop() {
			s.DropSilent(now.Add(-time.Second))
		}
	})
	b.Run("silent=100000", func(b *testing.B) {
		s := New(index.NewGrid(200, lat))
		for b.Loop() {
			b.StopTimer()
			fill(s, now)
			b.StartTimer()
			s.DropSilent(now.Add(time.Second))
		}
	})
}
