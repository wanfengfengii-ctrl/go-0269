package app

import (
	"context"
	"sync"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

// ScriptStep is one deterministic outcome of an instrument adapter call. When
// Succeed is true the call returns a successful attempt; otherwise it fails
// with the given category (defaulting to REJECTED when unset). RawResponse is
// the adapter's response summary for audit.
type ScriptStep struct {
	Succeed     bool
	Failure     domain.FailureCategory
	RawResponse string
}

// ScriptRunner is a deterministic, in-memory instrument adapter used by tests
// and smoke simulations. It is keyed by target object: each call pops the next
// step for that target, falling back to success when the script is exhausted.
type ScriptRunner struct {
	mu    sync.Mutex
	steps map[string][]ScriptStep
}

// NewScriptRunner builds a runner with the given per-target scripts.
func NewScriptRunner(steps map[string][]ScriptStep) *ScriptRunner {
	if steps == nil {
		steps = make(map[string][]ScriptStep)
	}
	return &ScriptRunner{steps: steps}
}

// Run pops and returns the next scripted step for the call's target object.
func (r *ScriptRunner) Run(_ context.Context, call domain.InstrumentCall) domain.InstrumentAttempt {
	r.mu.Lock()
	defer r.mu.Unlock()
	attemptNo := len(call.Attempts) + 1
	queue := r.steps[call.TargetObject]
	if len(queue) > 0 {
		step := queue[0]
		r.steps[call.TargetObject] = queue[1:]
		if !step.Succeed {
			failure := step.Failure
			if failure == 0 {
				failure = domain.FailureRejected
			}
			return domain.InstrumentAttempt{
				AttemptNo:      attemptNo,
				State:          domain.CallFailed,
				Failure:        failure,
				RawResponseSum: step.RawResponse,
				AtLogicalTime:  call.NextRetryAt,
			}
		}
		return domain.InstrumentAttempt{
			AttemptNo:      attemptNo,
			State:          domain.CallSucceeded,
			RawResponseSum: step.RawResponse,
			AtLogicalTime:  call.NextRetryAt,
		}
	}
	// Script exhausted: succeed deterministically.
	return domain.InstrumentAttempt{
		AttemptNo:     attemptNo,
		State:         domain.CallSucceeded,
		AtLogicalTime: call.NextRetryAt,
	}
}
