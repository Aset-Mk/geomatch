package main

import (
	"errors"
	"strconv"
	"time"

	"github.com/Aset-Mk/geomatch/internal/config"
)

type matcherConfig struct {
	ListenAddr      string
	CellSize        float64       // grid cell side, meters
	DriverTTL       time.Duration // a driver silent for longer is removed
	ShutdownTimeout time.Duration // how long a stop waits for open requests
	MaxConns        int           // connections above it are closed on arrival
	MetricsAddr     string        // empty serves no metrics
}

func loadMatcherConfig() (matcherConfig, error) {
	var errs []error
	cfg := matcherConfig{
		ListenAddr:      config.Env("LISTEN_ADDR", ":50051", config.String, &errs),
		CellSize:        config.Env("CELL_SIZE", 200.0, config.Float, &errs),
		DriverTTL:       config.Env("DRIVER_TTL", 10*time.Second, time.ParseDuration, &errs),
		ShutdownTimeout: config.Env("SHUTDOWN_TIMEOUT", 5*time.Second, time.ParseDuration, &errs),
		MaxConns:        config.Env("MAX_CONNS", 150000, strconv.Atoi, &errs),
		MetricsAddr:     config.Env("METRICS_ADDR", ":2112", config.String, &errs),
	}

	if cfg.CellSize <= 0 {
		errs = append(errs, errors.New("CELL_SIZE must be > 0"))
	}
	if cfg.DriverTTL <= 0 {
		errs = append(errs, errors.New("DRIVER_TTL must be > 0"))
	}
	if cfg.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("SHUTDOWN_TIMEOUT must be > 0"))
	}
	if cfg.MaxConns <= 0 {
		errs = append(errs, errors.New("MAX_CONNS must be > 0"))
	}
	return cfg, errors.Join(errs...)
}
