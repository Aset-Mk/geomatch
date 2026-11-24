// Package server serves the index over gRPC
package server

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
)

// MaxK is the largest k Nearest accepts
const MaxK = 100

// maxApply is how many updates go into the index under one lock, so a huge message cannot stall searches
const maxApply = 100

// remote is an index behind the network, its search can fail
type remote interface {
	Search(p geo.Point, k int) ([]index.Neighbor, error)
}

// Server checks requests and passes them to the index, which trusts its input
type Server struct {
	pb.UnimplementedMatcherServer
	idx index.Index

	mu    sync.Mutex
	seen  map[index.DriverID]time.Time // time of the position of every driver in the index
	fresh []index.Update               // scratch for apply, under mu
}

// stamped is a position with the time it was sent
type stamped struct {
	index.Update
	at time.Time
}

func New(idx index.Index) *Server {
	return &Server{idx: idx, seen: make(map[index.DriverID]time.Time)}
}

func (s *Server) Nearest(_ context.Context, req *pb.NearestRequest) (*pb.NearestResponse, error) {
	p, err := point(req.GetPoint())
	if err != nil {
		return nil, err
	}
	if req.GetK() == 0 || req.GetK() > MaxK {
		return nil, status.Errorf(codes.InvalidArgument, "k must be from 1 to %d, got %d", MaxK, req.GetK())
	}

	var found []index.Neighbor
	if r, ok := s.idx.(remote); ok {
		found, err = r.Search(p, int(req.GetK()))
		if err != nil {
			return nil, status.Errorf(codes.Unavailable, "index: %v", err)
		}
	} else {
		found = s.idx.Nearest(p, int(req.GetK()))
	}
	resp := &pb.NearestResponse{Neighbors: make([]*pb.Neighbor, len(found))}
	for i, n := range found {
		resp.Neighbors[i] = &pb.Neighbor{DriverId: uint64(n.ID), DistanceM: n.Dist}
	}
	return resp, nil
}

func (s *Server) UpdateDrivers(stream grpc.ClientStreamingServer[pb.UpdateDriversRequest, pb.UpdateDriversResponse]) error {
	updateStreams.Inc()
	defer updateStreams.Dec()

	var batch []stamped
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&pb.UpdateDriversResponse{})
		}
		if err != nil {
			return err
		}

		batch = batch[:0]
		now := time.Now()
		for _, u := range req.GetUpdates() {
			p, err := point(u.GetPos())
			if err != nil {
				return err
			}
			batch = append(batch, stamped{index.Update{ID: index.DriverID(u.GetDriverId()), Pos: p}, now})
		}
		s.apply(batch)
	}
}

// apply puts positions into the index in chunks, a position older than the one in the index is skipped
func (s *Server) apply(us []stamped) {
	for chunk := range slices.Chunk(us, maxApply) {
		s.mu.Lock()
		s.fresh = s.fresh[:0]
		for _, u := range chunk {
			if u.at.Before(s.seen[u.ID]) {
				staleSkipped.Inc()
				continue
			}
			s.seen[u.ID] = u.at
			s.fresh = append(s.fresh, u.Update)
		}
		s.idx.UpsertBatch(s.fresh)
		driverUpdates.Add(float64(len(s.fresh)))
		s.mu.Unlock()
	}
}

func (s *Server) RemoveDriver(_ context.Context, req *pb.RemoveDriverRequest) (*pb.RemoveDriverResponse, error) {
	s.remove(index.DriverID(req.GetDriverId()))
	return &pb.RemoveDriverResponse{}, nil
}

func (s *Server) remove(id index.DriverID) {
	s.mu.Lock()
	delete(s.seen, id)
	s.idx.Remove(id)
	s.mu.Unlock()
}

// DropSilent removes drivers that sent nothing since cutoff and returns how many
func (s *Server) DropSilent(cutoff time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0
	for id, t := range s.seen {
		if t.Before(cutoff) {
			delete(s.seen, id)
			s.idx.Remove(id)
			n++
		}
	}
	silentRemoved.Add(float64(n))
	drivers.Set(float64(len(s.seen)))
	return n
}

func point(p *pb.Point) (geo.Point, error) {
	gp := geo.Point{Lat: p.GetLat(), Lon: p.GetLon()}
	if p == nil || !gp.Valid() {
		return geo.Point{}, status.Errorf(codes.InvalidArgument, "bad point %v", p)
	}
	return gp, nil
}
