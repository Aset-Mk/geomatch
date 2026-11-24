package server_test

import (
	"context"
	"math"
	"math/rand/v2"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/Aset-Mk/geomatch/internal/client"
	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
	"github.com/Aset-Mk/geomatch/internal/redisindex"
	"github.com/Aset-Mk/geomatch/internal/server"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

// dial starts a server over an in-memory connection
func dial(t *testing.T, s pb.MatcherServer, opts ...grpc.ServerOption) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(opts...)
	pb.RegisterMatcherServer(srv, s)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestClientMatchesLocalIndex(t *testing.T) {
	b := sim.CityBounds
	lat := (b.Min.Lat + b.Max.Lat) / 2
	local := index.NewGrid(200, lat)
	remote := client.New(dial(t, server.New(index.NewGrid(200, lat))))

	r := rand.New(rand.NewPCG(1, 1))
	us := make([]index.Update, 2000)
	for i := range us {
		us[i] = index.Update{ID: index.DriverID(i), Pos: b.RandomPoint(r)}
	}
	local.UpsertBatch(us)
	remote.UpsertBatch(us[:1000])
	for _, u := range us[1000:] {
		remote.Upsert(u.ID, u.Pos)
	}
	local.Remove(7)
	remote.Remove(7)
	if err := remote.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	for range 100 {
		p := b.RandomPoint(r)
		got, want := remote.Nearest(p, 5), local.Nearest(p, 5)
		if len(got) != len(want) {
			t.Fatalf("Nearest(%v) returned %d drivers, want %d", p, len(got), len(want))
		}
		for i := range want {
			if got[i].ID != want[i].ID {
				t.Fatalf("Nearest(%v)[%d] = driver %d, want %d", p, i, got[i].ID, want[i].ID)
			}
		}
	}
	if err := remote.Flush(); err != nil {
		t.Fatalf("Flush after Nearest: %v", err)
	}
}

func TestRemoveAfterUpsert(t *testing.T) {
	idx := index.NewNaive()
	remote := client.New(dial(t, server.New(idx)))
	p := geo.Point{Lat: 55.7, Lon: 37.6}

	for i := range 100 {
		remote.Upsert(index.DriverID(i), p)
		remote.Remove(index.DriverID(i))
	}
	if err := remote.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if found := idx.Nearest(p, 100); len(found) != 0 {
		t.Errorf("%d drivers are still in the index after their removal", len(found))
	}
}

func TestNearestRejectsBadRequests(t *testing.T) {
	c := pb.NewMatcherClient(dial(t, server.New(index.NewNaive())))
	ok := &pb.Point{Lat: 55.7, Lon: 37.6}

	tests := []struct {
		name string
		req  *pb.NearestRequest
	}{
		{"no point", &pb.NearestRequest{K: 5}},
		{"lat out of range", &pb.NearestRequest{Point: &pb.Point{Lat: 91, Lon: 37.6}, K: 5}},
		{"lon out of range", &pb.NearestRequest{Point: &pb.Point{Lat: 55.7, Lon: -181}, K: 5}},
		{"nan", &pb.NearestRequest{Point: &pb.Point{Lat: math.NaN(), Lon: 37.6}, K: 5}},
		{"zero k", &pb.NearestRequest{Point: ok}},
		{"k too large", &pb.NearestRequest{Point: ok, K: server.MaxK + 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.Nearest(context.Background(), tt.req)
			if got := status.Code(err); got != codes.InvalidArgument {
				t.Errorf("code = %v, want InvalidArgument", got)
			}
		})
	}
}

func TestUpdateDriversRejectsBadPoint(t *testing.T) {
	idx := index.NewNaive()
	remote := client.New(dial(t, server.New(idx)))

	remote.Upsert(1, geo.Point{Lat: 55.7, Lon: 37.6})
	remote.Upsert(2, geo.Point{Lat: 955.7, Lon: 37.6})
	err := remote.Flush()
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("Flush code = %v, want InvalidArgument", got)
	}

	found := idx.Nearest(geo.Point{Lat: 55.7, Lon: 37.6}, 5)
	if len(found) != 1 || found[0].ID != 1 {
		t.Errorf("index holds %v, want only driver 1", found)
	}
}

func TestDropSilent(t *testing.T) {
	idx := index.NewNaive()
	srv := server.New(idx)
	remote := client.New(dial(t, srv))
	p := geo.Point{Lat: 55.7, Lon: 37.6}

	remote.Upsert(1, p)
	remote.Upsert(2, p)
	if err := remote.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	cutoff := time.Now()
	remote.Upsert(2, p)
	if err := remote.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if got := srv.DropSilent(cutoff); got != 1 {
		t.Errorf("DropSilent removed %d drivers, want 1", got)
	}
	found := idx.Nearest(p, 5)
	if len(found) != 1 || found[0].ID != 2 {
		t.Errorf("index holds %v, want only driver 2", found)
	}
}

// sizes is an index that remembers the size of every batch
type sizes struct {
	index.Index
	got []int
}

func (s *sizes) UpsertBatch(us []index.Update) {
	s.got = append(s.got, len(us))
}

func TestUpdateDriversSplitsBigMessage(t *testing.T) {
	idx := &sizes{}
	remote := client.New(dial(t, server.New(idx)))

	us := make([]index.Update, 250)
	for i := range us {
		us[i] = index.Update{ID: index.DriverID(i), Pos: geo.Point{Lat: 55.7, Lon: 37.6}}
	}
	remote.UpsertBatch(us)
	if err := remote.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	total := 0
	for _, n := range idx.got {
		if n > 100 {
			t.Errorf("index got a batch of %d updates, want <= 100", n)
		}
		total += n
	}
	if total != len(us) {
		t.Errorf("index got %d updates, want %d", total, len(us))
	}
}

func TestFleetMatchesLocalIndex(t *testing.T) {
	b := sim.CityBounds
	lat := (b.Min.Lat + b.Max.Lat) / 2
	local := index.NewGrid(200, lat)
	srv := server.New(index.NewGrid(200, lat))
	var streams atomic.Int64
	count := grpc.StreamInterceptor(func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		streams.Add(1)
		return h(srv, ss)
	})
	conns := []grpc.ClientConnInterface{dial(t, srv, count), dial(t, srv, count), dial(t, srv, count)}
	fleet := client.NewFleet(400, conns...)
	remote := client.New(conns[0])

	r := rand.New(rand.NewPCG(1, 1))
	// the second round moves every driver, only the last position must stay
	for range 2 {
		us := make([]index.Update, 400)
		for i := range us {
			us[i] = index.Update{ID: index.DriverID(i), Pos: b.RandomPoint(r)}
		}
		local.UpsertBatch(us)
		var wg sync.WaitGroup
		for part := range slices.Chunk(us, 100) {
			wg.Go(func() { fleet.UpsertBatch(part) })
		}
		wg.Wait()
	}
	if err := fleet.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := streams.Load(); got != 400 {
		t.Errorf("400 drivers opened %d streams, want one each", got)
	}

	for range 100 {
		p := b.RandomPoint(r)
		got, want := remote.Nearest(p, 5), local.Nearest(p, 5)
		if len(got) != len(want) {
			t.Fatalf("Nearest(%v) returned %d drivers, want %d", p, len(got), len(want))
		}
		for i := range want {
			if got[i].ID != want[i].ID {
				t.Fatalf("Nearest(%v)[%d] = driver %d, want %d", p, i, got[i].ID, want[i].ID)
			}
		}
	}
}

func TestNearestReportsRedisFailure(t *testing.T) {
	srv := miniredis.RunT(t)
	rdb, closeRedis, err := redisindex.Open(srv.Addr(), "drivers", 1000)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer closeRedis()
	c := pb.NewMatcherClient(dial(t, server.New(rdb)))
	srv.Close()

	_, err = c.Nearest(context.Background(), &pb.NearestRequest{Point: &pb.Point{Lat: 55.75, Lon: 37.61}, K: 5})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Nearest with Redis down returned %v, want Unavailable", err)
	}
}
