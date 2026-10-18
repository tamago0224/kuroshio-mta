package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/logging"
)

func TestConfigReloadValidationAndGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("log_level: info\n")
	initial, err := config.LoadWithPath(path)
	if err != nil {
		t.Fatal(err)
	}
	var applied atomic.Int64
	var logger atomic.Pointer[slog.Logger]
	logger.Store(logging.New("info", io.Discard).With("config_generation", int64(1)))
	r := &configReloader{
		path: path, current: initial, generation: 1,
		applyLogLevel: func(level string, generation int64) {
			logger.Store(logging.New(level, io.Discard).With("config_generation", generation))
			applied.Add(1)
		},
	}
	write("log_level: debug\n")
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			generation, err := r.reload()
			if err != nil || generation != 2 {
				t.Errorf("reload: generation=%d error=%v", generation, err)
			}
		})
		wg.Go(func() {
			for range 100 {
				logger.Load().Debug("concurrent session record")
			}
		})
	}
	wg.Wait()
	if applied.Load() != 1 || !logger.Load().Enabled(t.Context(), slog.LevelDebug) {
		t.Fatal("level was not published exactly once")
	}
	for _, tt := range []struct{ name, body string }{
		{"unknown key", "log_levle: warn\n"},
		{"restart setting", "log_level: error\nworker_count: 9\n"},
		{"invalid update", "log_level: error\nworker_count: -1\n"},
		{"invalid key", "log_level: error\ndkim_sign_domain: example.test\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			write(tt.body)
			generation, err := r.reload()
			if err == nil || generation != 2 {
				t.Fatalf("rejected reload: generation=%d error=%v", generation, err)
			}
			if applied.Load() != 1 || !logger.Load().Enabled(t.Context(), slog.LevelDebug) {
				t.Fatal("rejected update changed logger")
			}
			if r.current.WorkerCount != initial.WorkerCount || r.current.LogLevel != "debug" {
				t.Fatal("rejected update changed snapshot")
			}
		})
	}
	write("log_level: warn\n")
	if generation, err := r.reload(); err != nil || generation != 3 {
		t.Fatalf("recovery: generation=%d error=%v", generation, err)
	}
	if logger.Load().Enabled(t.Context(), slog.LevelInfo) {
		t.Fatal("new threshold not applied")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if generation, err := r.reload(); err == nil || generation != 3 {
		t.Fatalf("missing file: generation=%d error=%v", generation, err)
	}
}
