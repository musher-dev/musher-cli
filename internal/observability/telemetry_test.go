package observability

import (
	"testing"
)

func TestIsTelemetryEnabled(t *testing.T) {
	tests := []struct {
		name   string
		envVal string
		want   bool
	}{
		{"unset", "", false},
		{"1", "1", true},
		{"true", "true", true},
		{"TRUE", "TRUE", true},
		{"yes", "yes", true},
		{"YES", "YES", true},
		{"0", "0", false},
		{"false", "false", false},
		{"no", "no", false},
		{"random", "random", false},
		{"with spaces", "  true  ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_ENABLED", tt.envVal)

			got := IsTelemetryEnabled()
			if got != tt.want {
				t.Errorf("IsTelemetryEnabled() = %v, want %v (env=%q)", got, tt.want, tt.envVal)
			}
		})
	}
}

func TestNoopShutdown(t *testing.T) {
	t.Parallel()

	err := noopShutdown(t.Context())
	if err != nil {
		t.Fatalf("noopShutdown error = %v", err)
	}
}

func TestSetupTelemetryDisabled(t *testing.T) {
	t.Parallel()

	shutdown, err := SetupTelemetry(t.Context(), nil)
	if err != nil {
		t.Fatalf("SetupTelemetry(nil) error = %v", err)
	}

	if err := shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
}

func TestSetupTelemetryDisabledExplicit(t *testing.T) {
	t.Parallel()

	cfg := &TelemetryConfig{
		Enabled: false,
	}

	shutdown, err := SetupTelemetry(t.Context(), cfg)
	if err != nil {
		t.Fatalf("SetupTelemetry(disabled) error = %v", err)
	}

	if err := shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
}

func TestTracerReturnsNonNil(t *testing.T) {
	t.Parallel()

	tracer := Tracer("test")
	if tracer == nil {
		t.Fatal("Tracer returned nil")
	}
}

func TestSetupTelemetryEnabled(t *testing.T) {
	// This test uses a non-existent endpoint to avoid real connections.
	// The setup should succeed (exporter creation is lazy/async).
	cfg := &TelemetryConfig{
		Enabled:     true,
		Endpoint:    "localhost:1",
		ServiceName: "test-service",
		Namespace:   "test-ns",
		Version:     "0.0.1-test",
		Commit:      "abc123",
		Environment: "test",
	}

	ctx := t.Context()

	shutdown, err := SetupTelemetry(ctx, cfg)
	if err != nil {
		t.Fatalf("SetupTelemetry error = %v", err)
	}

	// Shutdown should restore original providers
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
}

func TestSetupTelemetryDefaults(t *testing.T) {
	// Test that defaults are applied when fields are empty.
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_ENVIRONMENT", "")

	cfg := &TelemetryConfig{
		Enabled:  true,
		Endpoint: "localhost:1",
	}

	ctx := t.Context()

	shutdown, err := SetupTelemetry(ctx, cfg)
	if err != nil {
		t.Fatalf("SetupTelemetry error = %v", err)
	}

	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
}

func TestSetupTelemetryWithEnvDefaults(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "env-service")
	t.Setenv("OTEL_ENVIRONMENT", "staging")

	cfg := &TelemetryConfig{
		Enabled:  true,
		Endpoint: "localhost:1",
	}

	ctx := t.Context()

	shutdown, err := SetupTelemetry(ctx, cfg)
	if err != nil {
		t.Fatalf("SetupTelemetry error = %v", err)
	}

	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
}

func TestSetupTelemetryWithCommit(t *testing.T) {
	cfg := &TelemetryConfig{
		Enabled:  true,
		Endpoint: "localhost:1",
		Commit:   "deadbeef",
	}

	ctx := t.Context()

	shutdown, err := SetupTelemetry(ctx, cfg)
	if err != nil {
		t.Fatalf("SetupTelemetry error = %v", err)
	}

	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
}
