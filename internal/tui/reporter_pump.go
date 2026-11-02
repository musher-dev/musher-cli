package tui

import (
	"fmt"
	"time"

	"github.com/musher-dev/musher-cli/internal/build/core"
)

// DeployReporter receives the narrative of a deploy run from the TUI. Its
// Step/Activity/Note methods are exactly workflow.Reporter's, so the command
// layer passes it straight to workflow.Deploy; BuildEvent receives the engine
// events of a deploy that builds its image first.
type DeployReporter interface {
	Step(id, state, note string)
	Activity(kind, message string, at time.Time)
	Note(level, format string, args ...any)
	BuildEvent(event core.Event)
}

// Deploy step states, as workflow.Reporter reports them.
const (
	stepRunning = "running"
	stepDone    = "done"
	stepFailed  = "failed"
	stepSkipped = "skipped"
)

// Deploy step ids that change the screen's behavior. They are the stable
// tokens of workflow.Step*; the command layer's tests assert every workflow
// step has a label here.
const (
	stepIDBuild = "build"
	stepIDPush  = "push"
	stepIDWatch = "watch"
)

// deployStepLabels names each workflow step for the checklist.
var deployStepLabels = map[string]string{
	"validate":  "Validate musher.yaml",
	"build":     "Build image",
	"push":      "Push image",
	"resolve":   "Resolve environment",
	"verify":    "Check registry access",
	"component": "Publish component",
	"blueprint": "Publish blueprint",
	"deploy":    "Submit deployment",
	"watch":     "Roll out",
	"endpoint":  "Resolve URL",
}

// DeployStepLabel returns the checklist label for a workflow step id.
func DeployStepLabel(id string) (string, bool) {
	label, ok := deployStepLabels[id]

	return label, ok
}

// deployItemKind classifies a pumped deploy item.
type deployItemKind int

const (
	deployItemStep deployItemKind = iota
	deployItemActivity
	deployItemNote
	deployItemBuild
	deployItemDone
)

// deployItem is one report pumped from the deploy goroutine.
type deployItem struct {
	gen     int
	kind    deployItemKind
	id      string
	state   string
	text    string
	level   string
	at      time.Time
	event   *core.Event
	outcome *DeployOutcome
	err     error
}

// reporterPump implements DeployReporter by sending every report to the
// screen over a channel. Sends give up once done closes, so a canceled run
// never blocks on a screen that stopped reading.
type reporterPump struct {
	gen   int
	items chan<- deployItem
	done  <-chan struct{}
}

func (p *reporterPump) send(item *deployItem) {
	item.gen = p.gen

	select {
	case p.items <- *item:
	case <-p.done:
	}
}

// Step implements DeployReporter.
func (p *reporterPump) Step(id, state, note string) {
	p.send(&deployItem{kind: deployItemStep, id: id, state: state, text: note})
}

// Activity implements DeployReporter.
func (p *reporterPump) Activity(kind, message string, at time.Time) {
	p.send(&deployItem{kind: deployItemActivity, id: kind, text: message, at: at})
}

// Note implements DeployReporter.
func (p *reporterPump) Note(level, format string, args ...any) {
	p.send(&deployItem{kind: deployItemNote, level: level, text: fmt.Sprintf(format, args...)})
}

// BuildEvent implements DeployReporter. It takes the event by value to match
// workflow.DeployBuild.Events.
//
//nolint:gocritic // hugeParam: the signature is fixed by workflow.DeployBuild.Events.
func (p *reporterPump) BuildEvent(event core.Event) {
	p.send(&deployItem{kind: deployItemBuild, event: &event})
}
