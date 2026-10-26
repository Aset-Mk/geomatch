package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type loadgenConfig struct {
	Drivers       int
	UpdatePeriod  time.Duration
	OrderRate     int // orders per second
	Duration      time.Duration
	Seed          uint64
	Out           string
	CPUProfile    string  // empty disables profiling
	Index         string  // naive or grid
	CellSize      float64 // grid cell side, meters
	OrderWorkers  int
	DriverWorkers int
	UpdateBatch   int
	UpdateLinger  time.Duration
}

func loadLoadgenConfig() (loadgenConfig, error) {
	var errs []error
	cfg := loadgenConfig{
		Drivers:       env("DRIVERS", 100000, strconv.Atoi, &errs),
		UpdatePeriod:  env("UPDATE_PERIOD", 2*time.Second, time.ParseDuration, &errs),
		OrderRate:     env("ORDER_RATE", 1000, strconv.Atoi, &errs),
		Duration:      env("DURATION", 30*time.Second, time.ParseDuration, &errs),
		Seed:          env("SEED", 1, parseUint, &errs),
		Out:           env("OUT", "results.json", parseString, &errs),
		CPUProfile:    env("CPU_PROFILE", "", parseString, &errs),
		Index:         env("INDEX", "naive", parseString, &errs),
		CellSize:      env("CELL_SIZE", 200.0, parseFloat, &errs),
		OrderWorkers:  env("ORDER_WORKERS", 1, strconv.Atoi, &errs),
		DriverWorkers: env("DRIVER_WORKERS", 1, strconv.Atoi, &errs),
		UpdateBatch:   env("UPDATE_BATCH", 1, strconv.Atoi, &errs),
		UpdateLinger:  env("UPDATE_LINGER", 5*time.Millisecond, time.ParseDuration, &errs),
	}

	if cfg.Drivers <= 0 {
		errs = append(errs, errors.New("DRIVERS must be > 0"))
	}
	if cfg.OrderRate <= 0 {
		errs = append(errs, errors.New("ORDER_RATE must be > 0"))
	}
	if cfg.UpdateBatch <= 0 {
		errs = append(errs, errors.New("UPDATE_BATCH must be > 0"))
	}
	if cfg.UpdateLinger < 0 {
		errs = append(errs, errors.New("UPDATE_LINGER must be >= 0"))
	}
	if cfg.OrderWorkers <= 0 || cfg.DriverWorkers <= 0 {
		errs = append(errs, errors.New("ORDER_WORKERS and DRIVER_WORKERS must be > 0"))
	} else {
		if cfg.OrderRate%cfg.OrderWorkers != 0 {
			errs = append(errs, errors.New("ORDER_RATE must be divisible by ORDER_WORKERS"))
		}
		if cfg.Drivers < cfg.DriverWorkers {
			errs = append(errs, errors.New("DRIVERS must be >= DRIVER_WORKERS"))
		}
	}
	if cfg.Index != "naive" && cfg.Index != "grid" {
		errs = append(errs, fmt.Errorf("INDEX must be naive or grid, got %q", cfg.Index))
	}
	if cfg.CellSize <= 0 {
		errs = append(errs, errors.New("CELL_SIZE must be > 0"))
	}
	if cfg.UpdatePeriod <= 0 || cfg.Duration <= 0 {
		errs = append(errs, errors.New("UPDATE_PERIOD and DURATION must be > 0"))
	}

	// RunDrivers spaces updates by UpdatePeriod/Drivers, which must not round to 0
	if cfg.UpdatePeriod < time.Duration(cfg.Drivers) {
		errs = append(errs, errors.New("UPDATE_PERIOD too short for DRIVERS"))
	}
	return cfg, errors.Join(errs...)
}

// env returns def if name is unset and appends parse errors to errs
func env[T any](name string, def T, parse func(string) (T, error), errs *[]error) T {
	s, ok := os.LookupEnv(name)
	if !ok {
		return def
	}
	v, err := parse(s)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: %w", name, err))
		return def
	}
	return v
}

func parseUint(s string) (uint64, error)   { return strconv.ParseUint(s, 10, 64) }
func parseFloat(s string) (float64, error) { return strconv.ParseFloat(s, 64) }
func parseString(s string) (string, error) { return s, nil }
