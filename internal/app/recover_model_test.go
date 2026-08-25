package app

import (
	"context"
	"reflect"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/ledger"
	"silkworm-egg-cold-storage-gate/internal/store"
)

type recoveryRecordingRunner struct {
	callIDs []string
}

func (r *recoveryRecordingRunner) Run(_ context.Context, call domain.InstrumentCall) domain.InstrumentAttempt {
	r.callIDs = append(r.callIDs, call.ID)
	return domain.InstrumentAttempt{
		AttemptNo:     len(call.Attempts) + 1,
		State:         domain.CallSucceeded,
		AtLogicalTime: call.NextRetryAt,
	}
}

func TestModel_StartupRecoveryResumesEligibleInstrumentCalls(t *testing.T) {
	ctx := context.Background()
	const recoveryTime int64 = 1000
	runner := &recoveryRecordingRunner{}
	svc := newTestService(t, runner, &testClock{now: recoveryTime})
	taskID := createAndLock(t, svc)

	callCases := []struct {
		name         string
		id           string
		state        domain.CallState
		nextRetryAt  int64
		attemptCount int
		wantResumed  bool
	}{
		{name: "due failed call", id: "call-a-due-failed", state: domain.CallFailed, nextRetryAt: recoveryTime, attemptCount: 1, wantResumed: true},
		{name: "due pending call left before first attempt", id: "call-z-due-pending", state: domain.CallPending, nextRetryAt: recoveryTime - 1, wantResumed: true},
		{name: "successful call", id: "call-b-succeeded", state: domain.CallSucceeded, nextRetryAt: 0, attemptCount: 1},
		{name: "retry instant not reached", id: "call-c-future", state: domain.CallFailed, nextRetryAt: recoveryTime + 1, attemptCount: 1},
		{name: "retry budget exhausted", id: "call-d-exhausted", state: domain.CallFailed, nextRetryAt: 0, attemptCount: ledger.MaxAttempts},
	}

	for _, tc := range callCases {
		t.Run("persist/"+tc.name, func(t *testing.T) {
			attempts := make([]domain.InstrumentAttempt, tc.attemptCount)
			for i := range attempts {
				attempts[i] = domain.InstrumentAttempt{
					AttemptNo: i + 1,
					State:     domain.CallFailed,
					Failure:   domain.FailureTimeout,
				}
			}
			if tc.state == domain.CallSucceeded {
				attempts[len(attempts)-1].State = domain.CallSucceeded
			}
			call := domain.InstrumentCall{
				ID:             tc.id,
				InstrumentType: domain.InstrumentMoistureMeter,
				TargetObject:   tc.id,
				RequestDigest:  "request-" + tc.id,
				TaskID:         taskID,
				Generation:     1,
				State:          tc.state,
				Attempts:       attempts,
				NextRetryAt:    tc.nextRetryAt,
			}
			if err := svc.store.WithTx(ctx, func(tx store.Tx) error {
				return tx.SaveInstrumentCall(ctx, call)
			}); err != nil {
				t.Fatalf("save instrument call: %v", err)
			}
		})
	}

	leaseCases := []struct {
		name      string
		resource  domain.ResourceType
		number    string
		expiresAt int64
		wantState domain.LeaseState
	}{
		{name: "overdue lease expires", resource: domain.ResourceIncubationCabin, number: "recovery-cabin", expiresAt: recoveryTime - 1, wantState: domain.LeaseExpired},
		{name: "unexpired lease remains active", resource: domain.ResourceSlide, number: "recovery-slide", expiresAt: recoveryTime + 1, wantState: domain.LeaseActive},
	}
	for _, tc := range leaseCases {
		t.Run("persist/"+tc.name, func(t *testing.T) {
			lease := domain.ResourceLease{
				TaskID: taskID, ResourceType: tc.resource, ResourceNo: tc.number,
				Generation: 1, State: domain.LeaseActive, AcquiredAt: 900, ExpiresAt: tc.expiresAt,
			}
			if err := svc.store.WithTx(ctx, func(tx store.Tx) error {
				return tx.AcquireLease(ctx, lease)
			}); err != nil {
				t.Fatalf("save lease: %v", err)
			}
		})
	}

	result, err := svc.Recover(ctx, recoveryTime)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}

	t.Run("recovery counts match mutations", func(t *testing.T) {
		if result.ExpiredLeases != 1 {
			t.Errorf("ExpiredLeases = %d, want 1", result.ExpiredLeases)
		}
		if result.ResumedCalls != 2 {
			t.Errorf("ResumedCalls = %d, want 2", result.ResumedCalls)
		}
	})

	t.Run("eligible calls run in call ID order", func(t *testing.T) {
		want := []string{"call-a-due-failed", "call-z-due-pending"}
		if !reflect.DeepEqual(runner.callIDs, want) {
			t.Fatalf("runner call IDs = %v, want %v", runner.callIDs, want)
		}
	})

	for _, tc := range callCases {
		t.Run("call/"+tc.name, func(t *testing.T) {
			var got *domain.InstrumentCall
			if err := svc.store.WithTx(ctx, func(tx store.Tx) error {
				var err error
				got, err = tx.GetInstrumentCall(ctx, tc.id)
				return err
			}); err != nil {
				t.Fatalf("load instrument call: %v", err)
			}
			wantAttempts := tc.attemptCount
			wantState := tc.state
			if tc.wantResumed {
				wantAttempts++
				wantState = domain.CallSucceeded
			}
			if len(got.Attempts) != wantAttempts {
				t.Errorf("attempt count = %d, want %d", len(got.Attempts), wantAttempts)
			}
			if got.State != wantState {
				t.Errorf("state = %s, want %s", got.State.String(), wantState.String())
			}
		})
	}

	for _, tc := range leaseCases {
		t.Run("lease/"+tc.name, func(t *testing.T) {
			var leases []domain.ResourceLease
			if err := svc.store.WithTx(ctx, func(tx store.Tx) error {
				var err error
				leases, err = tx.ListLeases(ctx, taskID)
				return err
			}); err != nil {
				t.Fatalf("list leases: %v", err)
			}
			for _, lease := range leases {
				if lease.ResourceType == tc.resource && lease.ResourceNo == tc.number {
					if lease.State != tc.wantState {
						t.Errorf("state = %s, want %s", lease.State.String(), tc.wantState.String())
					}
					return
				}
			}
			t.Fatalf("lease %s not found", tc.number)
		})
	}
}
