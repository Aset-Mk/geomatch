package geo

import (
	"math"
	"testing"
)

func TestDistanceOneDegreeLat(t *testing.T) {
	got := Distance(Point{0, 0}, Point{1, 0})
	want := 111195.0

	if math.Abs(got-want) > 1 {
		t.Fatalf("got %.0f m, want %.0f m", got, want)
	}
}

func TestDistanceSamePoint(t *testing.T) {
	if got := Distance(Point{55.75, 37.61}, Point{55.75, 37.61}); got != 0 {
		t.Fatalf("got %f m, want 0", got)
	}
}

func TestOffsetDistance(t *testing.T) {
	p := Point{Lat: 55.75, Lon: 37.61}

	for _, d := range []struct{ north, east float64 }{
		{500, 0}, {0, 500}, {300, 400},
	} {
		got := Distance(p, Offset(p, d.north, d.east))
		want := math.Hypot(d.north, d.east)
		if math.Abs(got-want) > 1 {
			t.Errorf("offset %v: got %.1f m, want %.1f m", d, got, want)
		}
	}
}
