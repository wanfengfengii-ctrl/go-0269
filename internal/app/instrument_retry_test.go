package app

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

// faultScript is a four-failure-then-success script for the moisture meter.
func faultScript() map[string][]ScriptStep {
	return map[string][]ScriptStep{
		"egg-card-1": {
			{Succeed: false, Failure: domain.FailureRejected},
			{Succeed: false, Failure: domain.FailureDisconnected},
			{Succeed: false, Failure: domain.FailureTimeout},
			{Succeed: false, Failure: domain.FailureFormatError},
			{Succeed: true},
		},
	}
}

func decodeCall(t *testing.T, body any) map[string]any {
	t.Helper()
	m, ok := body.(map[string]any)
	if !ok {
		t.Fatalf("body is %T, want map[string]any", body)
	}
	return m
}

func TestInstrumentRetryDeterministic(t *testing.T) {
	clock := &testClock{now: 1000}
	svc := newTestService(t, NewScriptRunner(faultScript()), clock)
	id := createAndLock(t, svc)
	driveToObservation(t, svc, id)
	ctx := context.Background()

	_, body, err := svc.StartInstrumentCall(ctx, "op-start", id, StartInstrumentCallRequest{
		Generation: 1, InstrumentType: domain.InstrumentMoistureMeter, TargetObject: "egg-card-1", RequestDigest: "req",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	call := decodeCall(t, body)
	callID := call["id"].(string)
	if got := call["nextRetryAt"].(int64); got != 1001 {
		t.Fatalf("next retry after attempt 1 = %v, want 1001", got)
	}

	// Each failure schedules the next retry at now+1, now+2, now+4, now+8.
	wantRetryAt := []int64{1001, 1003, 1007, 1015}
	for i, at := range wantRetryAt {
		clock.advance(at - clock.now)
		_, body, err = svc.RetryInstrumentCall(ctx, fmt.Sprintf("op-retry-%d", i+1), callID, RetryRequest{Generation: 1})
		if err != nil {
			t.Fatalf("retry %d: %v", i+1, err)
		}
		call = decodeCall(t, body)
	}
	if call["state"] != "SUCCEEDED" {
		t.Fatalf("final call state = %v, want SUCCEEDED", call["state"])
	}
	attempts := call["attempts"].([]map[string]any)
	if len(attempts) != 5 {
		t.Fatalf("attempt count = %d, want 5", len(attempts))
	}
	failures := []string{"REJECTED", "DISCONNECTED", "TIMEOUT", "FORMAT_ERROR"}
	for i, f := range failures {
		a := attempts[i]
		if a["failure"] != f {
			t.Fatalf("attempt %d failure = %v, want %s", i+1, a["failure"], f)
		}
	}
	if last := attempts[4]; last["state"] != "SUCCEEDED" {
		t.Fatalf("final attempt state = %v, want SUCCEEDED", last["state"])
	}
}

func TestInstrumentNotRetryableBeforeInstant(t *testing.T) {
	clock := &testClock{now: 1000}
	svc := newTestService(t, NewScriptRunner(faultScript()), clock)
	id := createAndLock(t, svc)
	driveToObservation(t, svc, id)
	ctx := context.Background()

	_, body, err := svc.StartInstrumentCall(ctx, "op-start", id, StartInstrumentCallRequest{
		Generation: 1, InstrumentType: domain.InstrumentMoistureMeter, TargetObject: "egg-card-1", RequestDigest: "req",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	callID := decodeCall(t, body)["id"].(string)
	// No clock advance: the next retry instant (1001) has not arrived.
	_, _, err = svc.RetryInstrumentCall(ctx, "op-retry-early", callID, RetryRequest{Generation: 1})
	if err == nil {
		t.Fatal("expected NOT_RETRYABLE_YET")
	}
	de, ok := err.(*domain.DomainError)
	if !ok || de.Code != domain.ErrInvalidStateTransition {
		t.Fatalf("error = %v, want INVALID_STATE_TRANSITION", err)
	}
}

func TestInstrumentRestartRecoveryConsistent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "instrument.db")
	clock := &testClock{now: 1000}
	svc := newTestServiceDB(t, dbPath, NewScriptRunner(faultScript()), clock)
	id := createAndLock(t, svc)
	driveToObservation(t, svc, id)
	ctx := context.Background()

	_, body, err := svc.StartInstrumentCall(ctx, "op-start", id, StartInstrumentCallRequest{
		Generation: 1, InstrumentType: domain.InstrumentMoistureMeter, TargetObject: "egg-card-1", RequestDigest: "req",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	callID := decodeCall(t, body)["id"].(string)

	// Restart: a fresh service over the same DB, with an always-succeeding
	// runner, must see the persisted attempt and append a new one.
	clock.advance(1)
	svc2 := newTestServiceDB(t, dbPath, nil, clock)
	_, body2, err := svc2.RetryInstrumentCall(ctx, "op-retry-after-restart", callID, RetryRequest{Generation: 1})
	if err != nil {
		t.Fatalf("retry after restart: %v", err)
	}
	call2 := decodeCall(t, body2)
	attempts := call2["attempts"].([]map[string]any)
	if len(attempts) != 2 {
		t.Fatalf("attempts after restart = %d, want 2", len(attempts))
	}
	if attempts[0]["failure"] != "REJECTED" {
		t.Fatalf("persisted attempt failure = %v, want REJECTED", attempts[0]["failure"])
	}
	if call2["state"] != "SUCCEEDED" {
		t.Fatalf("state after restart retry = %v, want SUCCEEDED", call2["state"])
	}
}
