package index

import (
	"cmp"
	"math"
	"slices"
	"sync"

	"github.com/Aset-Mk/geomatch/internal/geo"
)

// Grid is an Index that keeps drivers in square cells and searches rings of cells around the query
type Grid struct {
	mu      sync.RWMutex
	cellLat float64 // degrees
	cellLon float64 // degrees
	latSide float64 // meters
	pos     map[DriverID]geo.Point
	cells   map[cell]map[DriverID]geo.Point

	// rows and cols ever occupied, they only grow so the search always ends
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
	g.upsert(id, p)
}

func (g *Grid) UpsertBatch(us []Update) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, u := range us {
		g.upsert(u.ID, u.Pos)
	}
}

func (g *Grid) upsert(id DriverID, p geo.Point) {
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

	g.mu.RLock()
	defer g.mu.RUnlock()

	best := make([]Neighbor, 0, min(k, len(g.pos))+1)
	seen := 0
	visit := func(c cell) {
		for id, dp := range g.cells[c] {
			seen++
			best = pushNearest(best, k, Neighbor{ID: id, Dist: geo.Distance(p, dp)})
		}
	}

	if g.sparse(k) {
		return g.nearestAll(p, k)
	}

	c := g.cellOf(p)
	looked := 0
	for r := int32(0); seen < len(g.pos); r++ {
		// with few drivers most ring cells are empty, so past this point scanning all occupied cells is cheaper
		ring := max(1, 8*int(r))
		if looked+ring > len(g.cells) {
			return g.nearestAll(p, k)
		}
		looked += ring

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
		// drivers not seen yet are outside the searched square, at least r cell sides away
		if len(best) == k && best[k-1].Dist <= float64(r)*g.minSide(p, r) {
			break
		}
		if c.row-r <= g.minRow && c.row+r >= g.maxRow && c.col-r <= g.minCol && c.col+r >= g.maxCol {
			break
		}
	}
	return best
}

// sparse reports whether rings would visit more cells than are occupied, counting one ring past the k-th driver
func (g *Grid) sparse(k int) bool {
	box := int64(g.maxRow-g.minRow+1) * int64(g.maxCol-g.minCol+1)
	return 4*int64(k)*box > int64(len(g.pos))*int64(len(g.cells))
}

func (g *Grid) nearestAll(p geo.Point, k int) []Neighbor {
	best := make([]Neighbor, 0, min(k, len(g.pos))+1)
	for id, dp := range g.pos {
		best = pushNearest(best, k, Neighbor{ID: id, Dist: geo.Distance(p, dp)})
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

// minSide is the narrowest cell side in meters within r rings of p
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
