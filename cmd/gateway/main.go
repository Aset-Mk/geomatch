package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/twmb/franz-go/pkg/kgo"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/Aset-Mk/geomatch/internal/config"
	"github.com/Aset-Mk/geomatch/internal/feed"
	"github.com/Aset-Mk/geomatch/internal/gateway"
	"github.com/Aset-Mk/geomatch/internal/logging"
	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
	"github.com/Aset-Mk/geomatch/internal/serve"
)

func main() {
	probe := flag.Bool("health", false, "ask a running gateway for its health and exit")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid config", "err", err)
		os.Exit(1)
	}

	logger := logging.New(cfg.LogLevel)
	slog.SetDefault(logger)

	gcfg, err := loadGatewayConfig()
	if err != nil {
		slog.Error("invalid gateway config", "err", err)
		os.Exit(1)
	}

	if *probe {
		if err := serve.Check(gcfg.ListenAddr); err != nil {
			slog.Error("health", "err", err)
			os.Exit(1)
		}
		return
	}

	lis, err := net.Listen("tcp", gcfg.ListenAddr)
	if err != nil {
		slog.Error("listen", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("gateway started", "addr", lis.Addr().String(), "metrics_addr", gcfg.MetricsAddr, "max_conns", gcfg.MaxConns, "kafka_brokers", gcfg.KafkaBrokers, "kafka_topic", gcfg.KafkaTopic, "kafka_linger", gcfg.KafkaLinger.String())
	if err := run(ctx, lis, gcfg); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
	slog.Info("gateway stopped")
}

// run serves on lis until ctx is done, then lets open streams finish and sends what Kafka has not got yet
func run(ctx context.Context, lis net.Listener, cfg gatewayConfig) error {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.KafkaBrokers...),
		kgo.DefaultProduceTopic(cfg.KafkaTopic),
		kgo.ProducerLinger(cfg.KafkaLinger),
	)
	if err != nil {
		return err
	}
	defer cl.Close()

	svc := serve.New(cfg.Config)
	pb.RegisterMatcherServer(svc.GRPC, gateway.New(cl, cfg.KafkaTopic))

	// clients wait until the topic exists
	svc.Health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	go func() {
		if err := feed.EnsureTopic(ctx, cl, cfg.KafkaTopic, cfg.KafkaPartitions); err != nil {
			slog.Error("create topic", "err", err)
			return
		}
		svc.Health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	}()

	if err := svc.Run(ctx, lis); err != nil {
		return err
	}
	flush, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return cl.Flush(flush)
}
