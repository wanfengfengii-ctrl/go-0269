package task

import (
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

func TestLinearAdvance(t *testing.T) {
	tk := &domain.StorageTask{ID: "t1", State: domain.StatePendingLock, Version: 1}
	want := []domain.TaskState{
		domain.StatePendingProductionConfirmation,
		domain.StateSealingSamples,
		domain.StateOccupyingCabins,
		domain.StateObservingHatching,
		domain.StateVerifyingMicroscopy,
		domain.StateRetestingPhysicochemical,
		domain.StatePendingIndependentReview,
		domain.StateReadyToAdmit,
	}
	for _, target := range want {
		if err := Advance(tk, target); err != nil {
			t.Fatalf("Advance to %v: %v", target, err)
		}
	}
	if tk.State != domain.StateReadyToAdmit {
		t.Fatalf("final state = %v, want ready-to-admit", tk.State)
	}
}

func TestTerminalTransitions(t *testing.T) {
	for _, term := range []domain.TaskState{domain.StateAdmitted, domain.StateIsolated, domain.StateCancelled} {
		tk := &domain.StorageTask{ID: "t1", State: domain.StateReadyToAdmit, Version: 1}
		if err := Advance(tk, term); err != nil {
			t.Fatalf("Advance to terminal %v: %v", term, err)
		}
		if !tk.State.Terminal() {
			t.Fatalf("state %v not terminal", tk.State)
		}
	}
}

func TestRejectIllegalTransition(t *testing.T) {
	tk := &domain.StorageTask{ID: "t1", State: domain.StatePendingLock, Version: 1}
	// Skipping straight to a mid state is illegal.
	if err := Advance(tk, domain.StateObservingHatching); err == nil {
		t.Fatal("expected illegal transition error")
	}
	// Terminal writes after terminal are rejected.
	tk.State = domain.StateAdmitted
	if err := Advance(tk, domain.StateCancelled); err == nil {
		t.Fatal("expected rejection of late write to terminal state")
	}
}

func TestCheckGeneration(t *testing.T) {
	if err := CheckGeneration(2, 2); err != nil {
		t.Fatalf("matching generation rejected: %v", err)
	}
	if err := CheckGeneration(2, 3); err == nil {
		t.Fatal("stale generation not rejected")
	}
}

func TestCheckOperation(t *testing.T) {
	st, err := CheckOperation(nil, "op-1", "hash-1")
	if err != nil || st != domain.OpNew {
		t.Fatalf("new op = %v, %v", st, err)
	}
	existing := &domain.OperationRecord{OperationID: "op-1", ContentHash: "hash-1"}
	st, err = CheckOperation(existing, "op-1", "hash-1")
	if err != nil || st != domain.OpIdempotent {
		t.Fatalf("idempotent op = %v, %v", st, err)
	}
	st, err = CheckOperation(existing, "op-1", "hash-2")
	if err == nil || st != domain.OpConflict {
		t.Fatalf("conflict op = %v, %v", st, err)
	}
}
