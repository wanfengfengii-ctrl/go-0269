package app

import (
	"context"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/ledger"
)

// driveToRecheck sets up a task whose coverage and microscopy evidence trigger
// a recheck: low hatch rate, high dead rate, and a suspected pebrine spore.
func driveToRecheck(t *testing.T, svc *Service) string {
	t.Helper()
	ctx := context.Background()
	id := createAndLock(t, svc)
	driveToObservation(t, svc, id)
	cells := []domain.ObservationCell{
		{CardSeal: "seal-1", DayAge: 1, Hatched: 70, Unhatched: 9, Dead: 20, Damaged: 1},
		{CardSeal: "seal-1", DayAge: 2, Hatched: 70, Unhatched: 9, Dead: 20, Damaged: 1},
		{CardSeal: "seal-2", DayAge: 1, Hatched: 70, Unhatched: 9, Dead: 20, Damaged: 1},
		{CardSeal: "seal-2", DayAge: 2, Hatched: 70, Unhatched: 9, Dead: 20, Damaged: 1},
	}
	if _, _, err := svc.SubmitObservations(ctx, "op-obs", id, ObservationRequest{Generation: 1, Cells: cells}); err != nil {
		t.Fatalf("submit observations: %v", err)
	}
	if _, _, err := svc.VerifyEvidence(ctx, "op-ev", id, VerifyEvidenceRequest{
		Generation: 1, Type: domain.EvidenceMicroscopy, CardSeal: "seal-1", DayAge: 1, SlideNo: "slide-1", Reading: "1.00", CallID: "c1",
	}); err != nil {
		t.Fatalf("verify microscopy: %v", err)
	}
	return id
}

func TestRecheckMergesTriggers(t *testing.T) {
	svc := newTestService(t, nil, nil)
	id := driveToRecheck(t, svc)
	ctx := context.Background()

	_, body, err := svc.CreateRecheck(ctx, "op-recheck", id, RecheckRequest{Generation: 1})
	if err != nil {
		t.Fatalf("create recheck: %v", err)
	}
	m := body.(map[string]any)
	if m["recheck"] != true {
		t.Fatalf("expected a recheck, got %v", m["recheck"])
	}
	gen := m["generation"].(int64)
	if gen != 2 {
		t.Fatalf("recheck generation = %v, want 2", gen)
	}
	triggers := m["triggers"].([]string)
	if len(triggers) != 3 {
		t.Fatalf("triggers = %v, want 3 merged triggers", triggers)
	}

	// The recheck case covers all affected cards, blinds, days, and slides.
	_, detail, err := svc.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	rc := detail.(map[string]any)["recheck"].(map[string]any)
	if len(rc["affectedCards"].([]string)) != 2 {
		t.Fatalf("affected cards = %v, want 2", rc["affectedCards"])
	}
	if len(rc["affectedDayAges"].([]int)) != 2 {
		t.Fatalf("affected day ages = %v, want 2", rc["affectedDayAges"])
	}
	if detail.(map[string]any)["generation"].(int64) != 2 {
		t.Fatalf("task generation = %v, want 2", detail.(map[string]any)["generation"])
	}
}

func TestOldGenerationEvidenceExcluded(t *testing.T) {
	// A pure ledger check: only current-generation verified evidence
	// participates in the decision chain; late older-generation results are
	// retained for audit but excluded from the current chain.
	chain := []domain.EvidenceVersion{
		{Type: domain.EvidenceMicroscopy, Generation: 1, VersionNo: 1, Verified: true},
		{Type: domain.EvidenceMicroscopy, Generation: 2, VersionNo: 1, Verified: true},
		{Type: domain.EvidenceMicroscopy, Generation: 2, VersionNo: 2, Verified: false},
	}
	current := ledger.InGeneration(chain, 2)
	if len(current) != 2 {
		t.Fatalf("current generation evidence = %d, want 2", len(current))
	}
	for _, e := range current {
		if e.Generation != 2 {
			t.Fatalf("filtered evidence generation = %d, want 2", e.Generation)
		}
	}
}
