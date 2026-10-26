package sim

import (
	"math"
	"math/rand/v2"

	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
)

const maxTurn = 0.25 // maximum rotation per step in radians

// Driver is a simulated driver moving around the city
type Driver struct {
	ID      index.DriverID
	Pos     geo.Point
	Heading float64 // radians, 0 = north, pi/2 = east
	Speed   float64 // meters per second
}

// Step moves d for dt seconds, turning slightly and bouncing off the edges of b
func (d *Driver) Step(dt float64, b Bounds, r *rand.Rand) {
	d.Heading += (r.Float64()*2 - 1) * maxTurn

	dist := d.Speed * dt
	d.Pos = geo.Offset(d.Pos, dist*math.Cos(d.Heading), dist*math.Sin(d.Heading))

	if d.Pos.Lat < b.Min.Lat || d.Pos.Lat > b.Max.Lat {
		d.Heading = math.Pi - d.Heading
		d.Pos.Lat = min(max(d.Pos.Lat, b.Min.Lat), b.Max.Lat)
	}
	if d.Pos.Lon < b.Min.Lon || d.Pos.Lon > b.Max.Lon {
		d.Heading = -d.Heading
		d.Pos.Lon = min(max(d.Pos.Lon, b.Min.Lon), b.Max.Lon)
	}
}
