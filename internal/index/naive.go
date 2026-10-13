package index

import (
	"cmp"
	"slices"
	"sync"

	"github.com/Aset-Mk/geomatch/internal/geo"
)

// Naive is an Index implemented using a "brute-force"
type Naive struct {
	mu      sync.Mutex
	drivers map[DriverID]geo.Point
}

var _ Index = (*Naive)(nil)

// NewNaive returns an empty Naive index
func NewNaive() *Naive {
	return &Naive{drivers: make(map[DriverID]geo.Point)}
}

func (n *Naive) Upsert(id DriverID, p geo.Point) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.drivers[id] = p
}

func (n *Naive) Remove(id DriverID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.drivers, id)
}

func (n *Naive) Nearest(p geo.Point, k int) []Neighbor {
	if k <= 0 {
		return nil
	}

	n.mu.Lock()
	all := make([]Neighbor, 0, len(n.drivers))
	for id, dp := range n.drivers {
		all = append(all, Neighbor{ID: id, Dist: geo.Distance(p, dp)})
	}
	n.mu.Unlock()

	slices.SortFunc(all, func(a, b Neighbor) int {
		return cmp.Compare(a.Dist, b.Dist)
	})

	return all[:min(k, len(all))]
}
