package store

import (
	"context"
	"fmt"
)

// Recover runs the deterministic restart recovery scan inside a single
// transaction: it marks overdue leases expired and returns the count so the
// caller can log it. Open tasks, coverage matrices, blind codes, evidence
// chains, and unfinished instrument calls remain intact in the database and
// are picked up by the application service on demand.
func (s *SQLiteStore) Recover(ctx context.Context, logicalTime int64) (int, error) {
	if !s.Ready() {
		return 0, fmt.Errorf("store: not migrated")
	}
	var expired int
	err := s.WithTx(ctx, func(tx Tx) error {
		n, err := tx.ExpireLeases(ctx, logicalTime)
		if err != nil {
			return err
		}
		expired = n
		return nil
	})
	return expired, err
}
