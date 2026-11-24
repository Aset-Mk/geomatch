// Package redisindex keeps drivers in a Redis geo set behind the same interface as the local index
package redisindex

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/redis/rueidis"

	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
)

// maxRadius is half the equator, a search this wide sees every driver on Earth
const maxRadius = 20_037_508.0

// grow is how much the radius widens when it holds fewer than k drivers
const grow = 4

// timeout caps every command, without it rueidis retries a read while Redis is down forever
const timeout = time.Second

// Index answers Nearest with GEOSEARCH, starting from radius and widening it until k drivers are inside
type Index struct {
	c      rueidis.Client
	key    string
	radius float64

	mu  sync.Mutex
	err error
}

var _ index.Index = (*Index)(nil)

// New keeps drivers under key, a search starts from radius meters
func New(c rueidis.Client, key string, radius float64) *Index {
	return &Index{c: c, key: key, radius: radius}
}

// Open connects to Redis at addr and returns the index and a close function
func Open(addr, key string, radius float64) (*Index, func(), error) {
	c, err := rueidis.NewClient(rueidis.ClientOption{InitAddress: []string{addr}, DisableCache: true})
	if err != nil {
		return nil, nil, err
	}
	return New(c, key, radius), c.Close, nil
}

func (x *Index) Upsert(id index.DriverID, p geo.Point) {
	x.UpsertBatch([]index.Update{{ID: id, Pos: p}})
}

// UpsertBatch sends the whole batch as one GEOADD
func (x *Index) UpsertBatch(us []index.Update) {
	if len(us) == 0 {
		return
	}
	cmd := x.c.B().Geoadd().Key(x.key).LongitudeLatitudeMember()
	for _, u := range us {
		cmd = cmd.LongitudeLatitudeMember(u.Pos.Lon, u.Pos.Lat, member(u.ID))
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	x.fail(x.c.Do(ctx, cmd.Build()).Error())
}

func (x *Index) Remove(id index.DriverID) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	x.fail(x.c.Do(ctx, x.c.B().Zrem().Key(x.key).Member(member(id)).Build()).Error())
}

// Nearest returns nil when Redis fails, the error waits in Err
func (x *Index) Nearest(p geo.Point, k int) []index.Neighbor {
	out, err := x.Search(p, k)
	if err != nil {
		x.fail(err)
		return nil
	}
	return out
}

// Search is like Nearest but returns the Redis error
func (x *Index) Search(p geo.Point, k int) ([]index.Neighbor, error) {
	if k <= 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var locs []rueidis.GeoLocation
	for r := x.radius; ; r *= grow {
		cmd := x.c.B().Geosearch().Key(x.key).Fromlonlat(p.Lon, p.Lat).Byradius(min(r, maxRadius)).M().Asc().Count(int64(k)).Withcoord().Build()
		var err error
		locs, err = x.c.Do(ctx, cmd).AsGeosearch()
		if err != nil {
			return nil, err
		}
		if len(locs) >= k || r >= maxRadius {
			break
		}
	}

	// Redis uses another Earth radius, distances are recounted to match the local index
	out := make([]index.Neighbor, 0, len(locs))
	for _, l := range locs {
		id, err := strconv.ParseUint(l.Name, 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, index.Neighbor{ID: index.DriverID(id), Dist: geo.Distance(p, geo.Point{Lat: l.Latitude, Lon: l.Longitude})})
	}
	return out, nil
}

// Err returns the first error from Redis
func (x *Index) Err() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.err
}

func (x *Index) fail(err error) {
	if err == nil {
		return
	}
	x.mu.Lock()
	if x.err == nil {
		x.err = err
	}
	x.mu.Unlock()
}

func member(id index.DriverID) string {
	return strconv.FormatUint(uint64(id), 10)
}
