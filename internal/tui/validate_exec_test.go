package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

// stubValidator implements BundleValidator for testing exec steps.
type stubValidator struct {
	resolveResult string
	resolveErr    error
	readResult    []byte
	readErr       error
	schemaErrs    []bundledef.ValidationError
	loadResult    *bundledef.Def
	loadErr       error
	validateErr   error
}

func (m *stubValidator) Resolve(_ string) (string, error) {
	return m.resolveResult, m.resolveErr
}

func (m *stubValidator) ReadFile(_ string) ([]byte, error) {
	return m.readResult, m.readErr
}

func (m *stubValidator) ValidateSchema(_ []byte) []bundledef.ValidationError {
	return m.schemaErrs
}

func (m *stubValidator) Load(_ string) (*bundledef.Def, error) {
	return m.loadResult, m.loadErr
}

func (m *stubValidator) ValidateAssets(_ *bundledef.Def, _ string) error {
	return m.validateErr
}

func TestExecSchemaStep_Valid(t *testing.T) {
	t.Parallel()

	v := &stubValidator{schemaErrs: nil}
	result := execSchemaStep(v, []byte("valid yaml"), stepSchema, time.Now())

	if result.err != nil {
		t.Errorf("expected no error, got %v", result.err)
	}

	if result.stepIndex != stepSchema {
		t.Errorf("stepIndex = %d, want %d", result.stepIndex, stepSchema)
	}
}

func TestExecSchemaStep_Invalid(t *testing.T) {
	t.Parallel()

	v := &stubValidator{
		schemaErrs: []bundledef.ValidationError{
			{Path: "/name", Message: "required"},
			{Path: "/version", Message: "invalid format"},
		},
	}

	result := execSchemaStep(v, []byte("bad yaml"), stepSchema, time.Now())

	if result.err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExecResolveStep_Success(t *testing.T) {
	t.Parallel()

	v := &stubValidator{
		resolveResult: "/path/to/musher.yaml",
		readResult:    []byte("name: test"),
	}

	result := execResolveStep(v, "/work", stepResolve, time.Now())

	if result.err != nil {
		t.Errorf("expected no error, got %v", result.err)
	}

	if string(result.yamlData) != "name: test" {
		t.Errorf("yamlData = %q, want %q", result.yamlData, "name: test")
	}
}

func TestExecResolveStep_ResolveError(t *testing.T) {
	t.Parallel()

	v := &stubValidator{
		resolveErr: errors.New("not found"),
	}

	result := execResolveStep(v, "/work", stepResolve, time.Now())

	if result.err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExecResolveStep_ReadError(t *testing.T) {
	t.Parallel()

	v := &stubValidator{
		resolveResult: "/path/to/musher.yaml",
		readErr:       errors.New("read failed"),
	}

	result := execResolveStep(v, "/work", stepResolve, time.Now())

	if result.err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExecDefinitionStep_ValidateFails(t *testing.T) {
	t.Parallel()

	// An empty Def fails Validate() — verify the step reports the error.
	v := &stubValidator{loadResult: &bundledef.Def{}}

	result := execDefinitionStep(v, "/work", stepDefinition, time.Now())

	if result.err == nil {
		t.Fatal("expected validation error for empty Def")
	}
}

func TestExecDefinitionStep_LoadError(t *testing.T) {
	t.Parallel()

	v := &stubValidator{
		loadErr: errors.New("parse error"),
	}

	result := execDefinitionStep(v, "/work", stepDefinition, time.Now())

	if result.err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExecAssetsStep_Success(t *testing.T) {
	t.Parallel()

	v := &stubValidator{}
	result := execAssetsStep(v, &bundledef.Def{}, "/work", stepAssets, time.Now())

	if result.err != nil {
		t.Errorf("expected no error, got %v", result.err)
	}
}

func TestExecAssetsStep_Error(t *testing.T) {
	t.Parallel()

	v := &stubValidator{
		validateErr: errors.New("missing file"),
	}

	result := execAssetsStep(v, &bundledef.Def{}, "/work", stepAssets, time.Now())

	if result.err == nil {
		t.Fatal("expected error, got nil")
	}
}
