package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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
	"github.com/tamago0224/kuroshio-mta/internal/deliveryhistory"
	"github.com/tamago0224/kuroshio-mta/internal/deliverypause"
	"github.com/tamago0224/kuroshio-mta/internal/dmarcreport"
	"github.com/tamago0224/kuroshio-mta/internal/eventwebhook"
	"github.com/tamago0224/kuroshio-mta/internal/httpinjection"
	"github.com/tamago0224/kuroshio-mta/internal/logging"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/observability"
	"github.com/tamago0224/kuroshio-mta/internal/queue"
	"github.com/tamago0224/kuroshio-mta/internal/reputation"
	"github.com/tamago0224/kuroshio-mta/internal/retention"
	"github.com/tamago0224/kuroshio-mta/internal/smtp"
	"github.com/tamago0224/kuroshio-mta/internal/tlsreport"
	"github.com/tamago0224/kuroshio-mta/internal/userauth"
	"github.com/tamago0224/kuroshio-mta/internal/worker"
)

func main() {
	checkConfig := flag.Bool("check-config", false, "validate configuration without starting services")
	configPath := flag.String("config", "", "path to YAML config file")
	flag.Parse()
	if *checkConfig {
		if flag.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "check-config does not accept positional arguments")
			os.Exit(2)
		}
		if err := config.CheckWithPath(*configPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, "configuration OK")
		return
	}

	cfg, err := config.LoadWithPath(*configPath)
	if err != nil {
		fatal("config load failed", "error", err)
	}
	slog.SetDefault(logging.New(cfg.LogLevel, os.Stdout).With("config_generation", int64(1)))
	slog.Info("audit event", "component", "audit", "event", "config_loaded", "queue_backend", cfg.QueueBackend, "submission_enabled", (cfg.SubmissionAddr != "" || cfg.SubmissionTLSAddr != ""), "delivery_mode", cfg.DeliveryMode, "dmarc_policy_mode", cfg.DMARCPolicyMode)

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
	dmarcReports, err := dmarcreport.Open(ctx, filepath.Join(cfg.QueueDir, "dmarc-reports.db"))
	if err != nil {
		fatal("DMARC report store init failed", "error", err)
	}
	defer func() {
		if err := dmarcReports.Close(); err != nil {
			slog.Error("DMARC report store close failed", "error", err)
		}
	}()
	sup, err := bounce.NewSuppressionStore(filepath.Join(cfg.QueueDir, "suppression.json"))
	if err != nil {
		fatal("suppression init failed", "error", err)
	}
	metrics := observability.NewMetrics()
	queue.SetMetrics(q, metrics)
	var eventDispatcher *eventwebhook.Dispatcher
	var eventOutbox *eventwebhook.Outbox
	if cfg.EventWebhook.Enabled {
		eventOutbox, err = eventwebhook.Open(filepath.Join(cfg.QueueDir, "events"), cfg.EventWebhook.MaxPending, metrics)
		if err != nil {
			fatal("event webhook outbox init failed", "error", err)
		}
		eventDispatcher, err = eventwebhook.NewDispatcher(cfg.EventWebhook, eventOutbox, metrics)
		if err != nil {
			fatal("event webhook dispatcher init failed", "error", err)
		}
		defer eventDispatcher.Close()
		q = queue.WithAcceptedEvents(q, eventOutbox, metrics)
	}
	rep, err := reputation.NewPersistent(reputation.Config{
		StartDate:          reputation.ParseStartDate(cfg.ReputationStartDate),
		WarmupRules:        reputation.ParseWarmupRules(cfg.ReputationWarmupRules),
		BounceThreshold:    cfg.ReputationBounceThreshold,
		ComplaintThreshold: cfg.ReputationComplaintThresh,
		MinSamples:         cfg.ReputationMinSamples,
	}, filepath.Join(cfg.QueueDir, "reputation.json"))
	if err != nil {
		fatal("reputation state load failed", "error", err)
	}
	// Flush after all runtime workers have exited, preserving their final observations.
	defer func() {
		if err := rep.Flush(); err != nil {
			slog.Error("reputation final flush failed", "error", err)
		}
	}()

	pauses, err := deliverypause.New(ctx, cfg)
	if err != nil {
		fatal("delivery pause init failed", "error", err)
	}
	if pauses != nil {
		defer pauses.Close()
	}
	deliveryClient := delivery.NewClientWithContext(ctx, cfg, metrics)
	defer deliveryClient.Close()
	var tlsReports *tlsreport.Manager
	if cfg.TLSReport.Enabled {
		tlsReports, err = tlsreport.Open(cfg.TLSReport, cfg.QueueDir, metrics)
		if err != nil {
			fatal("TLS report store init failed", "error", err)
		}
		deliveryClient.SetTLSObserver(tlsReports.Observe)
	}
	stopReload := startConfigReload(ctx, *configPath, cfg, metrics, func() (bool, error) { return deliveryClient.ReloadSigning(ctx) })
	defer stopReload()
	d, err := worker.New(cfg, q, deliveryClient, sup, metrics, rep)
	if err != nil {
		fatal("worker init failed", "error", err)
	}
	d.SetDeliveryPauses(pauses)
	d.SetEventOutbox(eventOutbox)
	var deliveryHistory *deliveryhistory.Store
	if cfg.DeliveryHistory.Enabled {
		deliveryHistory, err = deliveryhistory.Open(
			filepath.Join(cfg.QueueDir, "delivery-history.db"),
			cfg.DeliveryHistory.Retention,
			cfg.DeliveryHistory.MaxRows,
		)
		if err != nil {
			fatal("delivery history init failed", "error", err)
		}
		defer func() {
			if closeErr := deliveryHistory.Close(); closeErr != nil {
				slog.Error("delivery history close failed", "error", closeErr)
			}
		}()
		d.SetDeliveryHistory(deliveryHistory)
	}
	s := smtp.NewServer(cfg, q, metrics)
	s.SetDMARCReportStore(dmarcReports)
	var submissionServers []*smtp.Server
	var authBackend userauth.Backend
	var bearerBackend userauth.BearerBackend
	if cfg.SubmissionAddr != "" || cfg.SubmissionTLSAddr != "" || cfg.HTTPInjection.Enabled {
		var (
			aErr error
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
			if cfg.SubmissionAuth && strings.TrimSpace(cfg.SubmissionUsers) == "" && cfg.SubmissionBearerIntrospectionURL == "" {
				fatal("submission auth is required but MTA_SUBMISSION_USERS is empty")
			}
		}
		if cfg.SubmissionBearerIntrospectionURL != "" {
			bearer, err := userauth.NewIntrospection(userauth.IntrospectionConfig{
				URL: cfg.SubmissionBearerIntrospectionURL, ClientID: cfg.SubmissionBearerClientID,
				ClientSecret: cfg.SubmissionBearerClientSecret, Issuer: cfg.SubmissionBearerIssuer,
				Audience: cfg.SubmissionBearerAudience, UsernameClaim: cfg.SubmissionBearerUsernameClaim,
				RequiredScope: cfg.SubmissionBearerRequiredScope,
			})
			if err != nil {
				fatal("submission bearer init failed", "error", err)
			}
			bearerBackend = bearer
			if cfg.SubmissionAuthBackend == "static" && strings.TrimSpace(cfg.SubmissionUsers) == "" {
				authBackend = nil
			}
			authBackend = &userauth.CombinedBackend{Password: authBackend, Bearer: bearer}
		}
		if cfg.SubmissionAddr != "" {
			submissionServers = append(submissionServers, smtp.NewSubmissionServer(cfg, q, metrics, authBackend))
		}
		if cfg.SubmissionTLSAddr != "" {
			submissionServers = append(submissionServers, smtp.NewImplicitTLSSubmissionServer(cfg, q, metrics, authBackend))
		}
	}

	workers := 5
	workers += len(submissionServers)
	if cfg.AdminAddr != "" {
		workers++
	}
	if cfg.HTTPInjection.Enabled {
		workers++
	}
	workers++
	if eventDispatcher != nil {
		workers++
	}
	if tlsReports != nil {
		workers++
	}
	errCh := make(chan error, workers)
	go func() { errCh <- s.Run(ctx) }()
	for _, submissionServer := range submissionServers {
		go func() { errCh <- submissionServer.Run(ctx) }()
	}
	go func() { errCh <- d.Run(ctx) }()
	if eventDispatcher != nil {
		go func() { errCh <- eventDispatcher.Run(ctx) }()
	}
	if tlsReports != nil {
		go func() { errCh <- tlsReports.Run(ctx) }()
	}
	go func() { errCh <- runDMARCReports(ctx, dmarcReports, q, cfg.Hostname, metrics) }()
	go func() { errCh <- rep.RunPersistence(ctx, 5*time.Second) }()
	go func() { errCh <- observability.RunServerWithReputation(ctx, cfg.ObservabilityAddr, metrics, rep) }()
	if cfg.AdminAddr != "" {
		var queueAdmin interface {
			ListState(state string, limit int) ([]*model.Message, error)
			RequeueFromState(state, id string, now time.Time) (*model.Message, error)
		}
		if localQueue := queue.LocalStore(q); localQueue != nil {
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
			errCh <- admin.RunServerWithServices(ctx, cfg.AdminAddr, adminAuthBackend, sup, queueAdmin, rep, pauses, deliveryHistory)
		}()
	}
	if cfg.HTTPInjection.Enabled {
		injectionAPI, apiErr := httpinjection.New(cfg, q, authBackend, bearerBackend)
		if apiErr != nil {
			fatal("HTTP injection init failed", "error", apiErr)
		}
		go func() { errCh <- httpinjection.Run(ctx, cfg, injectionAPI) }()
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

func runDMARCReports(ctx context.Context, store *dmarcreport.Store, backend queue.Backend, hostname string, metrics *observability.Metrics) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	flush := func() {
		flushCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := store.Flush(flushCtx, backend, hostname, "postmaster@"+hostname, time.Now().UTC()); err != nil && ctx.Err() == nil {
			metrics.Counter("dmarc_aggregate_flush_errors_total").Inc()
			slog.Warn("DMARC aggregate report flush failed", "component", "dmarc_report", "error", err)
		} else if err == nil {
			metrics.Counter("dmarc_aggregate_flush_runs_total").Inc()
		}
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			flush()
		}
	}
}

func fatal(msg string, args ...any) {
	base := []any{"component", "main"}
	base = append(base, args...)
	slog.Error(msg, base...)
	os.Exit(1)
}
