package app

import (
	"context"
	"encoding/json"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/ledger"
	"silkworm-egg-cold-storage-gate/internal/store"
)

// StartInstrumentCallRequest drives a scriptable instrument adapter.
type StartInstrumentCallRequest struct {
	Generation     int64
	InstrumentType domain.InstrumentType
	TargetObject   string
	RequestDigest  string
}

// instrumentContent is the canonical idempotency key for an instrument call:
// it binds the call to its task so identical requests on different tasks never
// collide.
type instrumentContent struct {
	TaskID string
	Req    StartInstrumentCallRequest
}

// StartInstrumentCall persists a call intent, executes the adapter, and
// records the attempt idempotently by call ID. The adapter never shares a
// transaction with the intent write, so a crash can resume the same call
// without fabricating a reading.
func (s *Service) StartInstrumentCall(ctx context.Context, opID, taskID string, req StartInstrumentCallRequest) (int, any, error) {
	content := instrumentContent{TaskID: taskID, Req: req}
	hash := contentHash(content)
	callID := hash[:24]

	var call domain.InstrumentCall
	replay := false
	err := s.store.WithTx(ctx, func(tx store.Tx) error {
		existing, err := tx.GetOperation(ctx, opID)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.ContentHash != hash {
				return domain.NewError(domain.ErrOperationConflict, domain.StatePendingLock, domain.Reason{
					Code:    "OPERATION_CONFLICT",
					Message: "same operation ID with different content",
				})
			}
			c, err := tx.GetInstrumentCall(ctx, callID)
			if err != nil {
				return err
			}
			call = *c
			replay = true
			return nil
		}
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return mapStoreErr(err)
		}
		if err := requireGeneration(t, req.Generation); err != nil {
			return err
		}
		if !instrumentAllowed(t.State) {
			return domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{
				Code:    "INSTRUMENT_NOT_ALLOWED",
				Message: "instrument calls are not allowed in this state",
			})
		}
		call = ledger.NewCall(callID, taskID, req.Generation, req.InstrumentType, req.TargetObject, req.RequestDigest, s.now())
		if err := tx.SaveInstrumentCall(ctx, call); err != nil {
			return mapStoreErr(err)
		}
		raw, _ := json.Marshal(callView(call))
		envelope, _ := json.Marshal(opResponse{HTTPStatus: 200, Body: raw})
		return tx.RecordOperation(ctx, opID, hash, envelope)
	})
	if err != nil {
		return 0, nil, err
	}
	if !replay {
		attempt := s.runner.Run(ctx, call)
		updated, err := s.recordAttempt(ctx, callID, attempt)
		if err != nil {
			return 0, nil, err
		}
		call = *updated
	}
	return 200, callView(call), nil
}

// RetryRequest resumes a pending instrument call.
type RetryRequest struct {
	Generation int64
}

// RetryInstrumentCall resumes a failed call when its next-retry instant has
// arrived. It never reveals a blind code, releases a lease, or produces
// evidence on a failed attempt.
func (s *Service) RetryInstrumentCall(ctx context.Context, opID, callID string, req RetryRequest) (int, any, error) {
	hash := contentHash(req)
	var call domain.InstrumentCall
	err := s.store.WithTx(ctx, func(tx store.Tx) error {
		existing, err := tx.GetOperation(ctx, opID)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.ContentHash != hash {
				return domain.NewError(domain.ErrOperationConflict, domain.StatePendingLock, domain.Reason{
					Code:    "OPERATION_CONFLICT",
					Message: "same operation ID with different content",
				})
			}
			return nil
		}
		c, err := tx.GetInstrumentCall(ctx, callID)
		if err != nil {
			return err
		}
		call = *c
		if call.State == domain.CallSucceeded {
			return domain.NewError(domain.ErrInvalidStateTransition, domain.StatePendingLock, domain.Reason{
				Code:    "CALL_ALREADY_SUCCEEDED",
				Message: "call has already succeeded",
			})
		}
		if len(call.Attempts) >= ledger.MaxAttempts {
			return domain.NewError(domain.ErrInvalidStateTransition, domain.StatePendingLock, domain.Reason{
				Code:    "RETRY_BUDGET_EXHAUSTED",
				Message: "call exhausted its retry budget",
			})
		}
		if call.NextRetryAt > s.now() {
			return domain.NewError(domain.ErrInvalidStateTransition, domain.StatePendingLock, domain.Reason{
				Code:    "NOT_RETRYABLE_YET",
				Message: "next retry instant has not arrived",
			})
		}
		raw, _ := json.Marshal(callView(call))
		envelope, _ := json.Marshal(opResponse{HTTPStatus: 200, Body: raw})
		return tx.RecordOperation(ctx, opID, hash, envelope)
	})
	if err != nil {
		return 0, nil, err
	}
	attempt := s.runner.Run(ctx, call)
	updated, err := s.recordAttempt(ctx, callID, attempt)
	if err != nil {
		return 0, nil, err
	}
	call = *updated
	return 200, callView(call), nil
}

// recordAttempt persists a runner attempt and returns the updated call state.
func (s *Service) recordAttempt(ctx context.Context, callID string, attempt domain.InstrumentAttempt) (*domain.InstrumentCall, error) {
	var updated domain.InstrumentCall
	err := s.store.WithTx(ctx, func(tx store.Tx) error {
		c, err := tx.GetInstrumentCall(ctx, callID)
		if err != nil {
			return err
		}
		ledger.RecordAttempt(c, attempt, s.now())
		if err := tx.SaveInstrumentCall(ctx, *c); err != nil {
			return err
		}
		updated = *c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// instrumentAllowed reports whether an instrument call is valid in a state.
func instrumentAllowed(state domain.TaskState) bool {
	switch state {
	case domain.StateObservingHatching, domain.StateVerifyingMicroscopy, domain.StateRetestingPhysicochemical:
		return true
	default:
		return false
	}
}

// callView renders an instrument call and its attempts.
func callView(c domain.InstrumentCall) map[string]any {
	attempts := make([]map[string]any, 0, len(c.Attempts))
	for _, a := range c.Attempts {
		attempts = append(attempts, map[string]any{
			"attemptNo":     a.AttemptNo,
			"state":         a.State.String(),
			"failure":       a.Failure.String(),
			"atLogicalTime": a.AtLogicalTime,
		})
	}
	return map[string]any{
		"id":             c.ID,
		"instrumentType": c.InstrumentType.String(),
		"targetObject":   c.TargetObject,
		"taskId":         c.TaskID,
		"generation":     c.Generation,
		"state":          c.State.String(),
		"attempts":       attempts,
		"nextRetryAt":    c.NextRetryAt,
	}
}
