package workflow

import (
	"testing"
	"time"
)

func TestSettle(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	opts := SettleOptions{
		DegradedGrace: time.Minute,
		Timeout:       10 * time.Minute,
		WaitFor:       WaitReady,
	}

	tests := []struct {
		name          string
		snap          DeploymentSnapshot
		degradedSince time.Time
		elapsed       time.Duration
		opts          SettleOptions
		want          Outcome
		// why documents the rule under test, so a future edit that flips an
		// expectation has to argue with the reason rather than just the value.
		why string
	}{
		{
			name:    "draft keeps waiting",
			snap:    DeploymentSnapshot{Phase: PhaseDraft},
			elapsed: time.Second,
			opts:    opts,
			want:    OutcomePending,
			why:     "DRAFT precedes provisioning",
		},
		{
			name:    "provisioning keeps waiting",
			snap:    DeploymentSnapshot{Phase: PhaseProvisioning},
			elapsed: time.Minute,
			opts:    opts,
			want:    OutcomePending,
			why:     "the long middle of a deploy",
		},
		{
			name:    "active and ready succeeds",
			snap:    DeploymentSnapshot{Phase: PhaseActive, Readiness: ReadinessReady},
			elapsed: time.Minute,
			opts:    opts,
			want:    OutcomeSucceeded,
			why:     "the happy path",
		},
		{
			name:    "active but not ready keeps waiting",
			snap:    DeploymentSnapshot{Phase: PhaseActive, Readiness: ReadinessNotReady},
			elapsed: time.Minute,
			opts:    opts,
			want:    OutcomePending,
			why:     "ACTIVE alone is not enough; health checks may still be failing",
		},
		{
			name:    "failed fails",
			snap:    DeploymentSnapshot{Phase: PhaseFailed},
			elapsed: time.Minute,
			opts:    opts,
			want:    OutcomeFailed,
			why:     "provisioning failed before ever going active",
		},
		{
			name:    "deleted aborts",
			snap:    DeploymentSnapshot{Phase: PhaseDeleted},
			elapsed: time.Minute,
			opts:    opts,
			want:    OutcomeAborted,
			why:     "someone deleted it while we watched; not our failure",
		},
		{
			name:    "suspended aborts",
			snap:    DeploymentSnapshot{Phase: PhaseSuspended},
			elapsed: time.Minute,
			opts:    opts,
			want:    OutcomeAborted,
			why:     "someone suspended it; do not fight them",
		},
		{
			name:          "degraded inside the grace window keeps waiting",
			snap:          DeploymentSnapshot{Phase: PhaseDegraded},
			degradedSince: start.Add(30 * time.Second),
			elapsed:       time.Minute,
			opts:          opts,
			want:          OutcomePending,
			why:           "DEGRADED -> ACTIVE is a legal recovery, so failing instantly would be wrong",
		},
		{
			name:          "degraded past the grace window fails",
			snap:          DeploymentSnapshot{Phase: PhaseDegraded},
			degradedSince: start,
			elapsed:       90 * time.Second,
			opts:          opts,
			want:          OutcomeFailed,
			why:           "CI cannot sit in DEGRADED forever",
		},
		{
			name:          "degraded with grace disabled keeps waiting",
			snap:          DeploymentSnapshot{Phase: PhaseDegraded},
			degradedSince: start,
			elapsed:       time.Hour,
			opts:          SettleOptions{DegradedGrace: 0, Timeout: 2 * time.Hour},
			want:          OutcomePending,
			why:           "a zero grace disables the rule rather than failing immediately",
		},
		{
			name:    "timeout expires",
			snap:    DeploymentSnapshot{Phase: PhaseProvisioning},
			elapsed: 11 * time.Minute,
			opts:    opts,
			want:    OutcomeTimedOut,
			why:     "bounded watch",
		},
		{
			name:    "success on the timeout tick still succeeds",
			snap:    DeploymentSnapshot{Phase: PhaseActive, Readiness: ReadinessReady},
			elapsed: 10 * time.Minute,
			opts:    opts,
			want:    OutcomeSucceeded,
			why:     "success is checked before expiry, so a deploy that lands exactly at the deadline is not reported as a timeout",
		},
		{
			name:    "wait-for-url pends without a public URL",
			snap:    DeploymentSnapshot{Phase: PhaseActive, Readiness: ReadinessReady},
			elapsed: time.Minute,
			opts:    SettleOptions{Timeout: 10 * time.Minute, WaitFor: WaitURL},
			want:    OutcomePending,
			why:     "the edge route has not published yet",
		},
		{
			name:    "wait-for-url succeeds with a public URL",
			snap:    DeploymentSnapshot{Phase: PhaseActive, Readiness: ReadinessReady, HasPublicURL: true},
			elapsed: time.Minute,
			opts:    SettleOptions{Timeout: 10 * time.Minute, WaitFor: WaitURL},
			want:    OutcomeSucceeded,
			why:     "route published",
		},
		{
			name:    "wait-for-url can still time out",
			snap:    DeploymentSnapshot{Phase: PhaseActive, Readiness: ReadinessReady},
			elapsed: 11 * time.Minute,
			opts:    SettleOptions{Timeout: 10 * time.Minute, WaitFor: WaitURL},
			want:    OutcomeTimedOut,
			why:     "a route that never publishes must not hang forever",
		},
		{
			name:    "no timeout configured never expires",
			snap:    DeploymentSnapshot{Phase: PhaseProvisioning},
			elapsed: 100 * time.Hour,
			opts:    SettleOptions{Timeout: 0},
			want:    OutcomePending,
			why:     "zero means unbounded, for --watch sessions a human is driving",
		},
		{
			name:    "unknown phase keeps waiting",
			snap:    DeploymentSnapshot{Phase: "SOMETHING_NEW"},
			elapsed: time.Minute,
			opts:    opts,
			want:    OutcomePending,
			why:     "the server vocabulary is additive; an unrecognized phase must not crash or fail the deploy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Settle(tt.snap, start, tt.degradedSince, start.Add(tt.elapsed), tt.opts)
			if got != tt.want {
				t.Errorf("Settle() = %v, want %v\nreason: %s", got, tt.want, tt.why)
			}
		})
	}
}

func TestTrackDegraded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Minute)

	t.Run("records the first degraded moment", func(t *testing.T) {
		t.Parallel()

		got := TrackDegraded(time.Time{}, PhaseDegraded, now)
		if !got.Equal(now) {
			t.Errorf("got %v, want %v", got, now)
		}
	})

	t.Run("keeps the original moment while still degraded", func(t *testing.T) {
		t.Parallel()

		got := TrackDegraded(now, PhaseDegraded, later)
		if !got.Equal(now) {
			t.Errorf("got %v, want the original %v", got, now)
		}
	})

	// A deployment flapping in and out of DEGRADED is unhealthy but recovering.
	// Accumulating its dips would fail it for the sum rather than for a
	// sustained outage, which is not what the grace window means.
	t.Run("resets when leaving degraded", func(t *testing.T) {
		t.Parallel()

		if got := TrackDegraded(now, PhaseActive, later); !got.IsZero() {
			t.Errorf("got %v, want zero", got)
		}
	})
}

func TestOutcomeString(t *testing.T) {
	t.Parallel()

	tests := map[Outcome]string{
		OutcomePending:   "PENDING",
		OutcomeSucceeded: "SUCCEEDED",
		OutcomeFailed:    "FAILED",
		OutcomeAborted:   "ABORTED",
		OutcomeTimedOut:  "TIMED_OUT",
		Outcome(99):      "UNKNOWN",
	}

	for outcome, want := range tests {
		if got := outcome.String(); got != want {
			t.Errorf("Outcome(%d).String() = %q, want %q", outcome, got, want)
		}
	}
}

func TestOutcomeTerminal(t *testing.T) {
	t.Parallel()

	if OutcomePending.Terminal() {
		t.Error("pending must not be terminal")
	}

	for _, outcome := range []Outcome{
		OutcomeSucceeded, OutcomeFailed, OutcomeAborted, OutcomeTimedOut,
	} {
		if !outcome.Terminal() {
			t.Errorf("%v must be terminal", outcome)
		}
	}
}
