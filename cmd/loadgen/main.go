package main

import (
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"os"
	"sync"

	"github.com/Aset-Mk/geomatch/internal/config"
	"github.com/Aset-Mk/geomatch/internal/index"
	"github.com/Aset-Mk/geomatch/internal/load"
	"github.com/Aset-Mk/geomatch/internal/logging"
	"github.com/Aset-Mk/geomatch/internal/sim"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid config", "err", err)
		os.Exit(1)
	}

	logger := logging.New(cfg.LogLevel)
	slog.SetDefault(logger)

	lcfg, err := loadLoadgenConfig()
	if err != nil {
		slog.Error("invalid loadgen config", "err", err)
		os.Exit(1)
	}
	slog.Info("loadgen started", "drivers", lcfg.Drivers, "update_period", lcfg.UpdatePeriod.String(), "order_rate", lcfg.OrderRate, "duration", lcfg.Duration.String(), "seed", lcfg.Seed)

	driversRand := rand.New(rand.NewPCG(lcfg.Seed, 1))
	ordersRand := rand.New(rand.NewPCG(lcfg.Seed, 2))

	b := sim.CityBounds
	idx := index.NewNaive()
	drivers := load.NewDrivers(lcfg.Drivers, b, driversRand)

	// Populate the index before orders start coming in, so that all drivers are visible as soon as the first orders arrive
	for _, d := range drivers {
		idx.Upsert(d.ID, d.Pos)
	}

	rec := load.NewRecorder()
	var wg sync.WaitGroup
	wg.Go(func() {
		load.RunDrivers(idx, drivers, b, driversRand, lcfg.UpdatePeriod, lcfg.Duration)
	})
	wg.Go(func() {
		load.RunOrders(idx, b, ordersRand, lcfg.OrderRate, lcfg.Duration, rec.Record)
	})
	wg.Wait()

	sum := rec.Summary()
	slog.Info("loadgen finished", "summary", sum)

	data, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		slog.Error("encode summary", "err", err)
		os.Exit(1)
	}
	if err := os.WriteFile(lcfg.Out, append(data, '\n'), 0o644); err != nil {
		slog.Error("write summary", "err", err)
		os.Exit(1)
	}
}
