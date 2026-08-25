// Package task implements the storage-task aggregate: the twelve business
// states, the strict forward state machine, generation matching, and the
// operation-id idempotency record. The single-write final barrier and the
// concurrent-version mapping belong to the store and arbiter layers, which
// consume the pure rules defined here.
package task

import (
	"silkworm-egg-cold-storage-gate/internal/domain"
)

// linearOrder is the strict forward sequence a task must follow before it may
// reach a terminal state.
var linearOrder = []domain.TaskState{
	domain.StatePendingLock,
	domain.StatePendingProductionConfirmation,
	domain.StateSealingSamples,
	domain.StateOccupyingCabins,
	domain.StateObservingHatching,
	domain.StateVerifyingMicroscopy,
	domain.StateRetestingPhysicochemical,
	domain.StatePendingIndependentReview,
	domain.StateReadyToAdmit,
}

// Next returns the single forward successor of a state, or ok=false when the
// state is terminal or otherwise has no linear successor.
func Next(s domain.TaskState) (domain.TaskState, bool) {
	for i, st := range linearOrder {
		if st == s && i+1 < len(linearOrder) {
			return linearOrder[i+1], true
		}
	}
	return 0, false
}

// CanAdvance reports whether a transition from current to target is legal:
// either the immediate linear successor, or a terminal state reached from
// ready-to-admit.
func CanAdvance(current, target domain.TaskState) bool {
	if current == target {
		return false
	}
	if current.Terminal() {
		return false
	}
	if next, ok := Next(current); ok && next == target {
		return true
	}
	if current == domain.StateReadyToAdmit {
		switch target {
		case domain.StateAdmitted, domain.StateIsolated, domain.StateCancelled:
			return true
		}
	}
	return false
}

// Advance transitions a task to target, returning a domain error with stable
// code INVALID_STATE_TRANSITION if the move is illegal.
func Advance(t *domain.StorageTask, target domain.TaskState) error {
	if !CanAdvance(t.State, target) {
		return domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{
			Code:    "INVALID_STATE_TRANSITION",
			Message: "cannot transition to " + target.String(),
		})
	}
	t.State = target
	return nil
}

// CheckGeneration rejects any generation-sensitive command whose generation
// does not match the task's current generation.
func CheckGeneration(current, expected int64) error {
	if current != expected {
		return domain.NewError(domain.ErrStaleGeneration, domain.StatePendingLock, domain.Reason{
			Code:    "STALE_GENERATION",
			Message: "command generation does not match current task generation",
		})
	}
	return nil
}

// CheckOperation classifies an incoming operation against an existing
// idempotency record: a matching operation ID and content hash is idempotent,
// a mismatched hash is a conflict, and an absent record is new.
func CheckOperation(existing *domain.OperationRecord, opID, contentHash string) (domain.OpStatus, error) {
	if existing == nil {
		return domain.OpNew, nil
	}
	if existing.OperationID != opID {
		return domain.OpNew, nil
	}
	if existing.ContentHash == contentHash {
		return domain.OpIdempotent, nil
	}
	return domain.OpConflict, domain.NewError(domain.ErrOperationConflict, domain.StatePendingLock, domain.Reason{
		Code:    "OPERATION_CONFLICT",
		Message: "same operation ID with different content hash",
	})
}
