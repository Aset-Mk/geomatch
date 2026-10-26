package index

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/Aset-Mk/geomatch/internal/geo"
)

func randomPoint(r *rand.Rand, center geo.Point) geo.Point {
	return geo.Point{Lat: center.Lat + 0.4*(r.Float64()-0.5), Lon: center.Lon + 0.4*(r.Float64()-0.5)}
}

func checkSame(t *testing.T, got, want []Neighbor, k int, q geo.Point) {
	t.Helper()
	g, w := ids(got), ids(want)
	if slices.Equal(g, w) {
		return
	}
	i := 0
	for i < len(g) && i < len(w) && g[i] == w[i] {
		i++
	}
	t.Fatalf("k=%d, query %v: got %d neighbors, want %d; first difference at %d: got %v, want %v",
		k, q, len(g), len(w), i, g[i:min(i+5, len(g))], w[i:min(i+5, len(w))])
}

func TestGridMatchesNaive(t *testing.T) {
	moscow, origin := geo.Point{Lat: 55.75, Lon: 37.61}, geo.Point{}
	for _, tc := range []struct {
		size    float64
		drivers int
		center  geo.Point
	}{
		{100, 2000, moscow},
		{500, 2000, moscow},
		{2000, 2000, moscow},
		{500, 3, moscow},
		{200, 300, moscow},
		{500, 2000, origin},
	} {
		t.Run(fmt.Sprintf("size=%v/drivers=%d/center=%v", tc.size, tc.drivers, tc.center), func(t *testing.T) {
			r := rand.New(rand.NewPCG(1, 1))
			grid, naive := NewGrid(tc.size, tc.center.Lat), NewNaive()
			upsert := func(id DriverID, p geo.Point) {
				grid.Upsert(id, p)
				naive.Upsert(id, p)
			}

			for i := range tc.drivers {
				upsert(DriverID(i), randomPoint(r, tc.center))
			}
			for i := range tc.drivers / 2 {
				id := DriverID(r.IntN(tc.drivers))
				if i%4 == 0 {
					grid.Remove(id)
					naive.Remove(id)
				} else {
					upsert(id, randomPoint(r, tc.center))
				}
			}

			for range 200 {
				q := randomPoint(r, tc.center)
				for _, k := range []int{1, 5, 50, tc.drivers + 1} {
					checkSame(t, grid.Nearest(q, k), naive.Nearest(q, k), k, q)
				}
			}
		})
	}
}

func TestGridNeighborCellCloser(t *testing.T) {
	g := NewGrid(500, 55.75)
	q := geo.Point{Lat: 55.75, Lon: 37.61}
	c := g.cellOf(q)

	edge := float64(c.col+1)*g.cellLon - 1e-6
	q.Lon = edge
	for i := range 5 {
		g.Upsert(DriverID(i), geo.Offset(q, 0, -400-float64(i)))
	}
	g.Upsert(99, geo.Offset(q, 0, 10))

	if got := g.Nearest(q, 1); len(got) != 1 || got[0].ID != 99 {
		t.Fatalf("got %v, want [99]", ids(got))
	}
}

func TestGridDropsEmptyCells(t *testing.T) {
	g := NewGrid(500, 55.75)
	g.Upsert(1, queryPoint)
	g.Upsert(1, geo.Offset(queryPoint, 5000, 0))
	g.Upsert(2, queryPoint)
	g.Remove(2)
	g.Remove(1)

	if len(g.cells) != 0 {
		t.Fatalf("got %d cells, want 0", len(g.cells))
	}
}
