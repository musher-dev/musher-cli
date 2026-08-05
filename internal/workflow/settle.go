package workflow

import "time"

// Deployment phases as reported by the platform.
//
// These are plain strings, not a Go enum type: the `exhaustive` linter is
// enabled, and the server's vocabulary is additive by design — a new phase
// would break the build at every switch rather than degrade gracefully.
const (
	PhaseDraft        = "DRAFT"
	PhaseProvisioning = "PROVISIONING"
	PhaseActive       = "ACTIVE"
	PhaseDegraded     = "DEGRADED"
	PhaseFailed       = "FAILED"
	PhaseSuspended    = "SUSPENDED"
	PhaseDeleted      = "DELETED"
)

// Readiness values.
const (
	ReadinessReady    = "READY"
	ReadinessNotReady = "NOT_READY"
	ReadinessUnknown  = "UNKNOWN"
)

// Outcome is the CLI's verdict on a deployment snapshot.
type Outcome int

// Outcome values.
const (
	// OutcomePending means keep watching.
	OutcomePending Outcome = iota
	OutcomeSucceeded
	OutcomeFailed
	// OutcomeAborted means someone else changed the deployment out from under
	// this watch (suspended or deleted elsewhere).
	OutcomeAborted
	OutcomeTimedOut
)

// String renders the outcome for logs and JSON.
func (o Outcome) String() string {
	switch o {
	case OutcomePending:
		return "PENDING"
	case OutcomeSucceeded:
		return "SUCCEEDED"
	case OutcomeFailed:
		return "FAILED"
	case OutcomeAborted:
		return "ABORTED"
	case OutcomeTimedOut:
		return "TIMED_OUT"
	default:
		return "UNKNOWN"
	}
}

// Terminal reports whether the outcome ends the watch.
func (o Outcome) Terminal() bool { return o != OutcomePending }

// DeploymentSnapshot is the subset of deployment status the watcher reasons about.
type DeploymentSnapshot struct {
	Phase     string
	Readiness string
	Health    string
	// HasPublicURL is true once a PUBLIC endpoint has been published.
	HasPublicURL bool
}

// WaitTarget selects how much has to be true before a deploy is called done.
type WaitTarget int

// WaitTarget values.
const (
	// WaitReady settles as soon as the deployment is ACTIVE and READY.
	WaitReady WaitTarget = iota
	// WaitURL additionally requires a published public URL.
	WaitURL
)

// SettleOptions tunes the terminal predicate.
type SettleOptions struct {
	// DegradedGrace is how long a deployment may sit in DEGRADED before the
	// CLI calls it failed. DEGRADED -> ACTIVE is a legal recovery, so failing
	// instantly would be wrong; but CI cannot wait forever either.
	DegradedGrace time.Duration
	// Timeout bounds the whole watch.
	Timeout time.Duration
	// WaitFor selects the success condition.
	WaitFor WaitTarget
}

// DefaultSettleOptions returns the defaults used by `musher deploy`.
func DefaultSettleOptions() SettleOptions {
	return SettleOptions{
		DegradedGrace: time.Minute,
		Timeout:       15 * time.Minute,
		WaitFor:       WaitReady,
	}
}

// Settle classifies a deployment snapshot.
//
// It is a pure function of a snapshot plus two timestamps, which is what lets
// the SSE path and the polling fallback share one implementation and settle
// identically — the only difference between them is latency.
//
// DELETED is the server's only terminal phase, which is correct for the state
// machine and useless for a CLI: FAILED is recoverable via :retry, so the
// server keeps it live. The CLI needs its own notion of "stop watching".
//
// started is when the watch began; degradedSince is when the deployment first
// entered DEGRADED during this watch (zero if it is not currently degraded).
func Settle(
	snap DeploymentSnapshot,
	started, degradedSince, now time.Time,
	opts SettleOptions,
) Outcome {
	// Success is checked first so a deployment that reaches ACTIVE on the same
	// tick it would have timed out is still reported as a success.
	if snap.Phase == PhaseActive && snap.Readiness == ReadinessReady {
		if opts.WaitFor == WaitURL && !snap.HasPublicURL {
			return pendingUnlessExpired(started, now, opts)
		}

		return OutcomeSucceeded
	}

	switch snap.Phase {
	case PhaseFailed:
		return OutcomeFailed
	case PhaseDeleted, PhaseSuspended:
		// Someone else acted on this deployment. Do not fight them, and do not
		// report their action as our failure.
		return OutcomeAborted
	case PhaseDegraded:
		if !degradedSince.IsZero() && opts.DegradedGrace > 0 &&
			now.Sub(degradedSince) >= opts.DegradedGrace {
			return OutcomeFailed
		}
	}

	return pendingUnlessExpired(started, now, opts)
}

func pendingUnlessExpired(started, now time.Time, opts SettleOptions) Outcome {
	if opts.Timeout > 0 && !started.IsZero() && now.Sub(started) >= opts.Timeout {
		return OutcomeTimedOut
	}

	return OutcomePending
}

// TrackDegraded maintains the degradedSince timestamp across snapshots. It
// returns the timestamp to carry into the next Settle call.
//
// Resetting on any transition out of DEGRADED is what makes the grace window a
// window rather than a cumulative budget: a deployment that flaps in and out of
// DEGRADED is unhealthy but recovering, and should not be failed for the sum of
// its dips.
func TrackDegraded(previous time.Time, phase string, now time.Time) time.Time {
	if phase != PhaseDegraded {
		return time.Time{}
	}

	if previous.IsZero() {
		return now
	}

	return previous
}
