package index

import (
	"slices"
	"testing"

	"github.com/Aset-Mk/geomatch/internal/geo"
)

var queryPoint = geo.Point{Lat: 55.75, Lon: 37.61}

func ids(ns []Neighbor) []DriverID {
	out := make([]DriverID, len(ns))
	for i, n := range ns {
		out[i] = n.ID
	}
	return out
}

func TestNearestOrder(t *testing.T) {
	idx := NewNaive()
	idx.Upsert(3, geo.Point{Lat: 55.78, Lon: 37.61})
	idx.Upsert(1, geo.Point{Lat: 55.751, Lon: 37.61})
	idx.Upsert(2, geo.Point{Lat: 55.76, Lon: 37.61})
	got := ids(idx.Nearest(queryPoint, 3))
	want := []DriverID{1, 2, 3}

	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Also covers fewer drivers than k.
func TestUpsertUpdates(t *testing.T) {
	idx := NewNaive()
	idx.Upsert(1, geo.Point{Lat: 56.0, Lon: 37.61})
	idx.Upsert(1, queryPoint)

	got := idx.Nearest(queryPoint, 5)
	if len(got) != 1 {
		t.Fatalf("got %d drivers, want 1", len(got))
	}
	if got[0].Dist != 0 {
		t.Fatalf("got dist %f, want 0", got[0].Dist)
	}
}

func TestRemove(t *testing.T) {
	idx := NewNaive()
	idx.Upsert(1, queryPoint)
	idx.Remove(1)

	if got := idx.Nearest(queryPoint, 5); len(got) != 0 {
		t.Fatalf("got %v, want empty", ids(got))
	}
}

func TestRemoveNonExistent(t *testing.T) {
	idx := NewNaive()
	idx.Upsert(1, queryPoint)
	idx.Remove(2)

	if got := idx.Nearest(queryPoint, 5); len(got) != 1 {
		t.Fatalf("got %v, want 1", ids(got))
	}
}

func TestNearestEmpty(t *testing.T) {
	idx := NewNaive()

	if got := idx.Nearest(queryPoint, 5); len(got) != 0 {
		t.Fatalf("got %v, want empty", ids(got))
	}
}

func TestNearestNonPositiveK(t *testing.T) {
	idx := NewNaive()
	idx.Upsert(1, queryPoint)

	for _, k := range []int{0, -1} {
		if got := idx.Nearest(queryPoint, k); len(got) != 0 {
			t.Fatalf("k=%d: got %v, want empty", k, ids(got))
		}
	}
}
