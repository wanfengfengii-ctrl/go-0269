package app

import (
	"context"
	"sync"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

// driveToReadyToAdmit advances a task all the way to READY_TO_ADMIT with two
// concurring reviews and a cold-storage cell lease in place.
func driveToReadyToAdmit(t *testing.T, svc *Service) string {
	t.Helper()
	ctx := context.Background()
	id := createAndLock(t, svc)
	driveToObservation(t, svc, id)

	cells := []domain.ObservationCell{
		{CardSeal: "seal-1", DayAge: 1, Hatched: 90, Unhatched: 6, Dead: 3, Damaged: 1},
		{CardSeal: "seal-1", DayAge: 2, Hatched: 90, Unhatched: 6, Dead: 3, Damaged: 1},
		{CardSeal: "seal-2", DayAge: 1, Hatched: 90, Unhatched: 6, Dead: 3, Damaged: 1},
		{CardSeal: "seal-2", DayAge: 2, Hatched: 90, Unhatched: 6, Dead: 3, Damaged: 1},
	}
	if _, _, err := svc.SubmitObservations(ctx, "op-obs", id, ObservationRequest{Generation: 1, Cells: cells}); err != nil {
		t.Fatalf("observations: %v", err)
	}
	evidence := []VerifyEvidenceRequest{
		{Generation: 1, Type: domain.EvidenceMicroscopy, CardSeal: "seal-1", DayAge: 1, SlideNo: "slide-1", Reading: "0.00", CallID: "c1"},
		{Generation: 1, Type: domain.EvidenceEnvironment, CardSeal: "seal-1", DayAge: 1, SlideNo: "slide-1", Reading: "25.00", CallID: "c2"},
		{Generation: 1, Type: domain.EvidenceMoisture, CardSeal: "seal-1", DayAge: 1, SlideNo: "slide-1", Reading: "10.00", CallID: "c3"},
		{Generation: 1, Type: domain.EvidenceDamage, CardSeal: "seal-1", DayAge: 1, SlideNo: "slide-1", Reading: "0.00", CallID: "c4"},
	}
	for i, ev := range evidence {
		if _, _, err := svc.VerifyEvidence(ctx, "op-ev-"+string(rune('a'+i)), id, ev); err != nil {
			t.Fatalf("verify evidence %d: %v", i, err)
		}
	}
	for _, p := range []string{"person-c", "person-d"} {
		if _, _, err := svc.SubmitReview(ctx, "op-review-"+p, id, ReviewRequest{Generation: 1, PersonID: p, Conclusion: "PASS"}); err != nil {
			t.Fatalf("review %s: %v", p, err)
		}
	}
	if _, _, err := svc.AcquireLease(ctx, "op-cell", id, LeaseRequest{
		Generation: 1, ResourceType: domain.ResourceColdStorageCell, ResourceNo: "cell-1", ExpiresAt: 10000,
	}); err != nil {
		t.Fatalf("acquire cell: %v", err)
	}
	return id
}

func TestAdmitGeneratesSingleCredential(t *testing.T) {
	svc := newTestService(t, nil, nil)
	id := driveToReadyToAdmit(t, svc)
	ctx := context.Background()

	_, body, err := svc.Decide(ctx, "op-decide", id, domain.FinalAdmit, DecideRequest{Generation: 1, DecidedBy: "person-c"})
	if err != nil {
		t.Fatalf("decide admit: %v", err)
	}
	m := body.(map[string]any)
	if m["admitCredential"] == "" {
		t.Fatal("admit credential is empty")
	}
	if m["type"] != "ADMIT" {
		t.Fatalf("decision type = %v, want ADMIT", m["type"])
	}
}

func TestThreeWayFinalRaceSingleWinner(t *testing.T) {
	svc := newTestService(t, nil, nil)
	id := driveToReadyToAdmit(t, svc)
	ctx := context.Background()

	start := make(chan struct{})
	types := []domain.FinalDecisionType{domain.FinalAdmit, domain.FinalIsolate, domain.FinalCancel}
	results := make([]error, 3)
	var wg sync.WaitGroup
	for i, typ := range types {
		wg.Add(1)
		go func(i int, typ domain.FinalDecisionType) {
			defer wg.Done()
			<-start
			_, _, err := svc.Decide(ctx, "op-decide-"+typ.String(), id, typ, DecideRequest{Generation: 1, DecidedBy: "person-c"})
			results[i] = err
		}(i, typ)
	}
	close(start)
	wg.Wait()

	winners, conflicts := 0, 0
	for _, err := range results {
		if err == nil {
			winners++
		} else if de, ok := err.(*domain.DomainError); ok && de.Code == domain.ErrFinalDecisionConflict {
			conflicts++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if winners != 1 || conflicts != 2 {
		t.Fatalf("winners=%d conflicts=%d, want 1 winner and 2 conflicts", winners, conflicts)
	}
}

func TestTerminalStateRejectsLateWrites(t *testing.T) {
	svc := newTestService(t, nil, nil)
	id := driveToReadyToAdmit(t, svc)
	ctx := context.Background()
	if _, _, err := svc.Decide(ctx, "op-decide", id, domain.FinalAdmit, DecideRequest{Generation: 1, DecidedBy: "person-c"}); err != nil {
		t.Fatalf("decide: %v", err)
	}
	// Any late business write after the terminal decision is rejected and the
	// database state is unchanged.
	_, _, err := svc.SubmitObservations(ctx, "op-late-obs", id, ObservationRequest{Generation: 1, Cells: []domain.ObservationCell{
		{CardSeal: "seal-1", DayAge: 1, Hatched: 90, Unhatched: 6, Dead: 3, Damaged: 1},
	}})
	if err == nil {
		t.Fatal("expected late observation to be rejected")
	}
	de, ok := err.(*domain.DomainError)
	if !ok || de.Code != domain.ErrInvalidStateTransition {
		t.Fatalf("late write error = %v, want INVALID_STATE_TRANSITION", err)
	}
}
