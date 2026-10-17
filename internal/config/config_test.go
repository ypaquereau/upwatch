package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// fakeEnv returns a getenv function backed by a map, so tests never touch the
// real process environment.
func fakeEnv(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestLoad(t *testing.T) {
	const dsn = "postgres://u:p@localhost:5432/upwatch"

	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr string // substring expected in the error; empty means success
	}{
		{
			name: "defaults",
			env:  map[string]string{"DATABASE_URL": dsn},
			want: Config{
				HTTPAddr:          ":8080",
				DatabaseURL:       dsn,
				WorkerEnabled:     true,
				WorkerConcurrency: 10,
				CheckTimeout:      5 * time.Second,
				Retention:         168 * time.Hour,
				LogLevel:          slog.LevelInfo,
				ShutdownTimeout:   15 * time.Second,
			},
		},
		{
			name: "all overridden",
			env: map[string]string{
				"HTTP_ADDR":          ":9090",
				"DATABASE_URL":       dsn,
				"WORKER_ENABLED":     "false",
				"WORKER_CONCURRENCY": "3",
				"CHECK_TIMEOUT":      "2s",
				"RETENTION":          "24h",
				"LOG_LEVEL":          "debug",
				"SHUTDOWN_TIMEOUT":   "30s",
			},
			want: Config{
				HTTPAddr:          ":9090",
				DatabaseURL:       dsn,
				WorkerEnabled:     false,
				WorkerConcurrency: 3,
				CheckTimeout:      2 * time.Second,
				Retention:         24 * time.Hour,
				LogLevel:          slog.LevelDebug,
				ShutdownTimeout:   30 * time.Second,
			},
		},
		{
			name:    "missing database url",
			env:     map[string]string{},
			wantErr: "DATABASE_URL",
		},
		{
			name:    "invalid bool",
			env:     map[string]string{"DATABASE_URL": dsn, "WORKER_ENABLED": "maybe"},
			wantErr: "WORKER_ENABLED",
		},
		{
			name:    "invalid int",
			env:     map[string]string{"DATABASE_URL": dsn, "WORKER_CONCURRENCY": "ten"},
			wantErr: "WORKER_CONCURRENCY",
		},
		{
			name:    "zero concurrency",
			env:     map[string]string{"DATABASE_URL": dsn, "WORKER_CONCURRENCY": "0"},
			wantErr: "WORKER_CONCURRENCY",
		},
		{
			name:    "invalid duration",
			env:     map[string]string{"DATABASE_URL": dsn, "CHECK_TIMEOUT": "soon"},
			wantErr: "CHECK_TIMEOUT",
		},
		{
			name:    "negative duration",
			env:     map[string]string{"DATABASE_URL": dsn, "RETENTION": "-1h"},
			wantErr: "RETENTION",
		},
		{
			name:    "invalid log level",
			env:     map[string]string{"DATABASE_URL": dsn, "LOG_LEVEL": "loud"},
			wantErr: "LOG_LEVEL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(fakeEnv(tt.env))

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Load() error = nil, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Load() error = %q, want it to mention %q", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
