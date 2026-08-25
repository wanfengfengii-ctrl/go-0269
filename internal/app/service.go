// Package app implements the storage-task application service: the command
// handlers that map each HTTP request to an atomic transaction over the store.
// Every command runs inside a single transaction, enforces operation-id
// idempotency, matches the task generation, applies the pure domain rules from
// catalog/task/ledger/arbiter, and persists the aggregate and its ledgers
// atomically.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"silkworm-egg-cold-storage-gate/internal/catalog"
	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/store"
	"silkworm-egg-cold-storage-gate/internal/task"
)

// Runner executes a persisted instrument call against an external adapter.
// Production can use an HTTP adapter; tests inject a deterministic in-memory
// fault script. A call is always persisted before the runner is invoked, and
// the result is recorded idempotently by call ID afterward.
type Runner interface {
	Run(ctx context.Context, call domain.InstrumentCall) domain.InstrumentAttempt
}

// Service is the application service consumed by the HTTP layer.
type Service struct {
	store  store.Store
	dir    *catalog.Directory
	runner Runner
	now    func() int64
}

// NewService builds a service over the store, catalog directory, instrument
// runner, and logical clock. A nil runner defaults to an always-succeeding
// runner; a nil clock defaults to wall-clock seconds.
func NewService(st store.Store, dir *catalog.Directory, runner Runner, now func() int64) *Service {
	if runner == nil {
		runner = SucceedRunner{}
	}
	if now == nil {
		now = wallClock
	}
	return &Service{store: st, dir: dir, runner: runner, now: now}
}

// Store exposes the underlying store for health/readiness wiring.
func (s *Service) Store() store.Store { return s.store }

// SucceedRunner always returns a successful attempt, used when no fault script
// is configured.
type SucceedRunner struct{}

// Run returns a successful attempt carrying a zero reading.
func (SucceedRunner) Run(_ context.Context, call domain.InstrumentCall) domain.InstrumentAttempt {
	return domain.InstrumentAttempt{
		AttemptNo:     len(call.Attempts) + 1,
		State:         domain.CallSucceeded,
		AtLogicalTime: call.NextRetryAt,
	}
}

// opResponse is the envelope persisted with an idempotency record so a replayed
// operation returns the original HTTP status and body verbatim.
type opResponse struct {
	HTTPStatus int             `json:"httpStatus"`
	Body       json.RawMessage `json:"body"`
}

// withOperation wraps a command with operation-id idempotency. It hashes the
// normalized content; a matching operation returns the stored response, a
// mismatched hash returns OPERATION_CONFLICT, and an absent record runs fn and
// records the successful response.
func (s *Service) withOperation(ctx context.Context, opID string, content any, fn func(tx store.Tx) (int, any, error)) (int, any, error) {
	hash := contentHash(content)
	var status int
	var body any

	err := s.store.WithTx(ctx, func(tx store.Tx) error {
		existing, err := tx.GetOperation(ctx, opID)
		if err != nil {
			return err
		}
		opStatus, err := task.CheckOperation(existing, opID, hash)
		if err != nil {
			return err
		}
		switch opStatus {
		case domain.OpIdempotent:
			var resp opResponse
			if err := json.Unmarshal(existing.Response, &resp); err != nil {
				return err
			}
			status = resp.HTTPStatus
			return json.Unmarshal(resp.Body, &body)
		case domain.OpConflict:
			return domain.NewError(domain.ErrOperationConflict, domain.StatePendingLock, domain.Reason{
				Code:    "OPERATION_CONFLICT",
				Message: "same operation ID with different content",
			})
		}
		st, b, err := fn(tx)
		if err != nil {
			return err
		}
		status = st
		body = b
		raw, err := json.Marshal(b)
		if err != nil {
			return err
		}
		envelope, err := json.Marshal(opResponse{HTTPStatus: st, Body: raw})
		if err != nil {
			return err
		}
		return tx.RecordOperation(ctx, opID, hash, envelope)
	})
	if err != nil {
		return 0, nil, err
	}
	return status, body, nil
}

// contentHash produces the canonical content hash of a request value.
func contentHash(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// wallClock returns the current wall-clock second.
func wallClock() int64 { return time.Now().Unix() }
