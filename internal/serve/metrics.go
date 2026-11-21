package serve

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var (
	requestSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "geomatch_request_seconds",
		Help:    "Time a request spends in its handler",
		Buckets: []float64{5e-6, 10e-6, 25e-6, 50e-6, 100e-6, 250e-6, 500e-6, 1e-3, 2.5e-3, 5e-3, 10e-3, 25e-3, 100e-3},
	}, []string{"method"})
	requestErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "geomatch_request_errors_total",
		Help: "Requests that ended with an error",
	}, []string{"method", "code"})
	connections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "geomatch_connections",
		Help: "Open client connections",
	})
	connectionsRefused = promauto.NewCounter(prometheus.CounterOpts{
		Name: "geomatch_connections_refused_total",
		Help: "Connections closed on arrival because the limit was reached",
	})
)

// Measure is a unary interceptor that records the handler time and the errors of every request
func Measure(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	requestSeconds.WithLabelValues(info.FullMethod).Observe(time.Since(start).Seconds())
	if err != nil {
		requestErrors.WithLabelValues(info.FullMethod, status.Code(err).String()).Inc()
	}
	return resp, err
}
