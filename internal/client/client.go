// Package client is an index.Index that talks to the matcher over gRPC
package client

import (
	"context"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc"

	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
)

// Client keeps the first error because index.Index methods cannot return one
type Client struct {
	c     pb.MatcherClient // carries updates and removals
	conns []pb.MatcherClient
	next  atomic.Uint64

	mu     sync.Mutex
	stream grpc.ClientStreamingClient[pb.UpdateDriversRequest, pb.UpdateDriversResponse]
	err    error
	failed atomic.Uint64
}

var _ index.Index = (*Client)(nil)

// New spreads Nearest calls over conns in turn
func New(conns ...grpc.ClientConnInterface) *Client {
	c := &Client{conns: make([]pb.MatcherClient, len(conns))}
	for i, conn := range conns {
		c.conns[i] = pb.NewMatcherClient(conn)
	}
	c.c = c.conns[0]
	return c
}

func (c *Client) Upsert(id index.DriverID, p geo.Point) {
	c.UpsertBatch([]index.Update{{ID: id, Pos: p}})
}

func (c *Client) UpsertBatch(us []index.Update) {
	req := &pb.UpdateDriversRequest{Updates: make([]*pb.DriverUpdate, len(us))}
	for i, u := range us {
		req.Updates[i] = &pb.DriverUpdate{
			DriverId: uint64(u.ID),
			Pos:      &pb.Point{Lat: u.Pos.Lat, Lon: u.Pos.Lon},
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stream == nil {
		stream, err := c.c.UpdateDrivers(context.Background())
		if err != nil {
			c.fail(err)
			return
		}
		c.stream = stream
	}
	if err := c.stream.Send(req); err != nil {
		// Send reports io.EOF when the server ended the stream, the cause comes from CloseAndRecv
		_, err = c.stream.CloseAndRecv()
		c.stream = nil
		c.fail(err)
	}
}

func (c *Client) Remove(id index.DriverID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// a removal is a separate call and would overtake updates still in the stream
	c.closeStream()
	if _, err := c.c.RemoveDriver(context.Background(), &pb.RemoveDriverRequest{DriverId: uint64(id)}); err != nil {
		c.fail(err)
	}
}

func (c *Client) Nearest(p geo.Point, k int) []index.Neighbor {
	if k <= 0 {
		return nil
	}
	conn := c.conns[c.next.Add(1)%uint64(len(c.conns))]
	resp, err := conn.Nearest(context.Background(), &pb.NearestRequest{
		Point: &pb.Point{Lat: p.Lat, Lon: p.Lon},
		K:     uint32(k),
	})
	if err != nil {
		c.mu.Lock()
		c.fail(err)
		c.mu.Unlock()
		return nil
	}

	found := make([]index.Neighbor, len(resp.GetNeighbors()))
	for i, n := range resp.GetNeighbors() {
		found[i] = index.Neighbor{ID: index.DriverID(n.GetDriverId()), Dist: n.GetDistanceM()}
	}
	return found
}

// Flush waits until the matcher has applied every update sent so far and returns the first error seen
func (c *Client) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closeStream()
	return c.err
}

func (c *Client) closeStream() {
	if c.stream != nil {
		_, err := c.stream.CloseAndRecv()
		c.stream = nil
		c.fail(err)
	}
}

// Failed returns how many calls have failed so far
func (c *Client) Failed() uint64 {
	return c.failed.Load()
}

func (c *Client) fail(err error) {
	if err == nil {
		return
	}
	c.failed.Add(1)
	if c.err == nil {
		c.err = err
	}
}
