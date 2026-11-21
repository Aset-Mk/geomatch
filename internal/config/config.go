package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
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

// Env returns def if name is unset and appends parse errors to errs
func Env[T any](name string, def T, parse func(string) (T, error), errs *[]error) T {
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

func String(s string) (string, error) { return s, nil }
func Float(s string) (float64, error) { return strconv.ParseFloat(s, 64) }
func Uint(s string) (uint64, error)   { return strconv.ParseUint(s, 10, 64) }

func Int32(s string) (int32, error) {
	n, err := strconv.ParseInt(s, 10, 32)
	return int32(n), err
}

// List splits a comma separated list, an empty string is an empty list
func List(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, ","), nil
}
