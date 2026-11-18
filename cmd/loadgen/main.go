package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/pprof"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/Aset-Mk/geomatch/internal/client"
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
	slog.Info("loadgen started", "matcher_addr", lcfg.MatcherAddr, "matcher_conns", lcfg.MatcherConns, "index", lcfg.Index, "cell_size", lcfg.CellSize, "order_workers", lcfg.OrderWorkers, "order_inflight", lcfg.OrderInflight, "driver_workers", lcfg.DriverWorkers, "update_batch", lcfg.UpdateBatch, "update_linger", lcfg.UpdateLinger.String(), "driver_conns", lcfg.DriverConns, "drivers", lcfg.Drivers, "update_period", lcfg.UpdatePeriod.String(), "order_rate", lcfg.OrderRate, "duration", lcfg.Duration.String(), "seed", lcfg.Seed)

	driversRand := rand.New(rand.NewPCG(lcfg.Seed, 1))

	b := sim.CityBounds
	var idx index.Index = index.NewNaive()
	if lcfg.Index == "grid" {
		idx = index.NewGrid(lcfg.CellSize, (b.Min.Lat+b.Max.Lat)/2)
	}
	var remote *client.Client
	if lcfg.MatcherAddr != "" {
		conns := make([]grpc.ClientConnInterface, lcfg.MatcherConns)
		for i := range conns {
			conn, err := grpc.NewClient(lcfg.MatcherAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				slog.Error("matcher client", "err", err)
				os.Exit(1)
			}
			defer func() { _ = conn.Close() }()
			conns[i] = conn
		}
		remote = client.New(conns...)
		idx = remote
	}
	drivers := load.NewDrivers(lcfg.Drivers, b, driversRand)

	var updater load.Updater = idx
	var fleet *client.Fleet
	if lcfg.DriverConns > 0 {
		fleet, err = newFleet(lcfg)
		if err != nil {
			slog.Error("driver connections", "err", err)
			os.Exit(1)
		}
		updater = fleet
	}

	// fill the index first so the first orders already see every driver
	fill := make([]index.Update, len(drivers))
	for i, d := range drivers {
		fill[i] = index.Update{ID: d.ID, Pos: d.Pos}
	}
	began := time.Now()
	if fleet != nil {
		// every driver opens its stream here, in parallel so the connections come up faster
		var wg sync.WaitGroup
		for chunk := range slices.Chunk(fill, (len(fill)+63)/64) {
			wg.Go(func() { fleet.UpsertBatch(chunk) })
		}
		wg.Wait()
	} else {
		// chunks keep a gRPC message far below its 4MB limit
		for chunk := range slices.Chunk(fill, 1000) {
			idx.UpsertBatch(chunk)
		}
	}
	if remote != nil {
		if err := remote.Flush(); err != nil {
			slog.Error("fill matcher", "err", err)
			os.Exit(1)
		}
	}
	slog.Info("drivers sent", "took", time.Since(began).String())

	if lcfg.MetricsAddr != "" {
		failed := func() uint64 {
			n := uint64(0)
			if remote != nil {
				n += remote.Failed()
			}
			if fleet != nil {
				n += fleet.Failed()
			}
			return n
		}
		if err := serveMetrics(lcfg.MetricsAddr, failed); err != nil {
			slog.Error("metrics", "err", err)
			os.Exit(1)
		}
	}

	if lcfg.CPUProfile != "" {
		stop, err := startCPUProfile(lcfg.CPUProfile)
		if err != nil {
			slog.Error("start cpu profile", "err", err)
			os.Exit(1)
		}
		defer stop()
	}

	// a stop signal ends the run early and the summary covers what was sent
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// odd streams go to drivers and even ones to orders, so one worker of each keeps the old streams
	var wg sync.WaitGroup
	for i := range lcfg.DriverWorkers {
		if lcfg.Drivers == 0 {
			break
		}
		r := driversRand
		if i > 0 {
			r = rand.New(rand.NewPCG(lcfg.Seed, uint64(2*i+1)))
		}
		part := drivers[i*len(drivers)/lcfg.DriverWorkers : (i+1)*len(drivers)/lcfg.DriverWorkers]
		wg.Go(func() {
			load.RunDrivers(ctx, updater, part, b, r, lcfg.UpdatePeriod, lcfg.Duration, lcfg.UpdateBatch, lcfg.UpdateLinger)
		})
	}

	recs := make([]*load.Recorder, lcfg.OrderWorkers)
	rate := lcfg.OrderRate / lcfg.OrderWorkers
	for i := range recs {
		recs[i] = load.NewRecorder()
		if rate == 0 {
			continue
		}
		record := recs[i].Record
		if lcfg.MetricsAddr != "" {
			record = func(d time.Duration) {
				recs[i].Record(d)
				orderSeconds.Observe(d.Seconds())
			}
		}
		r := rand.New(rand.NewPCG(lcfg.Seed, uint64(2*i+2)))
		offset := time.Second / time.Duration(lcfg.OrderRate) * time.Duration(i)
		wg.Go(func() {
			time.Sleep(offset)
			if lcfg.OrderInflight > 0 {
				load.RunOrdersAsync(ctx, idx, b, r, rate, lcfg.Duration, lcfg.OrderInflight, record)
				return
			}
			load.RunOrders(ctx, idx, b, r, rate, lcfg.Duration, record)
		})
	}
	wg.Wait()
	if fleet != nil {
		if err := fleet.Close(); err != nil {
			slog.Error("driver streams", "err", err)
			os.Exit(1)
		}
	}
	if remote != nil {
		if err := remote.Flush(); err != nil {
			slog.Error("matcher", "err", err)
			os.Exit(1)
		}
	}

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

var orderSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "geomatch_loadgen_order_seconds",
	Help:    "Time from the scheduled start of an order to its answer",
	Buckets: []float64{25e-6, 50e-6, 100e-6, 250e-6, 500e-6, 1e-3, 2.5e-3, 5e-3, 10e-3, 25e-3, 50e-3, 100e-3, 250e-3, 1},
})

// serveMetrics exposes the latency of orders and the number of failed calls to the matcher
func serveMetrics(addr string, failed func() uint64) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	promauto.NewCounterFunc(prometheus.CounterOpts{
		Name: "geomatch_loadgen_errors_total",
		Help: "Calls to the matcher that failed",
	}, func() float64 { return float64(failed()) })
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(lis) }()
	return nil
}

// newFleet opens DriverConns connections spread over DriverAddrs, they connect on the first stream
func newFleet(cfg loadgenConfig) (*client.Fleet, error) {
	conns := make([]grpc.ClientConnInterface, cfg.DriverConns)
	for i := range conns {
		// a driver sends a few dozen bytes, the default 32KB read buffer per connection would take gigabytes
		conn, err := grpc.NewClient(cfg.DriverAddrs[i%len(cfg.DriverAddrs)],
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithReadBufferSize(4096),
		)
		if err != nil {
			return nil, err
		}
		conns[i] = conn
	}
	return client.NewFleet(cfg.Drivers, conns...), nil
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
