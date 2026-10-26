package index_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/load"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

const benchDrivers = 100_000

type benchImpl struct {
	name string
	new  func() index.Index
}

func benchImpls() []benchImpl {
	lat := (sim.CityBounds.Min.Lat + sim.CityBounds.Max.Lat) / 2
	impls := []benchImpl{{"naive", func() index.Index { return index.NewNaive() }}}
	for _, size := range []float64{100, 150, 200, 250, 300, 350, 400, 500, 1000, 2000} {
		impls = append(impls, benchImpl{
			fmt.Sprintf("grid/%v", size),
			func() index.Index { return index.NewGrid(size, lat) },
		})
	}
	return impls
}

func filled(impl benchImpl) (index.Index, []sim.Driver, *rand.Rand) {
	r := rand.New(rand.NewPCG(1, 1))
	idx := impl.new()
	drivers := load.NewDrivers(benchDrivers, sim.CityBounds, r)
	for _, d := range drivers {
		idx.Upsert(d.ID, d.Pos)
	}
	return idx, drivers, r
}

func BenchmarkNearest(b *testing.B) {
	for _, impl := range benchImpls() {
		b.Run(impl.name, func(b *testing.B) {
			idx, _, r := filled(impl)
			for b.Loop() {
				idx.Nearest(sim.CityBounds.RandomPoint(r), 5)
			}
		})
	}
}

func BenchmarkNearestFewDrivers(b *testing.B) {
	lat := (sim.CityBounds.Min.Lat + sim.CityBounds.Max.Lat) / 2
	for _, n := range []int{10, 100, 300, 1000, 3000, 10_000} {
		b.Run(fmt.Sprintf("drivers=%d", n), func(b *testing.B) {
			r := rand.New(rand.NewPCG(1, 1))
			idx := index.NewGrid(200, lat)
			for _, d := range load.NewDrivers(n, sim.CityBounds, r) {
				idx.Upsert(d.ID, d.Pos)
			}
			for b.Loop() {
				idx.Nearest(sim.CityBounds.RandomPoint(r), 5)
			}
		})
	}
}

func BenchmarkUpsert(b *testing.B) {
	for _, impl := range benchImpls() {
		b.Run(impl.name, func(b *testing.B) {
			idx, drivers, r := filled(impl)
			i := 0
			for b.Loop() {
				d := &drivers[i%len(drivers)]
				d.Step(2, sim.CityBounds, r)
				idx.Upsert(d.ID, d.Pos)
				i++
			}
		})
	}
}
