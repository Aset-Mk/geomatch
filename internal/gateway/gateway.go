// Package gateway takes driver positions over gRPC and writes them to Kafka
package gateway

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Aset-Mk/geomatch/internal/feed"
	"github.com/Aset-Mk/geomatch/internal/geo"
	"github.com/Aset-Mk/geomatch/internal/index"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
)

var (
	written = promauto.NewCounter(prometheus.CounterOpts{
		Name: "geomatch_gateway_records_total",
		Help: "Driver records Kafka accepted",
	})
	writeErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "geomatch_gateway_record_errors_total",
		Help: "Driver records Kafka did not accept",
	})
	streams = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "geomatch_gateway_streams",
		Help: "Open streams of driver positions",
	})
)

// Producer is the part of a Kafka client the gateway needs
type Producer interface {
	Produce(ctx context.Context, r *kgo.Record, promise func(*kgo.Record, error))
}

// Gateway serves the driver calls of the matcher API, searches stay with the matcher
type Gateway struct {
	pb.UnimplementedMatcherServer
	kafka Producer
	topic string
}

func New(kafka Producer, topic string) *Gateway {
	return &Gateway{kafka: kafka, topic: topic}
}

// UpdateDrivers answers once Kafka has every position of the stream, so a client that got no error lost nothing
func (g *Gateway) UpdateDrivers(stream grpc.ClientStreamingServer[pb.UpdateDriversRequest, pb.UpdateDriversResponse]) error {
	streams.Inc()
	defer streams.Dec()

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	done := func(_ *kgo.Record, err error) {
		if err != nil {
			writeErrors.Inc()
			mu.Lock()
			if first == nil {
				first = err
			}
			mu.Unlock()
		} else {
			written.Inc()
		}
		wg.Done()
	}

	var recs []*kgo.Record
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			wg.Wait()
			if first != nil {
				return status.Errorf(codes.Unavailable, "kafka: %v", first)
			}
			return stream.SendAndClose(&pb.UpdateDriversResponse{})
		}
		if err != nil {
			return err
		}

		// a bad point rejects the whole message before anything of it is written
		recs = recs[:0]
		for _, u := range req.GetUpdates() {
			p := geo.Point{Lat: u.GetPos().GetLat(), Lon: u.GetPos().GetLon()}
			if u.GetPos() == nil || !p.Valid() {
				return status.Errorf(codes.InvalidArgument, "bad point %v", u.GetPos())
			}
			r, err := feed.Position(g.topic, index.DriverID(u.GetDriverId()), p)
			if err != nil {
				return status.Errorf(codes.Internal, "encode: %v", err)
			}
			recs = append(recs, r)
		}
		for _, r := range recs {
			wg.Add(1)
			g.kafka.Produce(stream.Context(), r, done)
		}
	}
}

// RemoveDriver goes through Kafka as well, otherwise it could overtake the last positions of the driver
func (g *Gateway) RemoveDriver(ctx context.Context, req *pb.RemoveDriverRequest) (*pb.RemoveDriverResponse, error) {
	errc := make(chan error, 1)
	g.kafka.Produce(ctx, feed.Removal(g.topic, index.DriverID(req.GetDriverId())), func(_ *kgo.Record, err error) { errc <- err })
	if err := <-errc; err != nil {
		writeErrors.Inc()
		return nil, status.Errorf(codes.Unavailable, "kafka: %v", err)
	}
	written.Inc()
	return &pb.RemoveDriverResponse{}, nil
}
