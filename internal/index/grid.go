package index

import (
	"cmp"
	"math"
	"slices"
	"sync"

	"github.com/Aset-Mk/geomatch/internal/geo"
)

// Grid is an Index that buckets drivers into square cells and searches outward from the query cell
type Grid struct {
	mu      sync.Mutex
	cellLat float64 // degrees
	cellLon float64 // degrees
	latSide float64 // meters
	pos     map[DriverID]geo.Point
	cells   map[cell]map[DriverID]geo.Point

	// rows and cols ever occupied; they only grow and guarantee the search ends
	minRow, maxRow, minCol, maxCol int32
}

type cell struct{ row, col int32 }

var _ Index = (*Grid)(nil)

// NewGrid returns an empty Grid with cells of size meters, square at latitude lat
func NewGrid(size, lat float64) *Grid {
	corner := geo.Offset(geo.Point{Lat: lat}, size, size)
	cellLat := corner.Lat - lat
	return &Grid{
		cellLat: cellLat,
		cellLon: corner.Lon,
		latSide: geo.Distance(geo.Point{}, geo.Point{Lat: cellLat}),
		pos:     make(map[DriverID]geo.Point),
		cells:   make(map[cell]map[DriverID]geo.Point),
		minRow:  math.MaxInt32,
		maxRow:  math.MinInt32,
		minCol:  math.MaxInt32,
		maxCol:  math.MinInt32,
	}
}

func (g *Grid) Upsert(id DriverID, p geo.Point) {
	g.mu.Lock()
	defer g.mu.Unlock()

	c := g.cellOf(p)
	if old, ok := g.pos[id]; ok {
		if oc := g.cellOf(old); oc != c {
			g.drop(oc, id)
		}
	}
	m := g.cells[c]
	if m == nil {
		m = make(map[DriverID]geo.Point)
		g.cells[c] = m
	}
	m[id] = p
	g.pos[id] = p

	g.minRow, g.maxRow = min(g.minRow, c.row), max(g.maxRow, c.row)
	g.minCol, g.maxCol = min(g.minCol, c.col), max(g.maxCol, c.col)
}

func (g *Grid) Remove(id DriverID) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if old, ok := g.pos[id]; ok {
		g.drop(g.cellOf(old), id)
		delete(g.pos, id)
	}
}

func (g *Grid) Nearest(p geo.Point, k int) []Neighbor {
	if k <= 0 {
		return nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	best := make([]Neighbor, 0, min(k, len(g.pos))+1)
	seen := 0
	visit := func(c cell) {
		for id, dp := range g.cells[c] {
			seen++
			best = pushNearest(best, k, Neighbor{ID: id, Dist: geo.Distance(p, dp)})
		}
	}

	c := g.cellOf(p)
	for r := int32(0); seen < len(g.pos); r++ {
		if r == 0 {
			visit(c)
		} else {
			for col := c.col - r; col <= c.col+r; col++ {
				visit(cell{c.row - r, col})
				visit(cell{c.row + r, col})
			}
			for row := c.row - r + 1; row < c.row+r; row++ {
				visit(cell{row, c.col - r})
				visit(cell{row, c.col + r})
			}
		}
		// Every driver not seen yet is outside the searched square, so at least r cell sides away.
		if len(best) == k && best[k-1].Dist <= float64(r)*g.minSide(p, r) {
			break
		}
		if c.row-r <= g.minRow && c.row+r >= g.maxRow && c.col-r <= g.minCol && c.col+r >= g.maxCol {
			break
		}
	}
	return best
}

func (g *Grid) cellOf(p geo.Point) cell {
	return cell{
		row: int32(math.Floor(p.Lat / g.cellLat)),
		col: int32(math.Floor(p.Lon / g.cellLon)),
	}
}

func (g *Grid) drop(c cell, id DriverID) {
	m := g.cells[c]
	delete(m, id)
	if len(m) == 0 {
		delete(g.cells, c)
	}
}

// minSide is the narrowest cell side in meters within r rings of p, taken at the latitude farthest from the equator
func (g *Grid) minSide(p geo.Point, r int32) float64 {
	lat := min(math.Abs(p.Lat)+float64(r+1)*g.cellLat, 90)
	lonSide := geo.Distance(geo.Point{Lat: lat}, geo.Point{Lat: lat, Lon: g.cellLon})
	return 0.99 * min(g.latSide, lonSide)
}

// pushNearest inserts n into best, which is sorted by distance and holds at most k neighbors
func pushNearest(best []Neighbor, k int, n Neighbor) []Neighbor {
	if len(best) == k && n.Dist >= best[k-1].Dist {
		return best
	}
	i, _ := slices.BinarySearchFunc(best, n.Dist, func(b Neighbor, d float64) int {
		return cmp.Compare(b.Dist, d)
	})
	best = slices.Insert(best, i, n)
	if len(best) > k {
		best = best[:k]
	}
	return best
}
