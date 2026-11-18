package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"

	"github.com/Aset-Mk/geomatch/internal/config"
	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/logging"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
	"github.com/Aset-Mk/geomatch/internal/server"
	"github.com/Aset-Mk/geomatch/internal/sim"
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

func main() {
	probe := flag.Bool("health", false, "ask a running matcher for its health and exit")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid config", "err", err)
		os.Exit(1)
	}

	logger := logging.New(cfg.LogLevel)
	slog.SetDefault(logger)

	mcfg, err := loadMatcherConfig()
	if err != nil {
		slog.Error("invalid matcher config", "err", err)
		os.Exit(1)
	}

	if *probe {
		if err := check(mcfg.ListenAddr); err != nil {
			slog.Error("health", "err", err)
			os.Exit(1)
		}
		return
	}

	lis, err := net.Listen("tcp", mcfg.ListenAddr)
	if err != nil {
		slog.Error("listen", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("matcher started", "addr", lis.Addr().String(), "metrics_addr", mcfg.MetricsAddr, "cell_size", mcfg.CellSize, "driver_ttl", mcfg.DriverTTL.String(), "max_conns", mcfg.MaxConns)
	if err := run(ctx, lis, mcfg); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
	slog.Info("matcher stopped")
}

// run serves on lis until ctx is done, then lets open requests finish
func run(ctx context.Context, lis net.Listener, cfg matcherConfig) error {
	var metrics *http.Server
	if cfg.MetricsAddr != "" {
		mlis, err := net.Listen("tcp", cfg.MetricsAddr)
		if err != nil {
			return err
		}
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		metrics = &http.Server{Handler: mux, ReadHeaderTimeout: handshakeTimeout}
		go func() { _ = metrics.Serve(mlis) }()
	}

	b := sim.CityBounds
	idx := index.NewGrid(cfg.CellSize, (b.Min.Lat+b.Max.Lat)/2)
	m := server.New(idx)

	limit := server.NewLimit(cfg.MaxConns)
	// a pool of handlers saves growing the stack of a new goroutine on every request
	srv := grpc.NewServer(
		grpc.MaxConcurrentStreams(maxStreams),
		grpc.NumStreamWorkers(uint32(runtime.NumCPU())),
		grpc.ConnectionTimeout(handshakeTimeout),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: pingAfter, Timeout: pingTimeout}),
		grpc.UnaryInterceptor(server.Measure),
		grpc.StatsHandler(limit),
	)
	pb.RegisterMatcherServer(srv, m)
	hs := health.NewServer()
	healthpb.RegisterHealthServer(srv, hs)

	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if n := m.DropSilent(time.Now().Add(-cfg.DriverTTL)); n > 0 {
					slog.Info("silent drivers removed", "count", n)
				}
			}
		}
	}()

	go func() {
		<-ctx.Done()
		slog.Info("matcher stopping")
		hs.Shutdown()
		// update streams do not end on their own, so the wait has a limit
		t := time.AfterFunc(cfg.ShutdownTimeout, srv.Stop)
		defer t.Stop()
		srv.GracefulStop()
		if metrics != nil {
			_ = metrics.Close()
		}
	}()

	return srv.Serve(limit.Listener(lis))
}

// check asks the matcher at addr whether it serves, containers run it as their health check
func check(addr string) error {
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
		return fmt.Errorf("matcher is %v", resp.GetStatus())
	}
	return nil
}
