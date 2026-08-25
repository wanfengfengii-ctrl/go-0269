package app

import (
	"context"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/store"
	"silkworm-egg-cold-storage-gate/internal/task"
)

// ConfirmProductionRequest records one producer's confirmation.
type ConfirmProductionRequest struct {
	Generation int64
	PersonID   string
	Conclusion string
}

// ConfirmProduction records a qualified producer's confirmation. Two distinct
// qualified producers advance the task to sample sealing.
func (s *Service) ConfirmProduction(ctx context.Context, opID, taskID string, req ConfirmProductionRequest) (int, any, error) {
	return s.withOperation(ctx, opID, req, func(tx store.Tx) (int, any, error) {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if t.State != domain.StatePendingProductionConfirmation {
			return 0, nil, domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{
				Code:    "NOT_PENDING_CONFIRMATION",
				Message: "task is not awaiting production confirmation",
			})
		}
		if err := requireGeneration(t, req.Generation); err != nil {
			return 0, nil, err
		}
		rule, _ := s.rule(t)
		if !task.Qualified(rule, task.RoleProduction, req.PersonID) {
			return 0, nil, domain.NewError(domain.ErrInvalidInput, t.State, domain.Reason{
				Code:    "UNQUALIFIED_PRODUCER",
				Message: "person is not qualified for production",
			})
		}
		conf := domain.Confirmation{
			TaskID:     taskID,
			Role:       task.RoleProduction,
			PersonID:   req.PersonID,
			Generation: req.Generation,
			Digest:     contentHash(req),
			Conclusion: req.Conclusion,
			AtLogical:  s.now(),
		}
		if err := tx.SaveConfirmation(ctx, conf); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		confirmations, err := tx.ListConfirmations(ctx, taskID)
		if err != nil {
			return 0, nil, err
		}
		if distinctPersons(confirmations) < 2 {
			return 200, taskView(t), nil
		}
		if err := task.ValidateProductionConfirmations(rule, confirmations); err != nil {
			return 0, nil, err
		}
		if err := task.Advance(t, domain.StateSealingSamples); err != nil {
			return 0, nil, err
		}
		t.UpdatedAt = s.now()
		if err := tx.UpdateTask(ctx, t); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		return 200, taskView(t), nil
	})
}

// SealSample is one card's triple-sample sealing entry.
type SealSample struct {
	CardSeal   string
	SampleNo   int
	Triplet    int
	SealDigest string
	BlindCode  string
}

// SealSamplesRequest carries the full triple-sample sealing manifest.
type SealSamplesRequest struct {
	Generation int64
	Samples    []SealSample
}

// SealSamples atomically registers the triple samples, seal digests, and blind
// codes for every locked card, then advances the task to cabin occupation.
func (s *Service) SealSamples(ctx context.Context, opID, taskID string, req SealSamplesRequest) (int, any, error) {
	return s.withOperation(ctx, opID, req, func(tx store.Tx) (int, any, error) {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if t.State != domain.StateSealingSamples {
			return 0, nil, domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{
				Code:    "NOT_SEALING",
				Message: "task is not awaiting sample sealing",
			})
		}
		if err := requireGeneration(t, req.Generation); err != nil {
			return 0, nil, err
		}
		if t.Snapshot == nil {
			return 0, nil, domain.NewError(domain.ErrInternal, t.State, domain.Reason{Code: "MISSING_SNAPSHOT"})
		}
		if len(req.Samples) != len(t.Snapshot.CardSeals) {
			return 0, nil, domain.NewError(domain.ErrInvalidInput, t.State, domain.Reason{
				Code:    "SAMPLE_COUNT_MISMATCH",
				Message: "sample count does not match locked cards",
			})
		}
		cardSet := make(map[string]bool, len(t.Snapshot.CardSeals))
		for _, c := range t.Snapshot.CardSeals {
			cardSet[c] = true
		}
		blindSet := make(map[string]bool)
		seals := make([]domain.SampleSeal, 0, len(req.Samples))
		for _, sm := range req.Samples {
			if !cardSet[sm.CardSeal] {
				return 0, nil, domain.NewError(domain.ErrInvalidInput, t.State, domain.Reason{
					Code:     "UNKNOWN_CARD_SEAL",
					CardSeal: sm.CardSeal,
					Message:  "card seal not in locked snapshot",
				})
			}
			if blindSet[sm.BlindCode] {
				return 0, nil, domain.NewError(domain.ErrDuplicateResource, t.State, domain.Reason{
					Code:    "DUPLICATE_BLIND_CODE",
					Message: "blind code duplicated across cards",
				})
			}
			blindSet[sm.BlindCode] = true
			seals = append(seals, domain.SampleSeal{
				TaskID:     taskID,
				CardSeal:   sm.CardSeal,
				SampleNo:   sm.SampleNo,
				Triplet:    sm.Triplet,
				SealDigest: sm.SealDigest,
				BlindCode:  sm.BlindCode,
			})
		}
		if err := tx.SaveSampleSeals(ctx, seals); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if err := task.Advance(t, domain.StateOccupyingCabins); err != nil {
			return 0, nil, err
		}
		t.UpdatedAt = s.now()
		if err := tx.UpdateTask(ctx, t); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		return 200, taskView(t), nil
	})
}

// LeaseRequest is the input to lease acquire/move/renew commands.
type LeaseRequest struct {
	Generation   int64
	ResourceType domain.ResourceType
	ResourceNo   string
	ToResourceNo string // move target
	ExpiresAt    int64
}

// AcquireLease acquires a unique resource lease. A cabin plus slide pair
// advances the task into hatch observation.
func (s *Service) AcquireLease(ctx context.Context, opID, taskID string, req LeaseRequest) (int, any, error) {
	return s.withOperation(ctx, opID, req, func(tx store.Tx) (int, any, error) {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if t.State.Terminal() {
			return 0, nil, domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{Code: "TERMINAL"})
		}
		if err := requireGeneration(t, req.Generation); err != nil {
			return 0, nil, err
		}
		if req.ResourceNo == "" {
			return 0, nil, domain.NewError(domain.ErrInvalidInput, t.State, domain.Reason{Code: "MISSING_RESOURCE_NO"})
		}
		existing, err := tx.ActiveLease(ctx, req.ResourceType, req.ResourceNo)
		if err != nil {
			return 0, nil, err
		}
		if existing != nil && existing.TaskID != taskID {
			return 0, nil, domain.NewError(domain.ErrDuplicateResource, t.State, domain.Reason{
				Code:    "DUPLICATE_RESOURCE",
				Message: "resource already leased by another task",
			})
		}
		expires := req.ExpiresAt
		if expires == 0 {
			expires = s.now() + defaultLeaseTTL
		}
		lease := domain.ResourceLease{
			TaskID:       taskID,
			ResourceType: req.ResourceType,
			ResourceNo:   req.ResourceNo,
			Generation:   req.Generation,
			State:        domain.LeaseActive,
			AcquiredAt:   s.now(),
			ExpiresAt:    expires,
		}
		if err := tx.AcquireLease(ctx, lease); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if t.State == domain.StateOccupyingCabins && hasObservationLeases(tx, ctx, taskID) {
			if err := task.Advance(t, domain.StateObservingHatching); err != nil {
				return 0, nil, err
			}
			t.UpdatedAt = s.now()
			if err := tx.UpdateTask(ctx, t); err != nil {
				return 0, nil, mapStoreErr(err)
			}
		}
		return 200, leaseView(lease), nil
	})
}

// MoveLease atomically releases one resource and acquires another, so a cabin
// change never leaves a partial state.
func (s *Service) MoveLease(ctx context.Context, opID, taskID string, req LeaseRequest) (int, any, error) {
	return s.withOperation(ctx, opID, req, func(tx store.Tx) (int, any, error) {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if t.State.Terminal() {
			return 0, nil, domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{Code: "TERMINAL"})
		}
		if err := requireGeneration(t, req.Generation); err != nil {
			return 0, nil, err
		}
		if req.ToResourceNo == "" || req.ResourceNo == req.ToResourceNo {
			return 0, nil, domain.NewError(domain.ErrInvalidInput, t.State, domain.Reason{Code: "INVALID_MOVE"})
		}
		existing, err := tx.ActiveLease(ctx, req.ResourceType, req.ToResourceNo)
		if err != nil {
			return 0, nil, err
		}
		if existing != nil && existing.TaskID != taskID {
			return 0, nil, domain.NewError(domain.ErrDuplicateResource, t.State, domain.Reason{Code: "DUPLICATE_RESOURCE"})
		}
		if err := tx.ReleaseLease(ctx, taskID, req.ResourceType, req.ResourceNo, "moved"); err != nil {
			return 0, nil, err
		}
		lease := domain.ResourceLease{
			TaskID:       taskID,
			ResourceType: req.ResourceType,
			ResourceNo:   req.ToResourceNo,
			Generation:   req.Generation,
			State:        domain.LeaseActive,
			AcquiredAt:   s.now(),
			ExpiresAt:    req.ExpiresAt,
		}
		if lease.ExpiresAt == 0 {
			lease.ExpiresAt = s.now() + defaultLeaseTTL
		}
		if err := tx.AcquireLease(ctx, lease); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		return 200, leaseView(lease), nil
	})
}

// RenewLease extends a lease's expiry.
func (s *Service) RenewLease(ctx context.Context, opID, taskID string, req LeaseRequest) (int, any, error) {
	return s.withOperation(ctx, opID, req, func(tx store.Tx) (int, any, error) {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if err := requireGeneration(t, req.Generation); err != nil {
			return 0, nil, err
		}
		if err := tx.RenewLease(ctx, taskID, req.ResourceType, req.ResourceNo, req.ExpiresAt); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		return 200, map[string]any{
			"taskId":       taskID,
			"resourceType": req.ResourceType.String(),
			"resourceNo":   req.ResourceNo,
			"expiresAt":    req.ExpiresAt,
		}, nil
	})
}

const defaultLeaseTTL = 86400

// hasObservationLeases reports whether a task holds at least one cabin lease
// and one slide lease, the pair required to enter hatch observation.
func hasObservationLeases(tx store.Tx, ctx context.Context, taskID string) bool {
	leases, err := tx.ListLeases(ctx, taskID)
	if err != nil {
		return false
	}
	var cabin, slide bool
	for _, l := range leases {
		if l.State != domain.LeaseActive {
			continue
		}
		switch l.ResourceType {
		case domain.ResourceIncubationCabin:
			cabin = true
		case domain.ResourceSlide:
			slide = true
		}
	}
	return cabin && slide
}

func leaseView(l domain.ResourceLease) map[string]any {
	return map[string]any{
		"taskId":       l.TaskID,
		"resourceType": l.ResourceType.String(),
		"resourceNo":   l.ResourceNo,
		"state":        l.State.String(),
		"expiresAt":    l.ExpiresAt,
	}
}

func distinctPersons(confirmations []domain.Confirmation) int {
	seen := make(map[string]bool)
	for _, c := range confirmations {
		seen[c.PersonID] = true
	}
	return len(seen)
}

func distinctReviewers(reviews []domain.Review) int {
	seen := make(map[string]bool)
	for _, r := range reviews {
		seen[r.PersonID] = true
	}
	return len(seen)
}
