package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Aset-Mk/geomatch/internal/config"
)

type loadgenConfig struct {
	Drivers       int
	UpdatePeriod  time.Duration
	OrderRate     int // orders per second
	Duration      time.Duration
	Seed          uint64
	Out           string
	CPUProfile    string  // empty disables profiling
	Index         string  // naive, grid or redis
	CellSize      float64 // grid cell side, meters
	RedisAddr     string
	RedisRadius   float64 // meters, a search widens from it until k drivers are found
	OrderWorkers  int
	OrderInflight int // unanswered orders per worker, 0 waits for each answer
	DriverWorkers int
	UpdateBatch   int
	UpdateLinger  time.Duration
	MatcherAddr   string // empty runs the index in this process
	MatcherConns  int
	MetricsAddr   string   // empty serves no metrics
	DriverConns   int      // 0 sends all drivers over one stream, otherwise each driver has its own stream
	DriverAddrs   []string // matcher addresses for driver connections, one address has only ~28k local ports
	Assign        bool     // orders go through the Orders API and this process answers the offers
	OfferDecline  float64  // share of offers declined
	OfferIgnore   float64  // share of offers left without an answer
	RideTime      time.Duration
}

func loadLoadgenConfig() (loadgenConfig, error) {
	var errs []error
	cfg := loadgenConfig{
		Drivers:       config.Env("DRIVERS", 100000, strconv.Atoi, &errs),
		UpdatePeriod:  config.Env("UPDATE_PERIOD", 2*time.Second, time.ParseDuration, &errs),
		OrderRate:     config.Env("ORDER_RATE", 1000, strconv.Atoi, &errs),
		Duration:      config.Env("DURATION", 30*time.Second, time.ParseDuration, &errs),
		Seed:          config.Env("SEED", 1, config.Uint, &errs),
		Out:           config.Env("OUT", "results.json", config.String, &errs),
		CPUProfile:    config.Env("CPU_PROFILE", "", config.String, &errs),
		Index:         config.Env("INDEX", "naive", config.String, &errs),
		CellSize:      config.Env("CELL_SIZE", 200.0, config.Float, &errs),
		RedisAddr:     config.Env("REDIS_ADDR", "localhost:6379", config.String, &errs),
		RedisRadius:   config.Env("REDIS_RADIUS", 250.0, config.Float, &errs),
		OrderWorkers:  config.Env("ORDER_WORKERS", 1, strconv.Atoi, &errs),
		OrderInflight: config.Env("ORDER_INFLIGHT", 0, strconv.Atoi, &errs),
		DriverWorkers: config.Env("DRIVER_WORKERS", 1, strconv.Atoi, &errs),
		UpdateBatch:   config.Env("UPDATE_BATCH", 1, strconv.Atoi, &errs),
		UpdateLinger:  config.Env("UPDATE_LINGER", 5*time.Millisecond, time.ParseDuration, &errs),
		MatcherAddr:   config.Env("MATCHER_ADDR", "", config.String, &errs),
		MatcherConns:  config.Env("MATCHER_CONNS", 1, strconv.Atoi, &errs),
		MetricsAddr:   config.Env("METRICS_ADDR", "", config.String, &errs),
		DriverConns:   config.Env("DRIVER_CONNS", 0, strconv.Atoi, &errs),
		Assign:        config.Env("ASSIGN", false, strconv.ParseBool, &errs),
		OfferDecline:  config.Env("OFFER_DECLINE", 0.0, config.Float, &errs),
		OfferIgnore:   config.Env("OFFER_IGNORE", 0.0, config.Float, &errs),
		RideTime:      config.Env("RIDE_TIME", 5*time.Second, time.ParseDuration, &errs),
	}

	cfg.DriverAddrs = strings.Split(config.Env("DRIVER_ADDRS", cfg.MatcherAddr, config.String, &errs), ",")
	if cfg.DriverConns < 0 {
		errs = append(errs, errors.New("DRIVER_CONNS must be >= 0"))
	}
	if cfg.DriverConns > 0 && cfg.MatcherAddr == "" {
		errs = append(errs, errors.New("DRIVER_CONNS needs MATCHER_ADDR"))
	}
	if cfg.DriverConns > cfg.Drivers {
		errs = append(errs, errors.New("DRIVER_CONNS must be <= DRIVERS"))
	}

	// drivers and orders can come from separate processes, as from phones and from the order service
	if cfg.Drivers < 0 || cfg.OrderRate < 0 {
		errs = append(errs, errors.New("DRIVERS and ORDER_RATE must be >= 0"))
	}
	if cfg.Drivers == 0 && cfg.OrderRate == 0 {
		errs = append(errs, errors.New("DRIVERS and ORDER_RATE cannot both be 0"))
	}
	if cfg.MatcherConns <= 0 {
		errs = append(errs, errors.New("MATCHER_CONNS must be > 0"))
	}
	if cfg.OrderInflight < 0 {
		errs = append(errs, errors.New("ORDER_INFLIGHT must be >= 0"))
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
		if cfg.Drivers > 0 && cfg.Drivers < cfg.DriverWorkers {
			errs = append(errs, errors.New("DRIVERS must be >= DRIVER_WORKERS"))
		}
	}
	if cfg.Index != "naive" && cfg.Index != "grid" && cfg.Index != "redis" {
		errs = append(errs, fmt.Errorf("INDEX must be naive, grid or redis, got %q", cfg.Index))
	}
	if cfg.Index == "redis" && cfg.MatcherAddr != "" {
		errs = append(errs, errors.New("INDEX=redis runs in this process, it cannot go with MATCHER_ADDR"))
	}
	if cfg.Assign && (cfg.MatcherAddr == "" || cfg.OrderInflight == 0) {
		errs = append(errs, errors.New("ASSIGN needs MATCHER_ADDR and ORDER_INFLIGHT"))
	}
	if cfg.OfferDecline < 0 || cfg.OfferIgnore < 0 || cfg.OfferDecline+cfg.OfferIgnore > 1 {
		errs = append(errs, errors.New("OFFER_DECLINE and OFFER_IGNORE must be >= 0 and add up to <= 1"))
	}
	if cfg.RideTime <= 0 {
		errs = append(errs, errors.New("RIDE_TIME must be > 0"))
	}
	if cfg.RedisRadius <= 0 {
		errs = append(errs, errors.New("REDIS_RADIUS must be > 0"))
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
