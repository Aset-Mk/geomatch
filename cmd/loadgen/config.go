package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type loadgenConfig struct {
	Drivers      int
	UpdatePeriod time.Duration
	OrderRate    int // orders per second
	Duration     time.Duration
	Seed         uint64
	Out          string
	CPUProfile   string // empty disables profiling
}

func loadLoadgenConfig() (loadgenConfig, error) {
	var errs []error
	cfg := loadgenConfig{
		Drivers:      env("DRIVERS", 100000, strconv.Atoi, &errs),
		UpdatePeriod: env("UPDATE_PERIOD", 2*time.Second, time.ParseDuration, &errs),
		OrderRate:    env("ORDER_RATE", 1000, strconv.Atoi, &errs),
		Duration:     env("DURATION", 30*time.Second, time.ParseDuration, &errs),
		Seed:         env("SEED", 1, parseUint, &errs),
		Out:          env("OUT", "results.json", parseString, &errs),
		CPUProfile:   env("CPU_PROFILE", "", parseString, &errs),
	}

	if cfg.Drivers <= 0 {
		errs = append(errs, errors.New("DRIVERS must be > 0"))
	}
	if cfg.OrderRate <= 0 {
		errs = append(errs, errors.New("ORDER_RATE must be > 0"))
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

// env returns def if name is unset; parse errors are appended to errs
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
func parseString(s string) (string, error) { return s, nil }
