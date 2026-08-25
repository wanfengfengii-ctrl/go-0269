package app

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/ledger"
	"silkworm-egg-cold-storage-gate/internal/store"
)

type modelRetryRunner struct {
	steps []ScriptStep
	calls int
}

func (r *modelRetryRunner) Run(_ context.Context, call domain.InstrumentCall) domain.InstrumentAttempt {
	r.calls++
	step := ScriptStep{Succeed: true}
	if len(r.steps) > 0 {
		step = r.steps[0]
		r.steps = r.steps[1:]
	}
	attempt := domain.InstrumentAttempt{
		AttemptNo:     len(call.Attempts) + 1,
		State:         domain.CallSucceeded,
		AtLogicalTime: call.NextRetryAt,
	}
	if !step.Succeed {
		attempt.State = domain.CallFailed
		attempt.Failure = step.Failure
		if attempt.Failure == 0 {
			attempt.Failure = domain.FailureRejected
		}
	}
	return attempt
}

func modelRetryCall(t *testing.T, svc *Service, opID, callID string, generation int64) (int, any, error) {
	t.Helper()
	return svc.RetryInstrumentCall(context.Background(), opID, callID, RetryRequest{Generation: generation})
}

func modelStartFailedCall(t *testing.T, svc *Service, taskID string) string {
	t.Helper()
	_, body, err := svc.StartInstrumentCall(context.Background(), "op-start-"+taskID, taskID, StartInstrumentCallRequest{
		Generation:     1,
		InstrumentType: domain.InstrumentMoistureMeter,
		TargetObject:   "egg-card-1",
		RequestDigest:  "moisture-request",
	})
	if err != nil {
		t.Fatalf("start instrument call: %v", err)
	}
	return decodeCall(t, body)["id"].(string)
}

func modelRequireReason(t *testing.T, err error, code domain.ErrorCode, reason string) {
	t.Helper()
	de, ok := err.(*domain.DomainError)
	if !ok || de.Code != code || len(de.Reasons) != 1 || de.Reasons[0].Code != reason {
		t.Fatalf("error = %#v, want %s with reason %s", err, code, reason)
	}
}

func TestModel_RetryInstrumentCallOperationReplay(t *testing.T) {
	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "same operation replays recorded result without another attempt",
			run: func(t *testing.T) {
				clock := &testClock{now: 1000}
				runner := &modelRetryRunner{steps: []ScriptStep{
					{Failure: domain.FailureRejected},
					{Failure: domain.FailureTimeout},
					{Succeed: true},
				}}
				svc := newTestService(t, runner, clock)
				taskID := createAndLock(t, svc)
				driveToObservation(t, svc, taskID)
				callID := modelStartFailedCall(t, svc, taskID)

				clock.advance(1)
				const opID = "op-retry-timeout-replay"
				if status, _, err := modelRetryCall(t, svc, opID, callID, 1); err != nil || status != 200 {
					t.Fatalf("first retry = (%d, %v), want status 200", status, err)
				}
				if runner.calls != 2 {
					t.Fatalf("runner calls after first retry = %d, want 2", runner.calls)
				}

				var recorded opResponse
				var before domain.InstrumentCall
				err := svc.Store().WithTx(context.Background(), func(tx store.Tx) error {
					op, err := tx.GetOperation(context.Background(), opID)
					if err != nil {
						return err
					}
					if op == nil {
						return fmt.Errorf("operation %q was not recorded", opID)
					}
					if err := json.Unmarshal(op.Response, &recorded); err != nil {
						return err
					}
					call, err := tx.GetInstrumentCall(context.Background(), callID)
					if err != nil {
						return err
					}
					before = *call
					return nil
				})
				if err != nil {
					t.Fatalf("read recorded retry: %v", err)
				}

				clock.advance(100)
				status, replayBody, err := modelRetryCall(t, svc, opID, callID, 1)
				if err != nil {
					t.Fatalf("replay retry: %v", err)
				}
				gotBody, err := json.Marshal(replayBody)
				if err != nil {
					t.Fatalf("marshal replay body: %v", err)
				}
				if status != recorded.HTTPStatus || string(gotBody) != string(recorded.Body) {
					t.Fatalf("replay = status %d body %s, want recorded status %d body %s", status, gotBody, recorded.HTTPStatus, recorded.Body)
				}
				if runner.calls != 2 {
					t.Fatalf("runner calls after replay = %d, want 2", runner.calls)
				}

				var after domain.InstrumentCall
				if err := svc.Store().WithTx(context.Background(), func(tx store.Tx) error {
					call, err := tx.GetInstrumentCall(context.Background(), callID)
					if err == nil {
						after = *call
					}
					return err
				}); err != nil {
					t.Fatalf("read call after replay: %v", err)
				}
				if !reflect.DeepEqual(after, before) {
					t.Fatalf("call changed on replay:\n before: %#v\n  after: %#v", before, after)
				}
				if len(after.Attempts) != 2 || after.State != domain.CallFailed || after.NextRetryAt != 1003 {
					t.Fatalf("call after replay = state %s attempts %d nextRetryAt %d, want FAILED/2/1003", after.State, len(after.Attempts), after.NextRetryAt)
				}
				for _, attempt := range after.Attempts {
					if attempt.AtLogicalTime == 0 {
						t.Fatalf("replay appended zero-time attempt: %#v", after.Attempts)
					}
				}
			},
		},
		{
			name: "same operation with different generation conflicts",
			run: func(t *testing.T) {
				clock := &testClock{now: 1000}
				runner := &modelRetryRunner{steps: []ScriptStep{{Failure: domain.FailureRejected}, {Failure: domain.FailureTimeout}}}
				svc := newTestService(t, runner, clock)
				taskID := createAndLock(t, svc)
				driveToObservation(t, svc, taskID)
				callID := modelStartFailedCall(t, svc, taskID)
				clock.advance(1)
				const opID = "op-retry-conflict"
				if _, _, err := modelRetryCall(t, svc, opID, callID, 1); err != nil {
					t.Fatalf("first retry: %v", err)
				}
				_, _, err := modelRetryCall(t, svc, opID, callID, 2)
				modelRequireReason(t, err, domain.ErrOperationConflict, "OPERATION_CONFLICT")
				if runner.calls != 2 {
					t.Fatalf("runner calls after conflict = %d, want 2", runner.calls)
				}
			},
		},
		{
			name: "retry before scheduled time remains rejected",
			run: func(t *testing.T) {
				clock := &testClock{now: 1000}
				runner := &modelRetryRunner{steps: []ScriptStep{{Failure: domain.FailureRejected}}}
				svc := newTestService(t, runner, clock)
				taskID := createAndLock(t, svc)
				driveToObservation(t, svc, taskID)
				callID := modelStartFailedCall(t, svc, taskID)
				_, _, err := modelRetryCall(t, svc, "op-retry-too-early", callID, 1)
				modelRequireReason(t, err, domain.ErrInvalidStateTransition, "NOT_RETRYABLE_YET")
				if runner.calls != 1 {
					t.Fatalf("runner calls after early rejection = %d, want 1", runner.calls)
				}
			},
		},
		{
			name: "successful call remains non-retryable",
			run: func(t *testing.T) {
				clock := &testClock{now: 1000}
				runner := &modelRetryRunner{steps: []ScriptStep{{Succeed: true}}}
				svc := newTestService(t, runner, clock)
				taskID := createAndLock(t, svc)
				driveToObservation(t, svc, taskID)
				callID := modelStartFailedCall(t, svc, taskID)
				_, _, err := modelRetryCall(t, svc, "op-retry-succeeded", callID, 1)
				modelRequireReason(t, err, domain.ErrInvalidStateTransition, "CALL_ALREADY_SUCCEEDED")
				if runner.calls != 1 {
					t.Fatalf("runner calls after succeeded rejection = %d, want 1", runner.calls)
				}
			},
		},
		{
			name: "exhausted call remains non-retryable",
			run: func(t *testing.T) {
				clock := &testClock{now: 1000}
				steps := make([]ScriptStep, ledger.MaxAttempts)
				for i := range steps {
					steps[i].Failure = domain.FailureTimeout
				}
				runner := &modelRetryRunner{steps: steps}
				svc := newTestService(t, runner, clock)
				taskID := createAndLock(t, svc)
				driveToObservation(t, svc, taskID)
				callID := modelStartFailedCall(t, svc, taskID)
				for attempt := 2; attempt <= ledger.MaxAttempts; attempt++ {
					clock.now += ledger.RetryDelay(attempt - 1)
					if _, _, err := modelRetryCall(t, svc, fmt.Sprintf("op-retry-budget-%d", attempt), callID, 1); err != nil {
						t.Fatalf("retry attempt %d: %v", attempt, err)
					}
				}
				_, _, err := modelRetryCall(t, svc, "op-retry-budget-exhausted", callID, 1)
				modelRequireReason(t, err, domain.ErrInvalidStateTransition, "RETRY_BUDGET_EXHAUSTED")
				if runner.calls != ledger.MaxAttempts {
					t.Fatalf("runner calls after exhausted rejection = %d, want %d", runner.calls, ledger.MaxAttempts)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
