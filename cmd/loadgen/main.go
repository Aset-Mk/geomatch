package main

import (
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"os"
	"runtime/pprof"
	"sync"
	"time"

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
	slog.Info("loadgen started", "index", lcfg.Index, "cell_size", lcfg.CellSize, "order_workers", lcfg.OrderWorkers, "driver_workers", lcfg.DriverWorkers, "update_batch", lcfg.UpdateBatch, "update_linger", lcfg.UpdateLinger.String(), "drivers", lcfg.Drivers, "update_period", lcfg.UpdatePeriod.String(), "order_rate", lcfg.OrderRate, "duration", lcfg.Duration.String(), "seed", lcfg.Seed)

	driversRand := rand.New(rand.NewPCG(lcfg.Seed, 1))

	b := sim.CityBounds
	var idx index.Index = index.NewNaive()
	if lcfg.Index == "grid" {
		idx = index.NewGrid(lcfg.CellSize, (b.Min.Lat+b.Max.Lat)/2)
	}
	drivers := load.NewDrivers(lcfg.Drivers, b, driversRand)

	// fill the index first so the first orders already see every driver
	for _, d := range drivers {
		idx.Upsert(d.ID, d.Pos)
	}

	if lcfg.CPUProfile != "" {
		stop, err := startCPUProfile(lcfg.CPUProfile)
		if err != nil {
			slog.Error("start cpu profile", "err", err)
			os.Exit(1)
		}
		defer stop()
	}

	// odd streams go to drivers and even ones to orders, so one worker of each keeps the old streams
	var wg sync.WaitGroup
	for i := range lcfg.DriverWorkers {
		r := driversRand
		if i > 0 {
			r = rand.New(rand.NewPCG(lcfg.Seed, uint64(2*i+1)))
		}
		part := drivers[i*len(drivers)/lcfg.DriverWorkers : (i+1)*len(drivers)/lcfg.DriverWorkers]
		wg.Go(func() {
			load.RunDrivers(idx, part, b, r, lcfg.UpdatePeriod, lcfg.Duration, lcfg.UpdateBatch, lcfg.UpdateLinger)
		})
	}

	recs := make([]*load.Recorder, lcfg.OrderWorkers)
	rate := lcfg.OrderRate / lcfg.OrderWorkers
	for i := range recs {
		recs[i] = load.NewRecorder()
		r := rand.New(rand.NewPCG(lcfg.Seed, uint64(2*i+2)))
		offset := time.Second / time.Duration(lcfg.OrderRate) * time.Duration(i)
		wg.Go(func() {
			time.Sleep(offset)
			load.RunOrders(idx, b, r, rate, lcfg.Duration, recs[i].Record)
		})
	}
	wg.Wait()

	rec := recs[0]
	for _, r := range recs[1:] {
		rec.Merge(r)
	}
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

// startCPUProfile writes a CPU profile to path until the returned stop is called
func startCPUProfile(path string) (stop func(), err error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		pprof.StopCPUProfile()
		if err := f.Close(); err != nil {
			slog.Error("close cpu profile", "err", err)
		}
	}, nil
}
