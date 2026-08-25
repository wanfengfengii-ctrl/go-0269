package app

import (
	"context"

	"silkworm-egg-cold-storage-gate/internal/catalog"
	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/store"
	"silkworm-egg-cold-storage-gate/internal/task"
)

// CreateTaskRequest is the input to task creation.
type CreateTaskRequest struct {
	TaskID        string
	LineageCode   string
	BatchCode     string
	MothBagDigest string
}

// CreateTask inserts a pending-lock task. It is idempotent by operation ID and
// rejects duplicate task IDs or unknown lineages.
func (s *Service) CreateTask(ctx context.Context, opID string, req CreateTaskRequest) (int, any, error) {
	if req.TaskID == "" || req.LineageCode == "" {
		return 0, nil, domain.NewError(domain.ErrInvalidInput, domain.StatePendingLock, domain.Reason{
			Code:    "MISSING_FIELDS",
			Message: "taskId and lineageCode are required",
		})
	}
	if !s.dir.HasLineage(req.LineageCode) {
		return 0, nil, domain.NewError(domain.ErrInvalidInput, domain.StatePendingLock, domain.Reason{
			Code:    "LINEAGE_MISMATCH",
			Lineage: req.LineageCode,
			Message: "unknown lineage",
		})
	}
	return s.withOperation(ctx, opID, req, func(tx store.Tx) (int, any, error) {
		now := s.now()
		t := &domain.StorageTask{
			ID:            req.TaskID,
			LineageCode:   req.LineageCode,
			BatchCode:     req.BatchCode,
			MothBagDigest: req.MothBagDigest,
			State:         domain.StatePendingLock,
			Generation:    0,
			Version:       1,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := tx.CreateTask(ctx, t); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		return 201, taskView(t), nil
	})
}

// LockTaskRequest is the complete lock manifest.
type LockTaskRequest struct {
	RuleVersion int64
	Generation  int64
	CardSeals   []string
	BlindCodes  []string
	DayAges     []int
	SlideNos    []string
	Reviewers   []string
}

// LockTask validates and freezes the immutable lock snapshot. Any unique-item
// conflict, stale rule, or mismatch rolls back the whole transaction.
func (s *Service) LockTask(ctx context.Context, opID, taskID string, req LockTaskRequest) (int, any, error) {
	return s.withOperation(ctx, opID, req, func(tx store.Tx) (int, any, error) {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if t.State != domain.StatePendingLock {
			return 0, nil, domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{
				Code:    "NOT_PENDING_LOCK",
				Message: "task is already locked or advanced",
			})
		}
		rule, ok := s.dir.Lookup(t.LineageCode, req.RuleVersion)
		if !ok {
			return 0, nil, domain.NewError(domain.ErrInvalidInput, t.State, domain.Reason{
				Code:    "STALE_RULE_VERSION",
				Lineage: t.LineageCode,
				Message: "catalog version not found",
			})
		}
		if err := task.CheckGeneration(req.Generation, t.Generation+1); err != nil {
			return 0, nil, err
		}
		creq := catalog.LockRequest{
			LineageCode:   t.LineageCode,
			BatchCode:     t.BatchCode,
			MothBagDigest: t.MothBagDigest,
			RuleVersion:   req.RuleVersion,
			Generation:    req.Generation,
			CardSeals:     req.CardSeals,
			BlindCodes:    req.BlindCodes,
			DayAges:       req.DayAges,
			SlideNos:      req.SlideNos,
			Reviewers:     req.Reviewers,
		}
		if err := catalog.ValidateLock(creq, rule); err != nil {
			return 0, nil, err
		}
		t.Snapshot = catalog.SnapshotFromRequest(creq, rule)
		t.Generation = req.Generation
		if err := task.Advance(t, domain.StatePendingProductionConfirmation); err != nil {
			return 0, nil, err
		}
		t.UpdatedAt = s.now()
		if err := tx.UpdateTask(ctx, t); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		return 200, taskView(t), nil
	})
}

// GetTask returns the full aggregate view of a task.
func (s *Service) GetTask(ctx context.Context, taskID string) (int, any, error) {
	var detail any
	err := s.store.WithTx(ctx, func(tx store.Tx) error {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return mapStoreErr(err)
		}
		d, err := s.buildDetail(ctx, tx, t)
		if err != nil {
			return err
		}
		detail = d
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	return 200, detail, nil
}

// rule returns the catalog rule frozen at lock time for a task, matching the
// rule by version so later catalog changes never retroactively alter a locked
// task's qualifications or thresholds.
func (s *Service) rule(t *domain.StorageTask) (domain.CatalogRule, bool) {
	if t.Snapshot == nil {
		return s.dir.Current(t.LineageCode)
	}
	return s.dir.Lookup(t.LineageCode, t.Snapshot.CatalogVersion)
}

// requireGeneration rejects a command whose generation does not match the task.
func requireGeneration(t *domain.StorageTask, gen int64) error {
	if gen != t.Generation {
		return domain.NewError(domain.ErrStaleGeneration, t.State, domain.Reason{
			Code:    "STALE_GENERATION",
			Message: "command generation does not match task generation",
		})
	}
	return nil
}

// taskView is the compact task representation for create/lock responses.
func taskView(t *domain.StorageTask) map[string]any {
	v := map[string]any{
		"id":          t.ID,
		"lineageCode": t.LineageCode,
		"batchCode":   t.BatchCode,
		"state":       t.State.String(),
		"generation":  t.Generation,
		"version":     t.Version,
	}
	if t.Snapshot != nil {
		v["locked"] = true
		v["catalogVersion"] = t.Snapshot.CatalogVersion
	}
	return v
}

func mapStoreErr(err error) error {
	switch err {
	case store.ErrDuplicate:
		return domain.NewError(domain.ErrDuplicateResource, domain.StatePendingLock, domain.Reason{
			Code:    "DUPLICATE_RESOURCE",
			Message: "resource already in use",
		})
	case store.ErrConcurrentVersion:
		return domain.NewError(domain.ErrConcurrentModification, domain.StatePendingLock, domain.Reason{
			Code:    "CONCURRENT_MODIFICATION",
			Message: "task was modified concurrently",
		})
	case store.ErrNotFound:
		return domain.NewError(domain.ErrNotFound, domain.StatePendingLock, domain.Reason{
			Code:    "NOT_FOUND",
			Message: "resource not found",
		})
	default:
		return err
	}
}
