// Package catalog implements the lineage and incubation-rule directory: it
// holds versioned lineage rules and validates lock snapshots against them.
// A lock snapshot is immutable and must carry the catalog version, task
// generation, and every specified entity and threshold. Any lineage/batch
// mismatch, stale moth-bag digest, duplicate seal, duplicate blind code, or
// premature reveal rejects the whole transaction.
package catalog

import (
	"sort"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

// LockRequest is the complete list of entities a caller submits to lock a
// storage task in a single transaction.
type LockRequest struct {
	LineageCode   string
	BatchCode     string
	MothBagDigest string
	RuleVersion   int64
	Generation    int64
	CardSeals     []string
	BlindCodes    []string
	DayAges       []int
	SlideNos      []string
	Reviewers     []string
}

// ValidateLock checks a lock request against a catalog rule and returns all
// deterministic, sorted rejection reasons. A nil error means the snapshot is
// valid and may be written.
func ValidateLock(req LockRequest, rule domain.CatalogRule) error {
	var reasons []domain.Reason
	add := func(code, msg string) {
		reasons = append(reasons, domain.Reason{
			Code:    code,
			Lineage: req.LineageCode,
			Batch:   req.BatchCode,
			Message: msg,
		})
	}

	if req.LineageCode == "" || req.LineageCode != rule.LineageCode {
		add("LINEAGE_MISMATCH", "lineage does not match catalog rule")
	}
	if req.RuleVersion != rule.RuleVersion {
		add("STALE_RULE_VERSION", "expected catalog version does not match current rule")
	}
	if !containsString(rule.AllowedBatches, req.BatchCode) {
		add("BATCH_NOT_ALLOWED", "batch is not allowed by catalog rule")
	}
	if req.Generation < 1 {
		add("INVALID_GENERATION", "task generation must be positive")
	}
	if dups := duplicateStrings(req.CardSeals); len(dups) > 0 {
		for _, d := range dups {
			reasons = append(reasons, domain.Reason{
				Code:     "DUPLICATE_CARD_SEAL",
				Lineage:  req.LineageCode,
				Batch:    req.BatchCode,
				CardSeal: d,
				Message:  "duplicate egg-card seal in lock request",
			})
		}
	}
	if dups := duplicateStrings(req.BlindCodes); len(dups) > 0 {
		for range dups {
			reasons = append(reasons, domain.Reason{
				Code:    "DUPLICATE_BLIND_CODE",
				Lineage: req.LineageCode,
				Batch:   req.BatchCode,
				Message: "duplicate blind code in lock request",
			})
		}
	}
	if len(req.CardSeals) == 0 {
		add("MISSING_CARD_SEALS", "at least one egg-card seal is required")
	}
	if len(req.DayAges) == 0 {
		add("MISSING_DAY_AGES", "at least one incubation day-age is required")
	}
	if len(req.Reviewers) < 2 {
		add("INSUFFICIENT_REVIEWERS", "at least two independent reviewers are required")
	}

	if len(reasons) == 0 {
		return nil
	}
	domain.SortReasons(reasons)
	return &domain.DomainError{
		Code:    domain.ErrInvalidInput,
		State:   domain.StatePendingLock,
		Reasons: reasons,
	}
}

// SnapshotFromRequest builds the immutable lock snapshot captured on success.
func SnapshotFromRequest(req LockRequest, rule domain.CatalogRule) *domain.LockSnapshot {
	snap := &domain.LockSnapshot{
		CatalogVersion: rule.RuleVersion,
		Generation:     req.Generation,
		LineageCode:    req.LineageCode,
		BatchCode:      req.BatchCode,
		MothBagDigest:  req.MothBagDigest,
		CardSeals:      append([]string(nil), req.CardSeals...),
		BlindCodes:     append([]string(nil), req.BlindCodes...),
		DayAges:        append([]int(nil), req.DayAges...),
		SlideNos:       append([]string(nil), req.SlideNos...),
		Reviewers:      append([]string(nil), req.Reviewers...),
		SampleSize:     rule.SampleSize,
		Thresholds:     rule.Thresholds,
	}
	sort.Strings(snap.CardSeals)
	sort.Strings(snap.BlindCodes)
	sort.Strings(snap.SlideNos)
	sort.Ints(snap.DayAges)
	return snap
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func duplicateStrings(list []string) []string {
	seen := make(map[string]bool, len(list))
	var dups []string
	for _, s := range list {
		if seen[s] {
			dups = append(dups, s)
		}
		seen[s] = true
	}
	return dups
}
