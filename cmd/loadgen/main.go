package main

import (
	"log/slog"
	"os"

	"github.com/Aset-Mk/geomatch/internal/config"
	"github.com/Aset-Mk/geomatch/internal/logging"
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
}
