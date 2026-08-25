// Package ledger implements the incubation-hatching and physicochemical
// ledgers: the day-age coverage matrix with count conservation, the
// append-only evidence version chains, and the retryable instrument call
// outbox. All rate and moisture/temperature arithmetic here is fixed-point;
// no floating point is used.
package ledger

import (
	"fmt"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/fixedpoint"
)

// Coverage is the immutable view a validation runs against: the locked
// snapshot plus the set of already-committed observation cells.
type Coverage struct {
	Snapshot *domain.LockSnapshot
	Cells    []domain.ObservationCell
}

// HasCell reports whether a coverage cell for (cardSeal, dayAge) exists.
func (c Coverage) HasCell(cardSeal string, dayAge int) bool {
	for _, cell := range c.Cells {
		if cell.CardSeal == cardSeal && cell.DayAge == dayAge {
			return true
		}
	}
	return false
}

// Gaps returns every (card seal, day age) pair that the locked snapshot
// requires but that has no committed observation cell, sorted by seal then age.
func (c Coverage) Gaps() [][2]any {
	if c.Snapshot == nil {
		return nil
	}
	have := make(map[string]bool, len(c.Cells))
	for _, cell := range c.Cells {
		have[fmt.Sprintf("%s|%d", cell.CardSeal, cell.DayAge)] = true
	}
	var gaps [][2]any
	for _, seal := range c.Snapshot.CardSeals {
		for _, age := range c.Snapshot.DayAges {
			if !have[fmt.Sprintf("%s|%d", seal, age)] {
				gaps = append(gaps, [2]any{seal, age})
			}
		}
	}
	return gaps
}

// Complete reports whether every required coverage cell is present.
func (c Coverage) Complete() bool {
	return len(c.Gaps()) == 0
}

// ValidateCounts checks that a single observation cell conserves the locked
// sample size: all four counts non-negative and their sum exactly equal to the
// sample size. A nil error means the cell is arithmetically valid.
func ValidateCounts(cell domain.ObservationCell, sampleSize int) error {
	if cell.Hatched < 0 || cell.Unhatched < 0 || cell.Dead < 0 || cell.Damaged < 0 {
		return domain.NewError(domain.ErrInvalidInput, domain.StateObservingHatching, domain.Reason{
			Code:     "NEGATIVE_COUNT",
			CardSeal: cell.CardSeal,
			DayAge:   cell.DayAge,
			Message:  "egg counts must be non-negative",
		})
	}
	sum := cell.Hatched + cell.Unhatched + cell.Dead + cell.Damaged
	if sum != int64(sampleSize) {
		return domain.NewError(domain.ErrInvalidInput, domain.StateObservingHatching, domain.Reason{
			Code:     "CONSERVATION_VIOLATION",
			CardSeal: cell.CardSeal,
			DayAge:   cell.DayAge,
			Message:  fmt.Sprintf("counts sum %d != sample size %d", sum, sampleSize),
		})
	}
	return nil
}

// HatchRate computes the aggregate hatch rate across all committed cells as a
// fixed-point percentage (hatched/total*100). A zero-denominator or overflow
// returns an error.
func (c Coverage) HatchRate(scale int) (fixedpoint.Value, error) {
	var hatched, total int64
	for _, cell := range c.Cells {
		hatched += cell.Hatched
		total += cell.Hatched + cell.Unhatched + cell.Dead + cell.Damaged
	}
	return fixedpoint.Percent(hatched, total, scale)
}

// DeadEggRate computes the aggregate dead-egg rate as a percentage
// (dead/total*100).
func (c Coverage) DeadEggRate(scale int) (fixedpoint.Value, error) {
	var dead, total int64
	for _, cell := range c.Cells {
		dead += cell.Dead
		total += cell.Hatched + cell.Unhatched + cell.Dead + cell.Damaged
	}
	return fixedpoint.Percent(dead, total, scale)
}
