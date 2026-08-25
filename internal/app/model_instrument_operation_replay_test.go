package app

import (
	"context"
	"reflect"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

type modelReplayRunner struct {
	calls   int
	state   domain.CallState
	failure domain.FailureCategory
}

func (r *modelReplayRunner) Run(_ context.Context, call domain.InstrumentCall) domain.InstrumentAttempt {
	r.calls++
	return domain.InstrumentAttempt{
		AttemptNo:      len(call.Attempts) + 1,
		State:          r.state,
		Failure:        r.failure,
		AtLogicalTime:  call.NextRetryAt,
		RawResponseSum: "instrument-result",
	}
}

func TestModel_InstrumentOperationReplayReturnsFirstResult(t *testing.T) {
	tests := []struct {
		name            string
		state           domain.CallState
		failure         domain.FailureCategory
		wantState       string
		wantNextRetryAt int64
	}{
		{
			name:            "successful call",
			state:           domain.CallSucceeded,
			wantState:       "SUCCEEDED",
			wantNextRetryAt: 0,
		},
		{
			name:            "retryable failed call",
			state:           domain.CallFailed,
			failure:         domain.FailureTimeout,
			wantState:       "FAILED",
			wantNextRetryAt: 1001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &testClock{now: 1000}
			runner := &modelReplayRunner{state: tt.state, failure: tt.failure}
			svc := newTestService(t, runner, clock)
			taskID := createAndLock(t, svc)
			driveToObservation(t, svc, taskID)
			ctx := context.Background()
			req := StartInstrumentCallRequest{
				Generation:     1,
				InstrumentType: domain.InstrumentMoistureMeter,
				TargetObject:   "egg-card-1",
				RequestDigest:  "request-digest",
			}

			firstStatus, firstBody, err := svc.StartInstrumentCall(ctx, "op-instrument", taskID, req)
			if err != nil {
				t.Fatalf("first instrument call: %v", err)
			}
			replayStatus, replayBody, err := svc.StartInstrumentCall(ctx, "op-instrument", taskID, req)
			if err != nil {
				t.Fatalf("replayed instrument call: %v", err)
			}

			if replayStatus != firstStatus {
				t.Fatalf("replay status = %d, want original status %d", replayStatus, firstStatus)
			}
			if !reflect.DeepEqual(replayBody, firstBody) {
				t.Fatalf("replay body = %#v, want original body %#v", replayBody, firstBody)
			}
			if runner.calls != 1 {
				t.Fatalf("runner calls = %d, want 1", runner.calls)
			}
			call := decodeCall(t, replayBody)
			attempts, ok := call["attempts"].([]map[string]any)
			if !ok {
				t.Fatalf("attempts is %T, want []map[string]any", call["attempts"])
			}
			if len(attempts) != 1 {
				t.Fatalf("attempt count = %d, want 1", len(attempts))
			}
			if call["state"] != tt.wantState {
				t.Fatalf("call state = %v, want %s", call["state"], tt.wantState)
			}
			if call["nextRetryAt"] != tt.wantNextRetryAt {
				t.Fatalf("nextRetryAt = %v, want %d", call["nextRetryAt"], tt.wantNextRetryAt)
			}
		})
	}
}
