package app

import (
	"context"
	"reflect"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/store"
)

func TestModel_TerminalRecheckBoundaryPreservesPersistentState(t *testing.T) {
	ctx := context.Background()

	driveToTrigger := func(t *testing.T, svc *Service, unhealthy bool) string {
		t.Helper()
		id := createAndLock(t, svc)
		driveToObservation(t, svc, id)

		var hatched, unhatched, dead int64 = 90, 6, 3
		reading := "0.00"
		if unhealthy {
			hatched, unhatched, dead = 70, 9, 20
			reading = "1.00"
		}
		cells := make([]domain.ObservationCell, 0, 4)
		for _, seal := range []string{"seal-1", "seal-2"} {
			for _, day := range []int{1, 2} {
				cells = append(cells, domain.ObservationCell{
					CardSeal: seal, DayAge: day, Hatched: hatched,
					Unhatched: unhatched, Dead: dead, Damaged: 1,
				})
			}
		}
		if _, _, err := svc.SubmitObservations(ctx, "op-observations", id, ObservationRequest{Generation: 1, Cells: cells}); err != nil {
			t.Fatalf("submit observations: %v", err)
		}
		if _, _, err := svc.VerifyEvidence(ctx, "op-microscopy", id, VerifyEvidenceRequest{
			Generation: 1, Type: domain.EvidenceMicroscopy, CardSeal: "seal-1",
			DayAge: 1, SlideNo: "slide-1", Reading: reading, CallID: "call-microscopy",
		}); err != nil {
			t.Fatalf("verify microscopy: %v", err)
		}
		return id
	}

	driveToReady := func(t *testing.T, svc *Service, id string) {
		t.Helper()
		for i, ev := range []VerifyEvidenceRequest{
			{Generation: 1, Type: domain.EvidenceEnvironment, CardSeal: "seal-1", DayAge: 1, SlideNo: "slide-1", Reading: "25.00", CallID: "call-environment"},
			{Generation: 1, Type: domain.EvidenceMoisture, CardSeal: "seal-1", DayAge: 1, SlideNo: "slide-1", Reading: "10.00", CallID: "call-moisture"},
			{Generation: 1, Type: domain.EvidenceDamage, CardSeal: "seal-1", DayAge: 1, SlideNo: "slide-1", Reading: "0.00", CallID: "call-damage"},
		} {
			if _, _, err := svc.VerifyEvidence(ctx, "op-evidence-"+string(rune('a'+i)), id, ev); err != nil {
				t.Fatalf("verify evidence %d: %v", i, err)
			}
		}
		for _, person := range []string{"person-c", "person-d"} {
			if _, _, err := svc.SubmitReview(ctx, "op-review-"+person, id, ReviewRequest{
				Generation: 1, PersonID: person, Conclusion: "PASS",
			}); err != nil {
				t.Fatalf("submit review for %s: %v", person, err)
			}
		}
		if _, _, err := svc.AcquireLease(ctx, "op-cold-cell", id, LeaseRequest{
			Generation: 1, ResourceType: domain.ResourceColdStorageCell,
			ResourceNo: "cell-1", ExpiresAt: 10000,
		}); err != nil {
			t.Fatalf("acquire cold-storage cell: %v", err)
		}
	}

	taskAndRechecks := func(t *testing.T, svc *Service, id string) (domain.StorageTask, *domain.RecheckCase, *domain.RecheckCase) {
		t.Helper()
		var got domain.StorageTask
		var generationTwo, generationThree *domain.RecheckCase
		if err := svc.store.WithTx(ctx, func(tx store.Tx) error {
			task, err := tx.GetTask(ctx, id)
			if err != nil {
				return err
			}
			got = *task
			if generationTwo, err = tx.GetRecheck(ctx, id, 2); err != nil {
				return err
			}
			generationThree, err = tx.GetRecheck(ctx, id, 3)
			return err
		}); err != nil {
			t.Fatalf("read persisted state: %v", err)
		}
		return got, generationTwo, generationThree
	}

	cases := []struct {
		name     string
		terminal *domain.FinalDecisionType
		mode     string
	}{
		{name: "open task merges the current trigger set", mode: "merge"},
		{name: "old-generation microscopy remains audit-only", mode: "old-evidence"},
		{name: "admitted task rejects recheck", terminal: func() *domain.FinalDecisionType { v := domain.FinalAdmit; return &v }()},
		{name: "isolated task rejects recheck", terminal: func() *domain.FinalDecisionType { v := domain.FinalIsolate; return &v }()},
		{name: "cancelled task rejects recheck", terminal: func() *domain.FinalDecisionType { v := domain.FinalCancel; return &v }()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := &testClock{now: 1000}
			svc := newTestService(t, nil, clock)

			switch tc.mode {
			case "merge":
				id := driveToTrigger(t, svc, true)
				_, body, err := svc.CreateRecheck(ctx, "op-recheck-current", id, RecheckRequest{Generation: 1})
				if err != nil {
					t.Fatalf("create current recheck: %v", err)
				}
				response := body.(map[string]any)
				wantTriggers := []string{"DEAD_RATE_EXCEEDED", "LOW_VITALITY", "SUSPECTED_PEBRINE"}
				if response["recheck"] != true || response["generation"] != int64(2) || !reflect.DeepEqual(response["triggers"], wantTriggers) {
					t.Fatalf("recheck response = %#v, want generation 2 with merged triggers %v", response, wantTriggers)
				}
				persisted, generationTwo, generationThree := taskAndRechecks(t, svc, id)
				if persisted.Generation != 2 || generationTwo == nil || generationTwo.TriggerSummary != "DEAD_RATE_EXCEEDED,LOW_VITALITY,SUSPECTED_PEBRINE" || generationThree != nil {
					t.Fatalf("persisted generation/rechecks = %d, %#v, %#v", persisted.Generation, generationTwo, generationThree)
				}
				return

			case "old-evidence":
				id := driveToTrigger(t, svc, false)
				if _, _, err := svc.VerifyEvidence(ctx, "op-old-suspect", id, VerifyEvidenceRequest{
					Generation: 1, Type: domain.EvidenceMicroscopy, CardSeal: "seal-1",
					DayAge: 1, SlideNo: "slide-1", Reading: "1.00", CallID: "call-old-suspect",
				}); err != nil {
					t.Fatalf("append suspect evidence: %v", err)
				}
				if _, _, err := svc.CreateRecheck(ctx, "op-first-recheck", id, RecheckRequest{Generation: 1}); err != nil {
					t.Fatalf("create first recheck: %v", err)
				}
				if _, _, err := svc.VerifyEvidence(ctx, "op-current-clear", id, VerifyEvidenceRequest{
					Generation: 2, Type: domain.EvidenceMicroscopy, CardSeal: "seal-1",
					DayAge: 1, SlideNo: "slide-1", Reading: "0.00", CallID: "call-current-clear",
				}); err != nil {
					t.Fatalf("append current clear evidence: %v", err)
				}
				_, body, err := svc.CreateRecheck(ctx, "op-current-ruling", id, RecheckRequest{Generation: 2})
				if err != nil {
					t.Fatalf("rule on current generation: %v", err)
				}
				response := body.(map[string]any)
				if response["recheck"] != false || !reflect.DeepEqual(response["triggers"], []string{}) {
					t.Fatalf("current ruling included old evidence: %#v", response)
				}
				persisted, generationTwo, generationThree := taskAndRechecks(t, svc, id)
				if persisted.Generation != 2 || generationTwo == nil || generationThree != nil {
					t.Fatalf("persisted generation/rechecks = %d, %#v, %#v", persisted.Generation, generationTwo, generationThree)
				}
				return
			}

			id := driveToTrigger(t, svc, true)
			driveToReady(t, svc, id)
			if _, _, err := svc.Decide(ctx, "op-final", id, *tc.terminal, DecideRequest{
				Generation: 1, DecidedBy: "person-c",
			}); err != nil {
				t.Fatalf("create terminal decision: %v", err)
			}
			before, beforeGenerationTwo, beforeGenerationThree := taskAndRechecks(t, svc, id)
			clock.advance(37)

			status, body, err := svc.CreateRecheck(ctx, "op-terminal-recheck", id, RecheckRequest{Generation: 1})
			if err == nil {
				t.Fatalf("terminal recheck succeeded: status=%d body=%#v", status, body)
			}
			de, ok := err.(*domain.DomainError)
			if !ok || de.Code != domain.ErrInvalidStateTransition {
				t.Fatalf("terminal recheck error = %v, want INVALID_STATE_TRANSITION", err)
			}

			after, afterGenerationTwo, afterGenerationThree := taskAndRechecks(t, svc, id)
			if after.Generation != before.Generation || after.State != before.State || after.Version != before.Version || after.UpdatedAt != before.UpdatedAt {
				t.Fatalf("terminal task changed: before={generation:%d state:%s version:%d updatedAt:%d} after={generation:%d state:%s version:%d updatedAt:%d}",
					before.Generation, before.State, before.Version, before.UpdatedAt,
					after.Generation, after.State, after.Version, after.UpdatedAt)
			}
			if beforeGenerationTwo != nil || beforeGenerationThree != nil || afterGenerationTwo != nil || afterGenerationThree != nil {
				t.Fatalf("terminal recheck wrote persistence: before=(%#v,%#v) after=(%#v,%#v)",
					beforeGenerationTwo, beforeGenerationThree, afterGenerationTwo, afterGenerationThree)
			}
		})
	}
}
