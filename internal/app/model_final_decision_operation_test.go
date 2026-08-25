package app

import (
	"context"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/store"
)

func TestModel_FinalDecisionOperationIDIsBoundToDecisionType(t *testing.T) {
	cases := []struct {
		name       string
		secondType domain.FinalDecisionType
		wantReplay bool
	}{
		{name: "same admit endpoint replays", secondType: domain.FinalAdmit, wantReplay: true},
		{name: "isolate endpoint conflicts", secondType: domain.FinalIsolate},
		{name: "cancel endpoint conflicts", secondType: domain.FinalCancel},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t, nil, nil)
			taskID := driveToReadyToAdmit(t, svc)
			ctx := context.Background()
			req := DecideRequest{Generation: 1, DecidedBy: "person-c"}

			status, firstBody, err := svc.Decide(ctx, "op-final", taskID, domain.FinalAdmit, req)
			if err != nil || status != 200 {
				t.Fatalf("initial admit = status %d, err %v", status, err)
			}
			first := firstBody.(map[string]any)
			credential, _ := first["admitCredential"].(string)
			if first["taskId"] != taskID || first["type"] != "ADMIT" || credential == "" {
				t.Fatalf("initial admit body = %#v", first)
			}

			status, secondBody, err := svc.Decide(ctx, "op-final", taskID, tc.secondType, req)
			if tc.wantReplay {
				if err != nil || status != 200 {
					t.Fatalf("same decision replay = status %d, err %v", status, err)
				}
				replay := secondBody.(map[string]any)
				if replay["taskId"] != taskID || replay["type"] != "ADMIT" || replay["admitCredential"] != credential {
					t.Fatalf("replayed body = %#v, want original ADMIT credential", replay)
				}
			} else {
				de, ok := err.(*domain.DomainError)
				if !ok || de.Code != domain.ErrOperationConflict {
					t.Fatalf("reused operation ID = status %d, body %#v, err %v; want OPERATION_CONFLICT", status, secondBody, err)
				}
			}

			var stored *domain.FinalDecision
			if err := svc.Store().WithTx(ctx, func(tx store.Tx) error {
				var err error
				stored, err = tx.GetFinalDecision(ctx, taskID)
				return err
			}); err != nil {
				t.Fatalf("read final decision: %v", err)
			}
			if stored == nil || stored.TaskID != taskID || stored.Type != domain.FinalAdmit || stored.AdmitCredential != credential {
				t.Fatalf("stored final decision = %#v, want sole original ADMIT", stored)
			}
		})
	}
}
