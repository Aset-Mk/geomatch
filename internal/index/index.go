package index

import "github.com/Aset-Mk/geomatch/internal/geo"

// DriverID identifies the driver
type DriverID uint64

// Neighbor is the driver found using Nearest
type Neighbor struct {
	ID   DriverID
	Dist float64 // meters
}

// Index stores the locations of drivers and finds the nearest ones
type Index interface {
	Upsert(id DriverID, p geo.Point)       // p must be valid
	Remove(id DriverID)                    // no-op if id is absent
	Nearest(p geo.Point, k int) []Neighbor // up to k, sorted by distance
}
