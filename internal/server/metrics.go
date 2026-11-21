package server

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	driverUpdates = promauto.NewCounter(prometheus.CounterOpts{
		Name: "geomatch_driver_updates_total",
		Help: "Driver positions put into the index",
	})
	silentRemoved = promauto.NewCounter(prometheus.CounterOpts{
		Name: "geomatch_silent_drivers_removed_total",
		Help: "Drivers removed because they stopped sending positions",
	})
	drivers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "geomatch_drivers",
		Help: "Drivers in the index, refreshed on every sweep of silent ones",
	})
	updateStreams = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "geomatch_update_streams",
		Help: "Open streams of driver positions",
	})
	updateDelay = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "geomatch_update_delay_seconds",
		Help:    "Time from a position written to Kafka to the same position in the index, Kafka keeps milliseconds",
		Buckets: []float64{1e-3, 2e-3, 3e-3, 4e-3, 5e-3, 6e-3, 8e-3, 10e-3, 12e-3, 15e-3, 20e-3, 25e-3, 30e-3, 40e-3, 50e-3, 60e-3, 80e-3, 100e-3, 150e-3, 200e-3, 300e-3, 500e-3, 1, 2, 5, 10, 30},
	})
	kafkaLag = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "geomatch_kafka_lag",
		Help: "Positions written to Kafka and not yet read, over all partitions",
	})
	staleSkipped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "geomatch_stale_updates_skipped_total",
		Help: "Positions older than the one already in the index",
	})
	badRecords = promauto.NewCounter(prometheus.CounterOpts{
		Name: "geomatch_bad_records_total",
		Help: "Kafka records that could not be read as a driver position",
	})
)
