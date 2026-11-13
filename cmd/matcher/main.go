package main

import (
	"log/slog"
	"net"
	"os"
	"runtime"
	"time"

	"google.golang.org/grpc"

	"github.com/Aset-Mk/geomatch/internal/config"
	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/logging"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
	"github.com/Aset-Mk/geomatch/internal/server"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

// maxStreams caps requests running at once on one connection, so a client that never reads its answers cannot eat the memory
const maxStreams = 100

func main() {
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

	lis, err := net.Listen("tcp", mcfg.ListenAddr)
	if err != nil {
		slog.Error("listen", "err", err)
		os.Exit(1)
	}

	b := sim.CityBounds
	idx := index.NewGrid(mcfg.CellSize, (b.Min.Lat+b.Max.Lat)/2)

	m := server.New(idx)
	go func() {
		for range time.Tick(time.Second) {
			if n := m.DropSilent(time.Now().Add(-mcfg.DriverTTL)); n > 0 {
				slog.Info("silent drivers removed", "count", n)
			}
		}
	}()

	// a pool of handlers saves growing the stack of a new goroutine on every request
	srv := grpc.NewServer(
		grpc.MaxConcurrentStreams(maxStreams),
		grpc.NumStreamWorkers(uint32(runtime.NumCPU())),
	)
	pb.RegisterMatcherServer(srv, m)

	slog.Info("matcher started", "addr", lis.Addr().String(), "cell_size", mcfg.CellSize, "driver_ttl", mcfg.DriverTTL.String())
	if err := srv.Serve(lis); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}
