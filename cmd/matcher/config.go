package main

import (
	"errors"
	"strconv"
	"time"

	"github.com/Aset-Mk/geomatch/internal/config"
	"github.com/Aset-Mk/geomatch/internal/serve"
)

type matcherConfig struct {
	ListenAddr   string
	CellSize     float64       // grid cell side, meters
	DriverTTL    time.Duration // a driver silent for longer is removed
	KafkaBrokers []string      // empty takes driver positions only over gRPC
	KafkaTopic   string
	// partitions of a topic the matcher creates, a topic that exists keeps its own
	KafkaPartitions int32
	serve.Config
}

func loadMatcherConfig() (matcherConfig, error) {
	var errs []error
	cfg := matcherConfig{
		ListenAddr:      config.Env("LISTEN_ADDR", ":50051", config.String, &errs),
		CellSize:        config.Env("CELL_SIZE", 200.0, config.Float, &errs),
		DriverTTL:       config.Env("DRIVER_TTL", 10*time.Second, time.ParseDuration, &errs),
		KafkaBrokers:    config.Env("KAFKA_BROKERS", nil, config.List, &errs),
		KafkaTopic:      config.Env("KAFKA_TOPIC", "driver-positions", config.String, &errs),
		KafkaPartitions: config.Env("KAFKA_PARTITIONS", int32(8), config.Int32, &errs),
		Config: serve.Config{
			ShutdownTimeout: config.Env("SHUTDOWN_TIMEOUT", 5*time.Second, time.ParseDuration, &errs),
			MaxConns:        config.Env("MAX_CONNS", 150000, strconv.Atoi, &errs),
			MetricsAddr:     config.Env("METRICS_ADDR", ":2112", config.String, &errs),
		},
	}

	if cfg.CellSize <= 0 {
		errs = append(errs, errors.New("CELL_SIZE must be > 0"))
	}
	if cfg.DriverTTL <= 0 {
		errs = append(errs, errors.New("DRIVER_TTL must be > 0"))
	}
	if cfg.KafkaPartitions <= 0 {
		errs = append(errs, errors.New("KAFKA_PARTITIONS must be > 0"))
	}
	if cfg.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("SHUTDOWN_TIMEOUT must be > 0"))
	}
	if cfg.MaxConns <= 0 {
		errs = append(errs, errors.New("MAX_CONNS must be > 0"))
	}
	return cfg, errors.Join(errs...)
}
