package client

// Validation scopes accepted by the :validate routes (core v2 §6.1). Each one
// holds every obligation of the one before it.
const (
	ScopeParser      = "PARSER"
	ScopeStructural  = "STRUCTURAL"
	ScopeDocument    = "DOCUMENT"
	ScopePublication = "PUBLICATION"
	ScopeDeployment  = "DEPLOYMENT"
)

// Diagnostic severities. Only ERROR fails a document; WARNING is advice.
const (
	SeverityError   = "ERROR"
	SeverityWarning = "WARNING"
)

// Validation verdicts carried by SpecValidationResult.Status.
const (
	ValidationValid      = "VALID"
	ValidationInvalid    = "INVALID"
	ValidationIncomplete = "INCOMPLETE"
)

// Spec documents the CLI writes.
const (
	// SpecVersionV2 is the only document version this CLI writes.
	SpecVersionV2 = "v2"
	// DocumentKindComponent discriminates a component document.
	DocumentKindComponent = "COMPONENT"
	// DocumentKindBlueprint discriminates a blueprint document.
	DocumentKindBlueprint = "BLUEPRINT"
)

// Version lifecycle statuses shared by components and blueprints.
const (
	VersionStatusDraft      = "DRAFT"
	VersionStatusPublished  = "PUBLISHED"
	VersionStatusDeprecated = "DEPRECATED"
	VersionStatusWithdrawn  = "WITHDRAWN"
)

// SpecRelatedLocation is a location in another artifact that a cross-document
// finding involves.
type SpecRelatedLocation struct {
	Artifact string `json:"artifact"`
	Pointer  string `json:"pointer"`
}

// SpecDiagnostic is one finding on a submitted document.
type SpecDiagnostic struct {
	// Pointer is an RFC 6901 JSON Pointer into the submitted document.
	Pointer string `json:"pointer"`
	// Code is the stable diagnostic code, such as ERR_UNKNOWN_ENDPOINT.
	Code string `json:"code"`
	// Phase is the validation layer that found it.
	Phase string `json:"phase,omitempty"`
	// Message is human-readable and never carries a secret value.
	Message string `json:"message"`
	// Severity is ERROR or WARNING.
	Severity string `json:"severity"`
	// Stage is the resolution stage, or "" outside the RESOLUTION phase.
	Stage string `json:"stage,omitempty"`
	// File names the document in a multi-file submission, when known.
	File string `json:"file,omitempty"`
	// Related lists locations in other artifacts.
	Related []SpecRelatedLocation `json:"related,omitempty"`
}

// IsError reports whether the diagnostic fails the document.
func (d *SpecDiagnostic) IsError() bool { return d.Severity == SeverityError }

// SpecDeferredObligation is a rule the validator could not complete for want
// of context.
type SpecDeferredObligation struct {
	Rule    string `json:"rule"`
	Pointer string `json:"pointer"`
	Missing string `json:"missing"`
	File    string `json:"file,omitempty"`
}

// SpecValidationResult is the outcome of validating a document under a
// claimed scope.
type SpecValidationResult struct {
	Status          string                   `json:"status"`
	ValidationScope string                   `json:"validationScope"`
	Phase           string                   `json:"phase"`
	Diagnostics     []SpecDiagnostic         `json:"diagnostics"`
	Deferred        []SpecDeferredObligation `json:"deferred"`
}

// Errors returns only the diagnostics that fail the document.
func (r *SpecValidationResult) Errors() []SpecDiagnostic {
	if r == nil {
		return nil
	}

	var errs []SpecDiagnostic

	for index := range r.Diagnostics {
		if r.Diagnostics[index].IsError() {
			errs = append(errs, r.Diagnostics[index])
		}
	}

	return errs
}

// Warnings returns only the advisory diagnostics.
func (r *SpecValidationResult) Warnings() []SpecDiagnostic {
	if r == nil {
		return nil
	}

	var warnings []SpecDiagnostic

	for index := range r.Diagnostics {
		if !r.Diagnostics[index].IsError() {
			warnings = append(warnings, r.Diagnostics[index])
		}
	}

	return warnings
}

// validateRequest is the body of the components:validate and
// blueprints:validate routes.
type validateRequest struct {
	Document any    `json:"document"`
	Scope    string `json:"scope,omitempty"`
}
