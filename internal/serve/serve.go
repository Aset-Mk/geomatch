// Package serve runs a gRPC service the same way for every binary
package serve

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
)

// maxStreams caps requests running at once on one connection, so a client that never reads its answers cannot eat the memory
const maxStreams = 100

const (
	// a client that connects and stays silent is dropped after handshakeTimeout
	handshakeTimeout = 5 * time.Second
	// a connection quiet for pingAfter gets a ping and is closed without an answer in pingTimeout
	pingAfter   = 30 * time.Second
	pingTimeout = 10 * time.Second
)

type Config struct {
	MaxConns        int           // connections above it are closed on arrival
	MetricsAddr     string        // empty serves no metrics
	ShutdownTimeout time.Duration // how long a stop waits for open requests
}

// Service is a gRPC server with health, metrics and a connection limit
type Service struct {
	GRPC   *grpc.Server
	Health *health.Server
	cfg    Config
	limit  *Limit
}

func New(cfg Config) *Service {
	limit := NewLimit(cfg.MaxConns)
	// a pool of handlers saves growing the stack of a new goroutine on every request
	srv := grpc.NewServer(
		grpc.MaxConcurrentStreams(maxStreams),
		grpc.NumStreamWorkers(uint32(runtime.NumCPU())),
		grpc.ConnectionTimeout(handshakeTimeout),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: pingAfter, Timeout: pingTimeout}),
		grpc.UnaryInterceptor(Measure),
		grpc.StatsHandler(limit),
	)
	hs := health.NewServer()
	healthpb.RegisterHealthServer(srv, hs)
	return &Service{GRPC: srv, Health: hs, cfg: cfg, limit: limit}
}

// Run serves on lis until ctx is done, then lets open requests finish
func (s *Service) Run(ctx context.Context, lis net.Listener) error {
	var metrics *http.Server
	if s.cfg.MetricsAddr != "" {
		mlis, err := net.Listen("tcp", s.cfg.MetricsAddr)
		if err != nil {
			return err
		}
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		metrics = &http.Server{Handler: mux, ReadHeaderTimeout: handshakeTimeout}
		go func() { _ = metrics.Serve(mlis) }()
	}

	go func() {
		<-ctx.Done()
		slog.Info("stopping")
		s.Health.Shutdown()
		// update streams do not end on their own, so the wait has a limit
		t := time.AfterFunc(s.cfg.ShutdownTimeout, s.GRPC.Stop)
		defer t.Stop()
		s.GRPC.GracefulStop()
		if metrics != nil {
			_ = metrics.Close()
		}
	}()

	return s.GRPC.Serve(s.limit.Listener(lis))
}

// Check asks the service at addr whether it serves, containers run it as their health check
func Check(addr string) error {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("service is %v", resp.GetStatus())
	}
	return nil
}
