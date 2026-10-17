// Package config loads and validates the application settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

// Config holds the application settings, loaded from environment variables.
type Config struct {
	HTTPAddr          string
	DatabaseURL       string
	WorkerEnabled     bool
	WorkerConcurrency int
	CheckTimeout      time.Duration
	Retention         time.Duration
	LogLevel          slog.Level
	ShutdownTimeout   time.Duration
}

// Load builds a Config from the environment, using getenv to read variables
// (os.Getenv in production, a fake in tests). An empty value means "unset" and
// keeps the default. It returns an error naming the first invalid variable.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:          ":8080",
		WorkerEnabled:     true,
		WorkerConcurrency: 10,
		CheckTimeout:      5 * time.Second,
		Retention:         168 * time.Hour,
		LogLevel:          slog.LevelInfo,
		ShutdownTimeout:   15 * time.Second,
	}

	if v := getenv("HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}

	cfg.DatabaseURL = getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	if v := getenv("WORKER_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("parsing WORKER_ENABLED: %w", err)
		}
		cfg.WorkerEnabled = b
	}

	if v := getenv("WORKER_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("parsing WORKER_CONCURRENCY: %w", err)
		}
		if n < 1 {
			return Config{}, fmt.Errorf("WORKER_CONCURRENCY must be >= 1, got %d", n)
		}
		cfg.WorkerConcurrency = n
	}

	var err error
	if cfg.CheckTimeout, err = envDuration(getenv, "CHECK_TIMEOUT", cfg.CheckTimeout); err != nil {
		return Config{}, err
	}
	if cfg.Retention, err = envDuration(getenv, "RETENTION", cfg.Retention); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = envDuration(getenv, "SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return Config{}, err
	}

	if v := getenv("LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return Config{}, fmt.Errorf("parsing LOG_LEVEL: %w", err)
		}
	}

	return cfg, nil
}

// envDuration reads a strictly positive duration, or returns def if unset.
func envDuration(getenv func(string) string, key string, def time.Duration) (time.Duration, error) {
	v := getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", key, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be > 0, got %s", key, d)
	}
	return d, nil
}
