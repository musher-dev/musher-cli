package workflow

import (
	"strconv"
	"strings"

	"github.com/musher-dev/musher-cli/internal/client"
)

// Documents a SpecError can be about.
const (
	DocumentComponent  = "component"
	DocumentBlueprint  = "blueprint"
	DocumentDeployment = "deployment"
)

// generatedLabel marks a pointer that nothing in musher.yaml explains.
const generatedLabel = "(generated document)"

// Diagnostic is one platform finding, located in the user's musher.yaml when
// the CLI can tell where it came from.
type Diagnostic struct {
	// Path is the musher.yaml path, such as spec.workload.endpoints.http, or
	// the raw JSON pointer when Generated is true.
	Path string
	// Generated is true when the finding is about a field the CLI generated,
	// so Path is a pointer into the compiled document.
	Generated bool
	Message   string
	Code      string
	Severity  string
}

// String renders "path: message [CODE]".
func (d *Diagnostic) String() string {
	return locate(d.Path, d.Generated) + d.Message + codeSuffix(d.Code)
}

// Deferral is a validation obligation the platform could not complete for
// want of context.
type Deferral struct {
	Path      string
	Generated bool
	Rule      string
	Missing   string
}

// String renders "path: deferred RULE (needs MISSING)".
func (d *Deferral) String() string {
	text := locate(d.Path, d.Generated) + "deferred " + d.Rule
	if d.Missing != "" {
		text += " (needs " + d.Missing + ")"
	}

	return text
}

// SpecError means the platform judged a compiled document invalid. It is the
// user's to fix in musher.yaml, and nothing was written when it came from the
// pre-write validation.
type SpecError struct {
	// Document is DocumentComponent, DocumentBlueprint or DocumentDeployment.
	Document    string
	Diagnostics []Diagnostic
	Deferred    []Deferral
	// Detail is the server's own summary, when it sent one.
	Detail string
}

// Error implements the error interface.
func (e *SpecError) Error() string {
	count := len(e.Diagnostics)
	if count == 0 && e.Detail != "" {
		return "the platform rejected the " + e.Document + ": " + e.Detail
	}

	noun := "problems"
	if count == 1 {
		noun = "problem"
	}

	return "the platform rejected the " + e.Document + " compiled from musher.yaml (" +
		strconv.Itoa(count) + " " + noun + ")"
}

// Lines renders every diagnostic and deferral, one per line.
func (e *SpecError) Lines() []string {
	lines := make([]string, 0, len(e.Diagnostics)+len(e.Deferred))

	for index := range e.Diagnostics {
		lines = append(lines, e.Diagnostics[index].String())
	}

	for index := range e.Deferred {
		lines = append(lines, e.Deferred[index].String())
	}

	return lines
}

// ConflictError means a natural key is held by something this deploy cannot
// adopt — a slug owned by another lineage, a revision that will not move, a
// resource managed elsewhere.
type ConflictError struct {
	// Resource is DocumentComponent, DocumentBlueprint or DocumentDeployment.
	Resource string
	// Slug is the natural key that conflicted.
	Slug string
	Err  error
}

// Error implements the error interface.
func (e *ConflictError) Error() string {
	return "the " + e.Resource + " " + strconv.Quote(e.Slug) + " conflicts with the platform's state: " + e.Err.Error()
}

// Unwrap exposes the transport error.
func (e *ConflictError) Unwrap() error { return e.Err }

// ForeignDeploymentError means a deployment with the requested name exists
// but runs a blueprint this deploy does not own. Rolling it onto another
// blueprint would silently replace someone else's application.
type ForeignDeploymentError struct {
	Name        string
	BlueprintID string
}

// Error implements the error interface.
func (e *ForeignDeploymentError) Error() string {
	if e.BlueprintID == "" {
		return "deployment " + strconv.Quote(e.Name) + " was not created from a blueprint musher deploy manages"
	}

	return "deployment " + strconv.Quote(e.Name) + " runs blueprint " + e.BlueprintID +
		", which musher deploy does not manage"
}

// locate renders the "path: " prefix of a diagnostic line.
func locate(path string, generated bool) string {
	switch {
	case generated && path == "":
		return generatedLabel + ": "
	case generated:
		return path + " " + generatedLabel + ": "
	default:
		return path + ": "
	}
}

func codeSuffix(code string) string {
	if strings.TrimSpace(code) == "" {
		return ""
	}

	return " [" + code + "]"
}

// mapDiagnostics locates server diagnostics in musher.yaml.
func mapDiagnostics(source provenance, diagnostics []client.SpecDiagnostic) []Diagnostic {
	mapped := make([]Diagnostic, 0, len(diagnostics))

	for index := range diagnostics {
		item := &diagnostics[index]
		path, generated := place(source, item.Pointer)
		mapped = append(mapped, Diagnostic{
			Path:      path,
			Generated: generated,
			Message:   item.Message,
			Code:      item.Code,
			Severity:  item.Severity,
		})
	}

	return mapped
}

// mapProblemItems locates the errors member of a 422 problem document.
func mapProblemItems(source provenance, items []client.ProblemItem) []Diagnostic {
	mapped := make([]Diagnostic, 0, len(items))

	for index := range items {
		item := &items[index]
		path, generated := place(source, item.Location())
		mapped = append(mapped, Diagnostic{
			Path:      path,
			Generated: generated,
			Message:   item.Text(),
			Code:      item.Code,
			Severity:  item.Severity,
		})
	}

	return mapped
}

// mapDeferred locates deferred obligations in musher.yaml.
func mapDeferred(source provenance, deferred []client.SpecDeferredObligation) []Deferral {
	mapped := make([]Deferral, 0, len(deferred))

	for index := range deferred {
		item := &deferred[index]
		path, generated := place(source, item.Pointer)
		mapped = append(mapped, Deferral{
			Path:      path,
			Generated: generated,
			Rule:      item.Rule,
			Missing:   item.Missing,
		})
	}

	return mapped
}

// place returns the musher.yaml path for pointer, or the pointer itself and
// generated=true when the user's file does not explain it.
func place(source provenance, pointer string) (string, bool) {
	if path, ok := source.lookup(pointer); ok {
		return path, false
	}

	return pointer, true
}

// specErrorFrom turns a 422 spec-document-invalid or resolution-invalid
// response into a SpecError, or returns nil for any other error.
func specErrorFrom(err error, document string, source provenance) *SpecError {
	problem := client.ProblemOf(err)
	if problem == nil {
		return nil
	}

	switch problem.Slug() {
	case client.SlugSpecDocumentInvalid, client.SlugResolutionInvalid:
	default:
		return nil
	}

	return &SpecError{
		Document:    document,
		Diagnostics: mapProblemItems(source, problem.Errors),
		Deferred:    mapDeferred(source, problem.Deferred),
		Detail:      strings.TrimSpace(problem.Detail),
	}
}
