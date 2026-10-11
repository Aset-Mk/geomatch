package config

import (
	"fmt"
	"log/slog"
	"os"
)

// Config holds settings read from environment variables
type Config struct {
	LogLevel slog.Level
}

// Load reads and validates environment variables
func Load() (Config, error) {
	cfg := Config{LogLevel: slog.LevelInfo}

	if v, ok := os.LookupEnv("LOG_LEVEL"); ok {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
		}
	}

	return cfg, nil
}
