package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/Aset-Mk/geomatch/internal/config"
	"github.com/Aset-Mk/geomatch/internal/feed"
	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/logging"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
	"github.com/Aset-Mk/geomatch/internal/redisindex"
	"github.com/Aset-Mk/geomatch/internal/serve"
	"github.com/Aset-Mk/geomatch/internal/server"
	"github.com/Aset-Mk/geomatch/internal/sim"
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
		if err := serve.Check(mcfg.ListenAddr); err != nil {
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

	slog.Info("matcher started", "addr", lis.Addr().String(), "metrics_addr", mcfg.MetricsAddr, "index", mcfg.Index, "cell_size", mcfg.CellSize, "driver_ttl", mcfg.DriverTTL.String(), "max_conns", mcfg.MaxConns, "kafka_brokers", mcfg.KafkaBrokers, "kafka_topic", mcfg.KafkaTopic)
	if err := run(ctx, lis, mcfg); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
	slog.Info("matcher stopped")
}

// run serves on lis until ctx is done, then lets open requests finish
func run(ctx context.Context, lis net.Listener, cfg matcherConfig) error {
	b := sim.CityBounds
	var idx index.Index = index.NewGrid(cfg.CellSize, (b.Min.Lat+b.Max.Lat)/2)
	if cfg.Index == "redis" {
		rdb, closeRedis, err := redisindex.Open(cfg.RedisAddr, "drivers", cfg.RedisRadius)
		if err != nil {
			return fmt.Errorf("redis: %w", err)
		}
		defer closeRedis()
		idx = rdb
	}
	m := server.New(idx)

	svc := serve.New(cfg.Config)
	pb.RegisterMatcherServer(svc.GRPC, m)

	// a broken Kafka stops the matcher as a signal does
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

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

	if len(cfg.KafkaBrokers) > 0 {
		// orders wait until the index holds every driver that was on the line before the start
		svc.Health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
		began := time.Now()
		ready := func() {
			slog.Info("index caught up with kafka", "took", time.Since(began).String())
			svc.Health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
		}
		go func() {
			if err := follow(ctx, m, cfg, ready); err != nil {
				cancel(fmt.Errorf("kafka: %w", err))
			}
		}()
	}

	if err := svc.Run(ctx, lis); err != nil {
		return err
	}
	if err := context.Cause(ctx); !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// follow creates the topic if the matcher is the first to start, then reads it
func follow(ctx context.Context, m *server.Server, cfg matcherConfig, ready func()) error {
	opts := []kgo.Opt{kgo.SeedBrokers(cfg.KafkaBrokers...)}
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return err
	}
	err = feed.EnsureTopic(ctx, cl, cfg.KafkaTopic, cfg.KafkaPartitions)
	cl.Close()
	if err != nil {
		return err
	}
	return m.Follow(ctx, cfg.KafkaTopic, cfg.DriverTTL, ready, opts...)
}
