package sim

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/Aset-Mk/geomatch/internal/geo"
)

func TestStepStaysInBounds(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 1))
	b := CityBounds
	d := Driver{Pos: geo.Point{Lat: b.Max.Lat, Lon: b.Max.Lon}, Heading: 0, Speed: 10}

	for i := range 10_000 {
		d.Step(2, b, r)

		lat, lon := d.Pos.Lat, d.Pos.Lon
		latOK := lat >= b.Min.Lat && lat <= b.Max.Lat
		lonOK := lon >= b.Min.Lon && lon <= b.Max.Lon
		if !latOK || !lonOK {
			t.Fatalf("step %d: position %+v out of bounds [%+v, %+v]", i, d.Pos, b.Min, b.Max)
		}
	}
}

func TestStepBouncesOffEdges(t *testing.T) {
	b := CityBounds
	midLat := (b.Min.Lat + b.Max.Lat) / 2
	midLon := (b.Min.Lon + b.Max.Lon) / 2

	tests := []struct {
		name    string
		pos     geo.Point
		heading float64
		onEdge  func(p geo.Point) bool
	}{
		{"north", geo.Point{Lat: b.Max.Lat, Lon: midLon}, 0,
			func(p geo.Point) bool { return p.Lat >= b.Max.Lat }},
		{"south", geo.Point{Lat: b.Min.Lat, Lon: midLon}, math.Pi,
			func(p geo.Point) bool { return p.Lat <= b.Min.Lat }},
		{"east", geo.Point{Lat: midLat, Lon: b.Max.Lon}, math.Pi / 2,
			func(p geo.Point) bool { return p.Lon >= b.Max.Lon }},
		{"west", geo.Point{Lat: midLat, Lon: b.Min.Lon}, -math.Pi / 2,
			func(p geo.Point) bool { return p.Lon <= b.Min.Lon }},
	}

	const maxEdgeSteps = 10

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rand.New(rand.NewPCG(1, 1))
			d := Driver{Pos: tt.pos, Heading: tt.heading, Speed: 10}

			atEdge := 0
			for range 100 {
				d.Step(2, b, r)
				if tt.onEdge(d.Pos) {
					atEdge++
				}
			}

			if atEdge > maxEdgeSteps {
				t.Fatalf("driver stuck at %s edge: %d of 100 steps, pos %+v", tt.name, atEdge, d.Pos)
			}
		})
	}
}
