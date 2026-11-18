package client

import (
	"context"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc"

	"github.com/Aset-Mk/geomatch/internal/index"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
)

// Fleet sends every driver over its own update stream, like the phones of real drivers
type Fleet struct {
	conns   []pb.MatcherClient
	streams []grpc.ClientStreamingClient[pb.UpdateDriversRequest, pb.UpdateDriversResponse] // by driver id

	mu     sync.Mutex
	err    error
	failed atomic.Uint64
}

// NewFleet serves drivers with ids from 0 to drivers-1, driver i goes over conns[i%len(conns)]
func NewFleet(drivers int, conns ...grpc.ClientConnInterface) *Fleet {
	f := &Fleet{
		conns:   make([]pb.MatcherClient, len(conns)),
		streams: make([]grpc.ClientStreamingClient[pb.UpdateDriversRequest, pb.UpdateDriversResponse], drivers),
	}
	for i, conn := range conns {
		f.conns[i] = pb.NewMatcherClient(conn)
	}
	return f
}

// UpsertBatch may run at once from several goroutines as long as they send different drivers
func (f *Fleet) UpsertBatch(us []index.Update) {
	for _, u := range us {
		st := f.streams[u.ID]
		if st == nil {
			var err error
			st, err = f.conns[int(u.ID)%len(f.conns)].UpdateDrivers(context.Background())
			if err != nil {
				f.fail(err)
				continue
			}
			f.streams[u.ID] = st
		}
		err := st.Send(&pb.UpdateDriversRequest{Updates: []*pb.DriverUpdate{{
			DriverId: uint64(u.ID),
			Pos:      &pb.Point{Lat: u.Pos.Lat, Lon: u.Pos.Lon},
		}}})
		if err != nil {
			_, err = st.CloseAndRecv()
			f.streams[u.ID] = nil
			f.fail(err)
		}
	}
}

// Close ends every stream and returns the first error seen
func (f *Fleet) Close() error {
	const workers = 64
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := w; i < len(f.streams); i += workers {
				if st := f.streams[i]; st != nil {
					_, err := st.CloseAndRecv()
					f.streams[i] = nil
					f.fail(err)
				}
			}
		})
	}
	wg.Wait()

	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

// Failed returns how many calls have failed so far
func (f *Fleet) Failed() uint64 {
	return f.failed.Load()
}

func (f *Fleet) fail(err error) {
	if err == nil {
		return
	}
	f.failed.Add(1)
	f.mu.Lock()
	if f.err == nil {
		f.err = err
	}
	f.mu.Unlock()
}
