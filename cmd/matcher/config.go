package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type matcherConfig struct {
	ListenAddr string
	CellSize   float64       // grid cell side, meters
	DriverTTL  time.Duration // a driver silent for longer is removed
}

func loadMatcherConfig() (matcherConfig, error) {
	cfg := matcherConfig{ListenAddr: ":50051", CellSize: 200, DriverTTL: 10 * time.Second}

	if v, ok := os.LookupEnv("LISTEN_ADDR"); ok {
		cfg.ListenAddr = v
	}
	if v, ok := os.LookupEnv("CELL_SIZE"); ok {
		size, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return matcherConfig{}, fmt.Errorf("CELL_SIZE: %w", err)
		}
		cfg.CellSize = size
	}
	if v, ok := os.LookupEnv("DRIVER_TTL"); ok {
		ttl, err := time.ParseDuration(v)
		if err != nil {
			return matcherConfig{}, fmt.Errorf("DRIVER_TTL: %w", err)
		}
		cfg.DriverTTL = ttl
	}
	if cfg.CellSize <= 0 {
		return matcherConfig{}, errors.New("CELL_SIZE must be > 0")
	}
	if cfg.DriverTTL <= 0 {
		return matcherConfig{}, errors.New("DRIVER_TTL must be > 0")
	}
	return cfg, nil
}
