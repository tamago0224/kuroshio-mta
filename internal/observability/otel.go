package observability

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func InitOTEL(ctx context.Context, cfg config.Config) (func(context.Context) error, error) {
	if !cfg.OTELEnabled {
		return func(context.Context) error { return nil }, nil
	}

	endpoint := strings.TrimSpace(cfg.OTELExporterOTLPEndpoint)
	if endpoint == "" {
		return nil, errors.New("otel_enabled is true but otel_exporter_otlp_endpoint is empty")
	}

	opts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(endpoint)}
	if cfg.OTELExporterOTLPInsecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	attrs := []attribute.KeyValue{
		attribute.String("service.name", cfg.OTELServiceName),
	}
	if v := strings.TrimSpace(cfg.OTELServiceVersion); v != "" {
		attrs = append(attrs, attribute.String("service.version", v))
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes("", attrs...),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.OTELTraceSampleRatio))),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	slog.Info(
		"otel tracing enabled",
		"component",
		"observability",
		"exporter",
		"otlphttp",
		"endpoint",
		endpoint,
		"service_name",
		cfg.OTELServiceName,
		"sample_ratio",
		cfg.OTELTraceSampleRatio,
	)

	return tp.Shutdown, nil
}
