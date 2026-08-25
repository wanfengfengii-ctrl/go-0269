package app

import (
	"context"

	"silkworm-egg-cold-storage-gate/internal/arbiter"
	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/store"
	"silkworm-egg-cold-storage-gate/internal/task"
)

// ReviewRequest records one independent reviewer's conclusion.
type ReviewRequest struct {
	Generation int64
	PersonID   string
	Conclusion string
}

// SubmitReview records a qualified, non-overlapping reviewer. Two distinct
// qualified reviewers advance the task to ready-to-admit.
func (s *Service) SubmitReview(ctx context.Context, opID, taskID string, req ReviewRequest) (int, any, error) {
	return s.withOperation(ctx, opID, req, func(tx store.Tx) (int, any, error) {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if t.State != domain.StatePendingIndependentReview {
			return 0, nil, domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{
				Code:    "NOT_PENDING_REVIEW",
				Message: "task is not awaiting independent review",
			})
		}
		if err := requireGeneration(t, req.Generation); err != nil {
			return 0, nil, err
		}
		rule, _ := s.rule(t)
		if !task.Qualified(rule, task.RoleReview, req.PersonID) {
			return 0, nil, domain.NewError(domain.ErrInvalidInput, t.State, domain.Reason{
				Code:    "UNQUALIFIED_REVIEWER",
				Message: "person is not qualified for review",
			})
		}
		confirmations, err := tx.ListConfirmations(ctx, taskID)
		if err != nil {
			return 0, nil, err
		}
		if task.ConfirmersSet(confirmations)[req.PersonID] {
			return 0, nil, domain.NewError(domain.ErrInvalidInput, t.State, domain.Reason{
				Code:    "ROLE_OVERLAP",
				Message: "reviewer overlaps a production role",
			})
		}
		review := domain.Review{
			TaskID:     taskID,
			PersonID:   req.PersonID,
			Generation: req.Generation,
			Digest:     contentHash(req),
			Conclusion: req.Conclusion,
			AtLogical:  s.now(),
		}
		if err := tx.SaveReview(ctx, review); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		reviews, err := tx.ListReviews(ctx, taskID)
		if err != nil {
			return 0, nil, err
		}
		if distinctReviewers(reviews) < 2 {
			return 200, taskView(t), nil
		}
		if err := task.ValidateIndependentReviews(rule, reviews, task.ConfirmersSet(confirmations)); err != nil {
			return 0, nil, err
		}
		if err := task.Advance(t, domain.StateReadyToAdmit); err != nil {
			return 0, nil, err
		}
		t.UpdatedAt = s.now()
		if err := tx.UpdateTask(ctx, t); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		return 200, taskView(t), nil
	})
}

// DecideRequest competes a terminal decision.
type DecideRequest struct {
	Generation int64
	DecidedBy  string
}

// Decide competes the single terminal decision through the task-level unique
// barrier. Admit requires concurring reviewers and a cold-storage cell lease;
// the loser of a concurrent race reads the existing decision and either
// replays it (same type) or returns FINAL_DECISION_CONFLICT (different type).
func (s *Service) Decide(ctx context.Context, opID, taskID string, typ domain.FinalDecisionType, req DecideRequest) (int, any, error) {
	return s.withOperation(ctx, opID, req, func(tx store.Tx) (int, any, error) {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if t.State.Terminal() {
			existing, e := tx.GetFinalDecision(ctx, taskID)
			if e != nil {
				return 0, nil, e
			}
			if existing != nil && existing.Type == typ {
				return 200, decisionView(*existing), nil
			}
			return 0, nil, domain.NewError(domain.ErrFinalDecisionConflict, t.State, domain.Reason{
				Code:    "FINAL_DECISION_CONFLICT",
				Message: "a terminal decision already exists",
			})
		}
		if t.State != domain.StateReadyToAdmit {
			return 0, nil, domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{
				Code:    "NOT_READY",
				Message: "task is not ready for a terminal decision",
			})
		}
		if err := requireGeneration(t, req.Generation); err != nil {
			return 0, nil, err
		}
		if !s.evidenceClosed(ctx, tx, taskID, t.Generation) {
			return 0, nil, domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{
				Code:    "EVIDENCE_NOT_CLOSED",
				Message: "evidence is not fully closed",
			})
		}
		reviews, err := tx.ListReviews(ctx, taskID)
		if err != nil {
			return 0, nil, err
		}
		rule, _ := s.rule(t)
		if err := task.ValidateIndependentReviews(rule, reviews, nil); err != nil {
			return 0, nil, err
		}
		if typ == domain.FinalAdmit {
			if !arbiter.Concurring(reviews) {
				return 0, nil, domain.NewError(domain.ErrInvalidInput, t.State, domain.Reason{
					Code:    "REVIEWS_NOT_CONCURRING",
					Message: "admit requires concurring PASS reviews",
				})
			}
			if !s.hasColdStorageCell(ctx, tx, taskID) {
				return 0, nil, domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{
					Code:    "NO_COLD_STORAGE_CELL",
					Message: "admit requires a cold-storage cell lease",
				})
			}
		}
		d := arbiter.NewDecision(taskID, typ, req.DecidedBy, s.now())
		if err := tx.SaveFinalDecision(ctx, d); err != nil {
			if err == store.ErrDuplicate {
				existing, e2 := tx.GetFinalDecision(ctx, taskID)
				if e2 != nil {
					return 0, nil, e2
				}
				if existing.Type == typ {
					return 200, decisionView(*existing), nil
				}
				return 0, nil, domain.NewError(domain.ErrFinalDecisionConflict, t.State, domain.Reason{
					Code:    "FINAL_DECISION_CONFLICT",
					Message: "a different terminal decision already exists",
				})
			}
			return 0, nil, mapStoreErr(err)
		}
		var terminal domain.TaskState
		switch typ {
		case domain.FinalAdmit:
			terminal = domain.StateAdmitted
		case domain.FinalIsolate:
			terminal = domain.StateIsolated
		default:
			terminal = domain.StateCancelled
		}
		if err := task.Advance(t, terminal); err != nil {
			return 0, nil, err
		}
		t.UpdatedAt = s.now()
		if err := tx.UpdateTask(ctx, t); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		return 200, decisionView(d), nil
	})
}

// evidenceClosed reports whether all four evidence types have a verified
// version in the given generation.
func (s *Service) evidenceClosed(ctx context.Context, tx store.Tx, taskID string, generation int64) bool {
	evidence, err := tx.ListEvidence(ctx, taskID)
	if err != nil {
		return false
	}
	verified := verifiedTypes(evidence, generation)
	return verified[domain.EvidenceMicroscopy] && verified[domain.EvidenceEnvironment] &&
		verified[domain.EvidenceMoisture] && verified[domain.EvidenceDamage]
}

// hasColdStorageCell reports whether the task holds an active cold-storage
// cell lease.
func (s *Service) hasColdStorageCell(ctx context.Context, tx store.Tx, taskID string) bool {
	leases, err := tx.ListLeases(ctx, taskID)
	if err != nil {
		return false
	}
	for _, l := range leases {
		if l.ResourceType == domain.ResourceColdStorageCell && l.State == domain.LeaseActive {
			return true
		}
	}
	return false
}

func decisionView(d domain.FinalDecision) map[string]any {
	return map[string]any{
		"taskId":          d.TaskID,
		"type":            d.Type.String(),
		"decisionVersion": d.DecisionVer,
		"admitCredential": d.AdmitCredential,
		"decidedBy":       d.DecidedBy,
	}
}
