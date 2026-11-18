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

// Server checks requests and passes them to the index, which trusts its input
type Server struct {
	pb.UnimplementedMatcherServer
	idx index.Index

	mu   sync.Mutex
	seen map[index.DriverID]time.Time // last update of every driver in the index
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

	found := s.idx.Nearest(p, int(req.GetK()))
	resp := &pb.NearestResponse{Neighbors: make([]*pb.Neighbor, len(found))}
	for i, n := range found {
		resp.Neighbors[i] = &pb.Neighbor{DriverId: uint64(n.ID), DistanceM: n.Dist}
	}
	return resp, nil
}

func (s *Server) UpdateDrivers(stream grpc.ClientStreamingServer[pb.UpdateDriversRequest, pb.UpdateDriversResponse]) error {
	updateStreams.Inc()
	defer updateStreams.Dec()

	var batch []index.Update
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&pb.UpdateDriversResponse{})
		}
		if err != nil {
			return err
		}

		batch = batch[:0]
		for _, u := range req.GetUpdates() {
			p, err := point(u.GetPos())
			if err != nil {
				return err
			}
			batch = append(batch, index.Update{ID: index.DriverID(u.GetDriverId()), Pos: p})
		}
		now := time.Now()
		for chunk := range slices.Chunk(batch, maxApply) {
			s.mu.Lock()
			for _, u := range chunk {
				s.seen[u.ID] = now
			}
			s.idx.UpsertBatch(chunk)
			s.mu.Unlock()
		}
		driverUpdates.Add(float64(len(batch)))
	}
}

func (s *Server) RemoveDriver(_ context.Context, req *pb.RemoveDriverRequest) (*pb.RemoveDriverResponse, error) {
	id := index.DriverID(req.GetDriverId())
	s.mu.Lock()
	delete(s.seen, id)
	s.idx.Remove(id)
	s.mu.Unlock()
	return &pb.RemoveDriverResponse{}, nil
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
