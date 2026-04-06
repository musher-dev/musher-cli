// Package workflow contains feature-orchestration logic for Musher CLI commands.
//
// Workflow functions own sequencing and decision-making for pull, run, push, and
// init flows. They accept abstract interfaces for I/O (Progress for spinners,
// Confirmer for prompts) and receive pre-built dependencies rather than loading
// config or constructing clients internally. This keeps the package free of
// presentation concerns and testable without cobra.
package workflow

// Progress reports status for a long-running operation.
// *output.Spinner structurally satisfies this interface.
type Progress interface {
	Start()
	Stop()
	StopWithSuccess(message string)
	StopWithFailure(message string)
	UpdateMessage(message string)
}

// ProgressFactory creates Progress instances.
// In production, this wraps output.Writer.Spinner().
type ProgressFactory interface {
	NewProgress(message string) Progress
}

// Confirmer asks the user yes/no questions.
// In production, this wraps prompt.Prompter.Confirm().
type Confirmer interface {
	Confirm(message string, defaultYes bool) (bool, error)
}

// Selector asks the user to pick from a list of options.
// In production, this wraps prompt.Prompter.Select().
type Selector interface {
	Select(message string, options []string) (int, error)
}

// NopProgress is a no-op [Progress] implementation for testing.
type NopProgress struct{}

// Start is a no-op.
func (NopProgress) Start() {}

// Stop is a no-op.
func (NopProgress) Stop() {}

// StopWithSuccess is a no-op.
func (NopProgress) StopWithSuccess(string) {}

// StopWithFailure is a no-op.
func (NopProgress) StopWithFailure(string) {}

// UpdateMessage is a no-op.
func (NopProgress) UpdateMessage(string) {}

// NopProgressFactory returns [NopProgress] instances.
type NopProgressFactory struct{}

// NewProgress returns a [NopProgress].
func (NopProgressFactory) NewProgress(string) Progress { return NopProgress{} }
