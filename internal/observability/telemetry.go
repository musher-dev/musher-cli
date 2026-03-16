package observability

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// TelemetryConfig holds the configuration for OpenTelemetry tracing.
type TelemetryConfig struct {
	Enabled     bool
	Endpoint    string
	ServiceName string
	Namespace   string
	Version     string
	Commit      string
	Environment string
}

// TelemetryShutdown gracefully flushes and shuts down the telemetry pipeline.
type TelemetryShutdown func(ctx context.Context) error

// SetupTelemetry initializes the OpenTelemetry tracing pipeline.
func SetupTelemetry(ctx context.Context, cfg *TelemetryConfig) (TelemetryShutdown, error) {
	if cfg == nil || !cfg.Enabled {
		return noopShutdown, nil
	}

	origTP := otel.GetTracerProvider()
	origPropagator := otel.GetTextMapPropagator()
	origErrorHandler := otel.GetErrorHandler()

	serviceName := cfg.ServiceName
	if serviceName == "" {
		if envName := os.Getenv("OTEL_SERVICE_NAME"); envName != "" {
			serviceName = envName
		} else {
			serviceName = "musher"
		}
	}

	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "musher"
	}

	environment := cfg.Environment
	if environment == "" {
		if envVal := os.Getenv("OTEL_ENVIRONMENT"); envVal != "" {
			environment = envVal
		} else {
			environment = "development"
		}
	}

	attrs := []attribute.KeyValue{
		attribute.String("service.name", serviceName),
		attribute.String("service.version", cfg.Version),
		attribute.String("service.namespace", namespace),
		attribute.String("deployment.environment", environment),
	}

	if cfg.Commit != "" {
		attrs = append(attrs, attribute.String("service.commit", cfg.Commit))
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(attrs...),
	)
	if err != nil {
		return noopShutdown, fmt.Errorf("merge otel resource: %w", err)
	}

	exporterOpts := []otlptracehttp.Option{
		otlptracehttp.WithCompression(otlptracehttp.GzipCompression),
	}

	if cfg.Endpoint != "" {
		exporterOpts = append(exporterOpts, otlptracehttp.WithEndpoint(cfg.Endpoint))
	}

	exporter, err := otlptracehttp.New(ctx, exporterOpts...)
	if err != nil {
		return noopShutdown, fmt.Errorf("create otel exporter: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {}))

	return func(shutdownCtx context.Context) error {
		err := provider.Shutdown(shutdownCtx)

		otel.SetTracerProvider(origTP)
		otel.SetTextMapPropagator(origPropagator)
		otel.SetErrorHandler(origErrorHandler)

		if err != nil {
			return fmt.Errorf("shutdown otel provider: %w", err)
		}

		return nil
	}, nil
}

// Tracer returns a named tracer from the global TracerProvider.
func Tracer(name string) trace.Tracer {
	return otel.GetTracerProvider().Tracer(name)
}

// IsTelemetryEnabled checks the OTEL_ENABLED env var.
func IsTelemetryEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_ENABLED")))
	return v == "1" || v == "true" || v == "yes"
}

func noopShutdown(context.Context) error { return nil }
