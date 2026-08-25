package app

import (
	"context"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/fixedpoint"
	"silkworm-egg-cold-storage-gate/internal/ledger"
)

func TestFullCoverageAdvancesToMicroscopy(t *testing.T) {
	svc := newTestService(t, nil, nil)
	id := createAndLock(t, svc)
	driveToObservation(t, svc, id)
	ctx := context.Background()

	cells := []domain.ObservationCell{
		{CardSeal: "seal-1", DayAge: 1, Hatched: 80, Unhatched: 15, Dead: 4, Damaged: 1},
		{CardSeal: "seal-1", DayAge: 2, Hatched: 82, Unhatched: 13, Dead: 4, Damaged: 1},
		{CardSeal: "seal-2", DayAge: 1, Hatched: 81, Unhatched: 14, Dead: 4, Damaged: 1},
		{CardSeal: "seal-2", DayAge: 2, Hatched: 83, Unhatched: 12, Dead: 4, Damaged: 1},
	}
	if _, _, err := svc.SubmitObservations(ctx, "op-obs", id, ObservationRequest{Generation: 1, Cells: cells}); err != nil {
		t.Fatalf("submit observations: %v", err)
	}
	_, detail, err := svc.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if detail.(map[string]any)["state"] != "VERIFYING_MICROSCOPY" {
		t.Fatalf("state after full coverage = %v", detail.(map[string]any)["state"])
	}
}

func TestConservationViolationRejected(t *testing.T) {
	svc := newTestService(t, nil, nil)
	id := createAndLock(t, svc)
	driveToObservation(t, svc, id)
	ctx := context.Background()
	_, _, err := svc.SubmitObservations(ctx, "op-obs", id, ObservationRequest{Generation: 1, Cells: []domain.ObservationCell{
		{CardSeal: "seal-1", DayAge: 1, Hatched: 50, Unhatched: 20, Dead: 4, Damaged: 1}, // sums to 75 != 100
	}})
	if err == nil {
		t.Fatal("expected conservation violation")
	}
	de, ok := err.(*domain.DomainError)
	if !ok || de.Code != domain.ErrInvalidInput {
		t.Fatalf("error = %v, want INVALID_INPUT", err)
	}
}

func TestDuplicateCoverageRejectedWithoutRecheck(t *testing.T) {
	svc := newTestService(t, nil, nil)
	id := createAndLock(t, svc)
	driveToObservation(t, svc, id)
	ctx := context.Background()
	cell := domain.ObservationCell{CardSeal: "seal-1", DayAge: 1, Hatched: 80, Unhatched: 15, Dead: 4, Damaged: 1}
	if _, _, err := svc.SubmitObservations(ctx, "op-obs-1", id, ObservationRequest{Generation: 1, Cells: []domain.ObservationCell{cell}}); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	_, _, err := svc.SubmitObservations(ctx, "op-obs-2", id, ObservationRequest{Generation: 1, Cells: []domain.ObservationCell{cell}})
	if err == nil {
		t.Fatal("expected duplicate coverage error")
	}
	de, ok := err.(*domain.DomainError)
	if !ok || de.Code != domain.ErrDuplicateResource {
		t.Fatalf("error = %v, want DUPLICATE_RESOURCE", err)
	}
}

func TestCoverageGapsReported(t *testing.T) {
	svc := newTestService(t, nil, nil)
	id := createAndLock(t, svc)
	driveToObservation(t, svc, id)
	ctx := context.Background()
	_, _, err := svc.SubmitObservations(ctx, "op-obs", id, ObservationRequest{Generation: 1, Cells: []domain.ObservationCell{
		{CardSeal: "seal-1", DayAge: 1, Hatched: 80, Unhatched: 15, Dead: 4, Damaged: 1},
	}})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	_, body, err := svc.GetCoverage(ctx, id)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	m := body.(map[string]any)
	if m["complete"] != false {
		t.Fatalf("coverage should be incomplete, got %v", m["complete"])
	}
	gaps := m["gaps"].([]map[string]any)
	if len(gaps) != 3 {
		t.Fatalf("gaps = %d, want 3", len(gaps))
	}
}

func TestHatchRateThresholdBoundary(t *testing.T) {
	cov := ledger.Coverage{Cells: []domain.ObservationCell{
		{CardSeal: "s1", DayAge: 1, Hatched: 80, Unhatched: 15, Dead: 4, Damaged: 1}, // 80% hatch
	}}
	rate, err := cov.HatchRate(2)
	if err != nil {
		t.Fatalf("hatch rate: %v", err)
	}
	threshold, _ := fixedpoint.Parse("80.00", 2)
	if fixedpoint.Cmp(rate, threshold) != 0 {
		t.Fatalf("hatch rate %s != threshold %s", rate.String(), threshold.String())
	}
	dead, _ := cov.DeadEggRate(2)
	maxDead, _ := fixedpoint.Parse("5.00", 2)
	if fixedpoint.Cmp(dead, maxDead) >= 0 {
		t.Fatalf("dead rate %s should be below %s", dead.String(), maxDead.String())
	}
}
