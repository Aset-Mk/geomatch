package index

import "github.com/Aset-Mk/geomatch/internal/geo"

// DriverID identifies the driver
type DriverID uint64

// Neighbor is the driver found using Nearest
type Neighbor struct {
	ID   DriverID
	Dist float64 // meters
}

// Update is a new position of a driver
type Update struct {
	ID  DriverID
	Pos geo.Point
}

// Index stores the locations of drivers and finds the nearest ones
type Index interface {
	Upsert(id DriverID, p geo.Point)       // p must be valid
	UpsertBatch(us []Update)               // like Upsert for each, in order
	Remove(id DriverID)                    // no-op if id is absent
	Nearest(p geo.Point, k int) []Neighbor // up to k, sorted by distance
}
