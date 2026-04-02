package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// checkStatus tracks the state of an individual validation check.
type checkStatus int

const (
	checkPending checkStatus = iota // Not started.
	checkRunning                    // In progress.
	checkPassed                     // Passed.
	checkFailed                     // Failed.
)

// Status indicator characters.
const (
	checkIconPending = "\u25CB" // ○
	checkIconPassed  = "\u2713" // ✓
	checkIconFailed  = "\u2717" // ✗
)

// Validation step indices.
const (
	stepResolve         = 0
	stepSchema          = 1
	stepDefinition      = 2
	stepAssets          = 3
	validationStepCount = 4
)

// validatorUnavailableMsg is the error detail shown when no BundleValidator is configured.
const validatorUnavailableMsg = "bundle validation not available"

// validateCheck is a single validation step with its result.
type validateCheck struct {
	label   string
	status  checkStatus
	detail  string        // error detail on failure
	elapsed time.Duration // wall-clock time for the step
}

// validateStepResultMsg is sent when an async validation step completes.
type validateStepResultMsg struct {
	stepIndex int
	err       error
	elapsed   time.Duration
	bundle    *bundledef.Def // populated after Load step succeeds
	yamlData  []byte         // populated after Resolve step reads file
}

// newValidationChecks returns the four validation checks initialized to pending.
func newValidationChecks() []validateCheck {
	return []validateCheck{
		{label: "Resolve musher.yaml", status: checkPending},
		{label: "Validate schema", status: checkPending},
		{label: "Validate definition", status: checkPending},
		{label: "Validate assets", status: checkPending},
	}
}

// runValidateStep dispatches the appropriate validation function for the given step index.
// It returns a tea.Cmd that sends a validateStepResultMsg when complete.
func runValidateStep(
	validator BundleValidator,
	workDir string,
	stepIndex int,
	bundle *bundledef.Def,
	yamlData []byte,
) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()

		switch stepIndex {
		case stepResolve:
			return execResolveStep(validator, workDir, stepIndex, start)
		case stepSchema:
			return execSchemaStep(validator, yamlData, stepIndex, start)
		case stepDefinition:
			return execDefinitionStep(validator, workDir, stepIndex, start)
		case stepAssets:
			return execAssetsStep(validator, bundle, workDir, stepIndex, start)
		default:
			return validateStepResultMsg{stepIndex: stepIndex, err: repoerrors.Errorf("unknown step %d", stepIndex)}
		}
	}
}

func execResolveStep(validator BundleValidator, workDir string, stepIndex int, start time.Time) validateStepResultMsg {
	yamlPath, err := validator.Resolve(workDir)
	if err != nil {
		return validateStepResultMsg{stepIndex: stepIndex, err: err, elapsed: time.Since(start)}
	}

	data, readErr := validator.ReadFile(yamlPath)
	if readErr != nil {
		return validateStepResultMsg{stepIndex: stepIndex, err: readErr, elapsed: time.Since(start)}
	}

	return validateStepResultMsg{stepIndex: stepIndex, elapsed: time.Since(start), yamlData: data}
}

func execSchemaStep(validator BundleValidator, yamlData []byte, stepIndex int, start time.Time) validateStepResultMsg {
	schemaErrs := validator.ValidateSchema(yamlData)
	if len(schemaErrs) > 0 {
		parts := make([]string, 0, len(schemaErrs))
		for _, e := range schemaErrs {
			parts = append(parts, e.String())
		}

		return validateStepResultMsg{
			stepIndex: stepIndex,
			err:       repoerrors.Errorf("schema validation failed:\n  %s", strings.Join(parts, "\n  ")),
			elapsed:   time.Since(start),
		}
	}

	return validateStepResultMsg{stepIndex: stepIndex, elapsed: time.Since(start)}
}

func execDefinitionStep(validator BundleValidator, workDir string, stepIndex int, start time.Time) validateStepResultMsg {
	def, loadErr := validator.Load(workDir)
	if loadErr != nil {
		return validateStepResultMsg{stepIndex: stepIndex, err: loadErr, elapsed: time.Since(start)}
	}

	if valErr := def.Validate(); valErr != nil {
		return validateStepResultMsg{stepIndex: stepIndex, err: valErr, elapsed: time.Since(start)}
	}

	return validateStepResultMsg{stepIndex: stepIndex, elapsed: time.Since(start), bundle: def}
}

func execAssetsStep(validator BundleValidator, bundle *bundledef.Def, workDir string, stepIndex int, start time.Time) validateStepResultMsg {
	if err := validator.ValidateAssets(bundle, workDir); err != nil {
		return validateStepResultMsg{stepIndex: stepIndex, err: err, elapsed: time.Since(start)}
	}

	return validateStepResultMsg{stepIndex: stepIndex, elapsed: time.Since(start)}
}

// advanceValidation updates the check slice with a step result.
// Returns whether the step passed.
func advanceValidation(checks []validateCheck, msg validateStepResultMsg) bool {
	if msg.stepIndex < 0 || msg.stepIndex >= len(checks) {
		return false
	}

	checks[msg.stepIndex].elapsed = msg.elapsed

	if msg.err != nil {
		checks[msg.stepIndex].status = checkFailed
		checks[msg.stepIndex].detail = msg.err.Error()

		return false
	}

	checks[msg.stepIndex].status = checkPassed

	return true
}

// renderValidationChecks renders the checklist with status indicators.
func renderValidationChecks(checks []validateCheck, sty *styles, spin *spinner.Model) string {
	var buf strings.Builder

	for _, c := range checks {
		var icon string

		switch c.status {
		case checkPending:
			icon = sty.muted.Render(checkIconPending)
		case checkRunning:
			icon = spin.View()
		case checkPassed:
			icon = sty.success.Render(checkIconPassed)
		case checkFailed:
			icon = sty.errStyle.Render(checkIconFailed)
		}

		label := c.label
		if c.status == checkPassed || c.status == checkFailed {
			elapsed := formatElapsed(c.elapsed)
			label += " " + sty.muted.Render(elapsed)
		}

		buf.WriteString(icon + " " + label + "\n")

		if c.status == checkFailed && c.detail != "" {
			// Indent error detail below the failed check.
			for line := range strings.SplitSeq(c.detail, "\n") {
				buf.WriteString("  " + sty.errStyle.Render(line) + "\n")
			}
		}
	}

	return strings.TrimRight(buf.String(), "\n")
}

// formatElapsed formats a duration for display (e.g. "[0.2s]").
func formatElapsed(dur time.Duration) string {
	if dur < time.Millisecond {
		return "[<1ms]"
	}

	if dur < time.Second {
		return fmt.Sprintf("[%dms]", dur.Milliseconds())
	}

	return fmt.Sprintf("[%.1fs]", dur.Seconds())
}
