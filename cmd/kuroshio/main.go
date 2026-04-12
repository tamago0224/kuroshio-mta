package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/admin"
	"github.com/tamago0224/kuroshio-mta/internal/bounce"
	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/delivery"
	"github.com/tamago0224/kuroshio-mta/internal/logging"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
	"github.com/tamago0224/kuroshio-mta/internal/reputation"
	"github.com/tamago0224/kuroshio-mta/internal/retention"
	"github.com/tamago0224/kuroshio-mta/internal/smtp"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
	"github.com/tamago0224/kuroshio-mta/internal/worker"
)

func main() {
	configPath := flag.String("config", "", "path to YAML config file")
	flag.Parse()

	cfg, err := config.LoadWithPath(*configPath)
	if err != nil {
		fatal("config load failed", "error", err)
	}
	slog.SetDefault(logging.New(cfg.LogLevel, os.Stdout))
	slog.Info("audit event", "component", "audit", "event", "config_loaded", "queue_backend", cfg.QueueBackend, "submission_enabled", cfg.SubmissionAddr != "", "delivery_mode", cfg.DeliveryMode)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, err := observability.InitOTEL(ctx, cfg)
	if err != nil {
		fatal("otel init failed", "error", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if sErr := otelShutdown(shutdownCtx); sErr != nil {
			slog.Error("otel shutdown failed", "component", "main", "error", sErr)
		}
	}()

	q, err := queue.NewBackend(cfg)
	if err != nil {
		fatal("queue init failed", "error", err)
	}
	defer func() {
		if cErr := q.Close(); cErr != nil {
			slog.Error("queue close failed", "component", "main", "error", cErr)
		}
	}()
	sup, err := bounce.NewSuppressionStore(filepath.Join(cfg.QueueDir, "suppression.json"))
	if err != nil {
		fatal("suppression init failed", "error", err)
	}
	metrics := observability.NewMetrics()
	rep := reputation.New(reputation.Config{
		StartDate:          reputation.ParseStartDate(cfg.ReputationStartDate),
		WarmupRules:        reputation.ParseWarmupRules(cfg.ReputationWarmupRules),
		BounceThreshold:    cfg.ReputationBounceThreshold,
		ComplaintThreshold: cfg.ReputationComplaintThresh,
		MinSamples:         cfg.ReputationMinSamples,
	})

	d, err := worker.New(cfg, q, delivery.NewClientWithMetrics(cfg, metrics), sup, metrics, rep)
	if err != nil {
		fatal("worker init failed", "error", err)
	}
	s := smtp.NewServer(cfg, q, metrics)
	var submissionServer *smtp.Server
	if cfg.SubmissionAddr != "" {
		var (
			authBackend userauth.Backend
			aErr        error
		)
		switch cfg.SubmissionAuthBackend {
		case "sqlite":
			authBackend, aErr = userauth.NewSQLite(cfg.SubmissionAuthDSN)
			if aErr != nil {
				fatal("submission auth init failed", "error", aErr)
			}
		default:
			authBackend, aErr = userauth.NewStatic(cfg.SubmissionUsers)
			if aErr != nil {
				fatal("submission auth init failed", "error", aErr)
			}
			if cfg.SubmissionAuth && strings.TrimSpace(cfg.SubmissionUsers) == "" {
				fatal("submission auth is required but MTA_SUBMISSION_USERS is empty")
			}
		}
		submissionServer = smtp.NewSubmissionServer(cfg, q, metrics, authBackend)
	}

	workers := 3
	if submissionServer != nil {
		workers++
	}
	if cfg.AdminAddr != "" {
		workers++
	}
	workers++
	errCh := make(chan error, workers)
	go func() { errCh <- s.Run(ctx) }()
	if submissionServer != nil {
		go func() { errCh <- submissionServer.Run(ctx) }()
	}
	go func() { errCh <- d.Run(ctx) }()
	go func() { errCh <- observability.RunServerWithReputation(ctx, cfg.ObservabilityAddr, metrics, rep) }()
	if cfg.AdminAddr != "" {
		var queueAdmin interface {
			ListState(state string, limit int) ([]*model.Message, error)
			RequeueFromState(state, id string, now time.Time) (*model.Message, error)
		}
		if localQueue, ok := q.(*queue.Store); ok {
			queueAdmin = localQueue
		}
		adminAuthBackend := admin.NewConfigTokenBackend(cfg.AdminTokens)
		if cfg.AdminAuthBackend == "sqlite" {
			adminAuthBackend, err = admin.NewSQLiteTokenBackend(cfg.AdminAuthDSN)
			if err != nil {
				fatal("admin auth init failed", "error", err)
			}
		}
		go func() {
			errCh <- admin.RunServerWithBackend(ctx, cfg.AdminAddr, adminAuthBackend, sup, queueAdmin, rep)
		}()
	}
	go func() {
		errCh <- retention.Run(ctx, cfg.QueueDir, retention.Policy{
			SentTTL:   cfg.DataRetentionSent,
			DLQTTL:    cfg.DataRetentionDLQ,
			PoisonTTL: cfg.DataRetentionPoison,
			Interval:  cfg.RetentionSweepInterval,
		})
	}()

	for i := 0; i < workers; i++ {
		err := <-errCh
		if err == nil || errors.Is(err, context.Canceled) {
			continue
		}
		slog.Error("runtime error", "component", "main", "error", err)
		stop()
	}
}

func fatal(msg string, args ...any) {
	base := []any{"component", "main"}
	base = append(base, args...)
	slog.Error(msg, base...)
	os.Exit(1)
}
