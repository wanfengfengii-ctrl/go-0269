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
// has arrived, ordered by call ID. Successful or exhausted calls are left
// untouched; a resumed call records its attempt idempotently by call ID so a
// crash mid-resume can be replayed safely.
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

	var pending []string
	if err := s.store.WithTx(ctx, func(tx store.Tx) error {
		calls, err := tx.ListPendingCalls(ctx)
		if err != nil {
			return err
		}
		for _, c := range calls {
			if c.NextRetryAt <= logicalTime && len(c.Attempts) < ledger.MaxAttempts {
				pending = append(pending, c.ID)
			}
		}
		return nil
	}); err != nil {
		return res, err
	}
	sort.Strings(pending)
	for _, id := range pending {
		if err := s.resumeCall(ctx, id); err == nil {
			res.ResumedCalls++
		}
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
