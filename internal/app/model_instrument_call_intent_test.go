package app

import (
	"context"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

func TestModel_InstrumentCallIntentIdentity(t *testing.T) {
	tests := []struct {
		name          string
		operationID   string
		changeRequest func(*StartInstrumentCallRequest)
		wantReplay    bool
		wantConflict  bool
	}{
		{
			name:        "fresh operation ID replays identical persisted call",
			operationID: "op-resend",
			wantReplay:  true,
		},
		{
			name:        "different target creates an independent call",
			operationID: "op-other-target",
			changeRequest: func(req *StartInstrumentCallRequest) {
				req.TargetObject = "egg-card-2"
			},
		},
		{
			name:        "different request digest creates an independent call",
			operationID: "op-other-digest",
			changeRequest: func(req *StartInstrumentCallRequest) {
				req.RequestDigest = "request-v2"
			},
		},
		{
			name:        "same operation ID with different content remains a conflict",
			operationID: "op-initial",
			changeRequest: func(req *StartInstrumentCallRequest) {
				req.RequestDigest = "request-v2"
			},
			wantConflict: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &testClock{now: 1000}
			runner := NewScriptRunner(map[string][]ScriptStep{
				"egg-card-1": {
					{Failure: domain.FailureRejected},
					{Failure: domain.FailureDisconnected},
				},
				"egg-card-2": {
					{Failure: domain.FailureTimeout},
				},
			})
			svc := newTestService(t, runner, clock)
			taskID := createAndLock(t, svc)
			driveToObservation(t, svc, taskID)
			ctx := context.Background()
			initialRequest := StartInstrumentCallRequest{
				Generation:     1,
				InstrumentType: domain.InstrumentMoistureMeter,
				TargetObject:   "egg-card-1",
				RequestDigest:  "request-v1",
			}

			status, initialBody, err := svc.StartInstrumentCall(ctx, "op-initial", taskID, initialRequest)
			if err != nil || status != 200 {
				t.Fatalf("initial call = status %d, error %v", status, err)
			}
			initial := initialBody.(map[string]any)
			initialID := initial["id"].(string)
			initialAttempts := initial["attempts"].([]map[string]any)
			if initial["state"] != "FAILED" || len(initialAttempts) != 1 || initialAttempts[0]["failure"] != "REJECTED" || initial["nextRetryAt"] != int64(1001) {
				t.Fatalf("initial persisted call = %#v, want one REJECTED attempt and retry at 1001", initial)
			}

			secondRequest := initialRequest
			if tt.changeRequest != nil {
				tt.changeRequest(&secondRequest)
			}
			status, secondBody, err := svc.StartInstrumentCall(ctx, tt.operationID, taskID, secondRequest)
			if tt.wantConflict {
				de, ok := err.(*domain.DomainError)
				if !ok || de.Code != domain.ErrOperationConflict {
					t.Fatalf("second call error = %v, want OPERATION_CONFLICT", err)
				}
				return
			}
			if err != nil || status != 200 {
				t.Fatalf("second call = status %d, error %v", status, err)
			}
			second := secondBody.(map[string]any)
			secondAttempts := second["attempts"].([]map[string]any)

			if tt.wantReplay {
				if second["id"] != initialID || second["state"] != "FAILED" || len(secondAttempts) != 1 || secondAttempts[0]["failure"] != "REJECTED" || second["nextRetryAt"] != int64(1001) {
					t.Fatalf("resent intent = %#v, want unchanged persisted call %#v", second, initial)
				}

				clock.advance(1)
				_, retryBody, err := svc.RetryInstrumentCall(ctx, "op-recover", initialID, RetryRequest{Generation: 1})
				if err != nil {
					t.Fatalf("retry preserved call: %v", err)
				}
				recovered := retryBody.(map[string]any)
				recoveredAttempts := recovered["attempts"].([]map[string]any)
				if len(recoveredAttempts) != 2 || recoveredAttempts[0]["failure"] != "REJECTED" || recoveredAttempts[1]["failure"] != "DISCONNECTED" || recovered["nextRetryAt"] != int64(1003) {
					t.Fatalf("recovered call = %#v, want REJECTED then DISCONNECTED and retry at 1003", recovered)
				}
				return
			}

			if second["id"] == initialID {
				t.Fatalf("new intent reused call ID %q", initialID)
			}
			if len(secondAttempts) != 1 {
				t.Fatalf("new call attempts = %d, want 1", len(secondAttempts))
			}
		})
	}
}
