package app

import (
	"context"
	"sort"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/ledger"
	"silkworm-egg-cold-storage-gate/internal/store"
)

// RecoverResult reports the outcome of a startup recovery scan.
type RecoverResult struct {
	ExpiredLeases int
	ResumedCalls  int
}

// Recover runs the deterministic restart recovery scan: it expires overdue
// leases and resumes every unfinished instrument call whose next-retry instant
// has arrived and whose retry budget is not exhausted, ordered by call ID.
// Successful or budget-exhausted calls are left untouched; a resumed call
// records its attempt idempotently by call ID so a crash mid-resume can be
// replayed safely without fabricating a reading or exceeding the budget.
func (s *Service) Recover(ctx context.Context, logicalTime int64) (RecoverResult, error) {
	var res RecoverResult
	if r, ok := s.store.(interface {
		Recover(ctx context.Context, logicalTime int64) (int, error)
	}); ok {
		expired, err := r.Recover(ctx, logicalTime)
		if err != nil {
			return res, err
		}
		res.ExpiredLeases = expired
	}

	// Snapshot the unfinished calls inside one transaction, then resume each
	// due, budget-remaining call outside any transaction: the adapter never
	// shares a transaction with the attempt write, matching Start/Retry.
	var pending []domain.InstrumentCall
	if err := s.store.WithTx(ctx, func(tx store.Tx) error {
		calls, err := tx.ListPendingCalls(ctx)
		if err != nil {
			return err
		}
		pending = calls
		return nil
	}); err != nil {
		return res, err
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].ID < pending[j].ID })

	for _, c := range pending {
		if c.State == domain.CallSucceeded {
			continue
		}
		if len(c.Attempts) >= ledger.MaxAttempts {
			continue
		}
		if c.NextRetryAt > logicalTime {
			continue
		}
		if err := s.resumeCall(ctx, c.ID); err != nil {
			return res, err
		}
		res.ResumedCalls++
	}
	return res, nil
}

// resumeCall re-runs a pending call's adapter and records the attempt.
func (s *Service) resumeCall(ctx context.Context, callID string) error {
	var call domain.InstrumentCall
	err := s.store.WithTx(ctx, func(tx store.Tx) error {
		c, err := tx.GetInstrumentCall(ctx, callID)
		if err != nil {
			return err
		}
		call = *c
		return nil
	})
	if err != nil {
		return err
	}
	attempt := s.runner.Run(ctx, call)
	_, err = s.recordAttempt(ctx, callID, attempt)
	return err
}
