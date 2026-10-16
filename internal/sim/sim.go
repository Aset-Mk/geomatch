package sim

import (
	"math/rand/v2"

	"github.com/Aset-Mk/geomatch/internal/geo"
)

// Bounds is a rectangle that defines the boundaries of an area on a map
type Bounds struct {
	Min, Max geo.Point // southwest and northeast corners
}

// MoscowMKAD roughly covers the area inside MKAD
var MoscowMKAD = Bounds{
	Min: geo.Point{Lat: 55.57, Lon: 37.36},
	Max: geo.Point{Lat: 55.92, Lon: 37.86},
}

// RandomPoint returns a uniformly random point within b
func (b Bounds) RandomPoint(r *rand.Rand) geo.Point {
	return geo.Point{
		Lat: b.Min.Lat + r.Float64()*(b.Max.Lat-b.Min.Lat),
		Lon: b.Min.Lon + r.Float64()*(b.Max.Lon-b.Min.Lon),
	}
}
