package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"reflect"
	"sync"
	"syscall"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/logging"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
)

// Only log_level can change. The configuration used by SMTP sessions and
// workers remains immutable, including when several reloads run concurrently.
type configReloader struct {
	mu            sync.Mutex
	path          string
	current       config.Config
	generation    int64
	applyLogLevel func(string, int64)
}

func (r *configReloader) reload() (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next, err := config.LoadCheckedWithPath(r.path)
	if err != nil {
		return r.generation, err
	}
	unchanged := next
	unchanged.LogLevel = r.current.LogLevel
	if !reflect.DeepEqual(unchanged, r.current) {
		return r.generation, errors.New("configuration changes include restart-required settings; update rejected")
	}
	if next.LogLevel == r.current.LogLevel {
		return r.generation, nil
	}
	generation := r.generation + 1
	// slog.SetDefault atomically publishes one immutable logger, including its
	// level and generation attribute. No live session configuration is mutated.
	r.applyLogLevel(next.LogLevel, generation)
	r.current = next
	r.generation = generation
	return generation, nil
}

func startConfigReload(ctx context.Context, path string, cfg config.Config, metrics *observability.Metrics) func() {
	r := &configReloader{
		path: path, current: cfg, generation: 1,
		applyLogLevel: func(level string, generation int64) {
			slog.SetDefault(logging.New(level, os.Stdout).With("config_generation", generation))
		},
	}
	metrics.SetGauge("config_generation", 1)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP)
	reloadCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-reloadCtx.Done():
				return
			case <-signals:
				if reloadCtx.Err() != nil {
					return
				}
				generation, err := r.reload()
				if err != nil {
					metrics.Counter("config_reload_failure_total").Inc()
					slog.ErrorContext(reloadCtx, "configuration reload rejected", "component", "config", "event", "config_reload_rejected", "error", err)
					continue
				}
				metrics.SetGauge("config_generation", generation)
				metrics.Counter("config_reload_success_total").Inc()
				slog.InfoContext(reloadCtx, "configuration reload completed", "component", "config", "event", "config_reloaded", "generation", generation)
			}
		}
	}()
	return func() {
		signal.Stop(signals)
		cancel()
		<-done
	}
}
