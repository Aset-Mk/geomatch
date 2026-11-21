package main

import (
	"errors"
	"strconv"
	"time"

	"github.com/Aset-Mk/geomatch/internal/config"
	"github.com/Aset-Mk/geomatch/internal/serve"
)

type gatewayConfig struct {
	ListenAddr      string
	KafkaBrokers    []string
	KafkaTopic      string
	KafkaPartitions int32         // partitions of a topic the gateway creates, a topic that exists keeps its own
	KafkaLinger     time.Duration // how long a partition waits for more records before a batch goes out
	serve.Config
}

func loadGatewayConfig() (gatewayConfig, error) {
	var errs []error
	cfg := gatewayConfig{
		ListenAddr:      config.Env("LISTEN_ADDR", ":50052", config.String, &errs),
		KafkaBrokers:    config.Env("KAFKA_BROKERS", []string{"localhost:9094"}, config.List, &errs),
		KafkaTopic:      config.Env("KAFKA_TOPIC", "driver-positions", config.String, &errs),
		KafkaPartitions: config.Env("KAFKA_PARTITIONS", int32(8), config.Int32, &errs),
		KafkaLinger:     config.Env("KAFKA_LINGER", 10*time.Millisecond, time.ParseDuration, &errs),
		Config: serve.Config{
			ShutdownTimeout: config.Env("SHUTDOWN_TIMEOUT", 5*time.Second, time.ParseDuration, &errs),
			MaxConns:        config.Env("MAX_CONNS", 150000, strconv.Atoi, &errs),
			MetricsAddr:     config.Env("METRICS_ADDR", ":2113", config.String, &errs),
		},
	}

	if len(cfg.KafkaBrokers) == 0 {
		errs = append(errs, errors.New("KAFKA_BROKERS must not be empty"))
	}
	if cfg.KafkaPartitions <= 0 {
		errs = append(errs, errors.New("KAFKA_PARTITIONS must be > 0"))
	}
	if cfg.KafkaLinger < 0 {
		errs = append(errs, errors.New("KAFKA_LINGER must be >= 0"))
	}
	if cfg.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("SHUTDOWN_TIMEOUT must be > 0"))
	}
	if cfg.MaxConns <= 0 {
		errs = append(errs, errors.New("MAX_CONNS must be > 0"))
	}
	return cfg, errors.Join(errs...)
}
