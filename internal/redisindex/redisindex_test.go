package redisindex_test

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/redisindex"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

func open(t *testing.T, radius float64) (*redisindex.Index, *miniredis.Miniredis) {
	srv := miniredis.RunT(t)
	x, closeRedis, err := redisindex.Open(srv.Addr(), "drivers", radius)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(closeRedis)
	return x, srv
}

func TestMatchesNaive(t *testing.T) {
	x, _ := open(t, 1000)
	b := sim.CityBounds
	want := index.NewNaive()
	r := rand.New(rand.NewPCG(1, 1))
	us := make([]index.Update, 2000)
	for i := range us {
		us[i] = index.Update{ID: index.DriverID(i), Pos: b.RandomPoint(r)}
	}
	want.UpsertBatch(us)
	x.UpsertBatch(us)
	for i := range 100 {
		p := b.RandomPoint(r)
		want.Upsert(index.DriverID(i), p)
		x.Upsert(index.DriverID(i), p)
	}
	want.Remove(7)
	x.Remove(7)
	if got := x.Nearest(us[7].Pos, 1); len(got) == 1 && got[0].ID == 7 {
		t.Fatal("driver 7 is still found after Remove")
	}

	for range 200 {
		p := b.RandomPoint(r)
		got, exp := x.Nearest(p, 5), want.Nearest(p, 5)
		if len(got) != len(exp) {
			t.Fatalf("Nearest(%v) returned %d drivers, want %d", p, len(got), len(exp))
		}
		// Redis keeps a position as a 52 bit geohash, under a meter off
		for i := range exp {
			if math.Abs(got[i].Dist-exp[i].Dist) > 1 {
				t.Fatalf("Nearest(%v)[%d] = driver %d at %.1f m, want driver %d at %.1f m", p, i, got[i].ID, got[i].Dist, exp[i].ID, exp[i].Dist)
			}
		}
	}
	if err := x.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
}

func TestNearestWidensRadius(t *testing.T) {
	x, _ := open(t, 100)
	p := geo.Point{Lat: 55.75, Lon: 37.61}
	x.Upsert(1, geo.Offset(p, 50, 0))
	x.Upsert(2, geo.Offset(p, 3000, 0))
	x.Upsert(3, geo.Offset(p, 0, 80000))

	got := x.Nearest(p, 3)
	if len(got) != 3 || got[0].ID != 1 || got[1].ID != 2 || got[2].ID != 3 {
		t.Fatalf("Nearest = %v, want drivers 1, 2, 3", got)
	}
	if got := x.Nearest(p, 10); len(got) != 3 {
		t.Fatalf("Nearest with k above the number of drivers returned %d, want all 3", len(got))
	}
}

func TestRedisFailureIsKept(t *testing.T) {
	x, srv := open(t, 1000)
	srv.Close()
	if got := x.Nearest(geo.Point{Lat: 55.75, Lon: 37.61}, 5); got != nil {
		t.Errorf("Nearest with Redis down = %v, want nil", got)
	}
	if x.Err() == nil {
		t.Fatal("Err is nil after Redis went down")
	}
}
