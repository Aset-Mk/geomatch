package dispatch

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	ordersTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "geomatch_orders_total",
		Help: "Orders by result: assigned, no_driver, canceled, error",
	}, []string{"result"})

	offersTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "geomatch_offers_total",
		Help: "Offers by answer: accepted, declined, timeout, expired",
	}, []string{"answer"})

	offersDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "geomatch_offers_dropped_total",
		Help: "Offers not sent to an Offers stream that was behind",
	})

	assignSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "geomatch_assign_seconds",
		Help:    "Time from a new order to the driver who accepted it",
		Buckets: []float64{100e-6, 250e-6, 500e-6, 1e-3, 2.5e-3, 5e-3, 10e-3, 25e-3, 50e-3, 100e-3, 250e-3, 1, 2.5, 5, 10, 30},
	})
)
