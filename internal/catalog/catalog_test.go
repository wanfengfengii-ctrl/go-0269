package catalog

import (
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/fixedpoint"
)

func testRule() domain.CatalogRule {
	th, _ := fixedpoint.Parse("80.00", 2)
	return domain.CatalogRule{
		LineageCode:       "lineage-001",
		RuleVersion:       3,
		AllowedBatches:    []string{"batch-001"},
		IncubationDayAges: []int{1, 2, 3},
		Thresholds:        domain.Thresholds{MinHatchRate: th},
	}
}

func validReq() LockRequest {
	return LockRequest{
		LineageCode:   "lineage-001",
		BatchCode:     "batch-001",
		MothBagDigest: "digest-1",
		RuleVersion:   3,
		Generation:    1,
		CardSeals:     []string{"seal-1", "seal-2"},
		BlindCodes:    []string{"blind-1", "blind-2"},
		DayAges:       []int{1, 2, 3},
		SlideNos:      []string{"slide-1"},
		Reviewers:     []string{"person-c", "person-d"},
	}
}

func TestValidateLockSuccess(t *testing.T) {
	if err := ValidateLock(validReq(), testRule()); err != nil {
		t.Fatalf("ValidateLock success error = %v", err)
	}
}

func TestValidateLockLineageMismatch(t *testing.T) {
	req := validReq()
	req.LineageCode = "lineage-999"
	err := ValidateLock(req, testRule())
	if err == nil {
		t.Fatal("expected error for lineage mismatch")
	}
	if !hasReason(err, "LINEAGE_MISMATCH") {
		t.Fatalf("missing LINEAGE_MISMATCH reason: %v", err)
	}
}

func TestValidateLockStaleRuleVersion(t *testing.T) {
	req := validReq()
	req.RuleVersion = 2
	err := ValidateLock(req, testRule())
	if err == nil || !hasReason(err, "STALE_RULE_VERSION") {
		t.Fatalf("expected stale rule version, got %v", err)
	}
}

func TestValidateLockDuplicateSealsRollback(t *testing.T) {
	req := validReq()
	req.CardSeals = []string{"seal-1", "seal-1"}
	err := ValidateLock(req, testRule())
	if err == nil || !hasReason(err, "DUPLICATE_CARD_SEAL") {
		t.Fatalf("expected duplicate seal rejection, got %v", err)
	}
}

func TestValidateLockDuplicateBlindCodes(t *testing.T) {
	req := validReq()
	req.BlindCodes = []string{"blind-1", "blind-1"}
	err := ValidateLock(req, testRule())
	if err == nil || !hasReason(err, "DUPLICATE_BLIND_CODE") {
		t.Fatalf("expected duplicate blind code rejection, got %v", err)
	}
}

func TestSnapshotFromRequestSorted(t *testing.T) {
	req := validReq()
	req.CardSeals = []string{"seal-2", "seal-1"}
	req.BlindCodes = []string{"blind-2", "blind-1"}
	req.DayAges = []int{3, 1, 2}
	snap := SnapshotFromRequest(req, testRule())
	if snap.CardSeals[0] != "seal-1" || snap.CardSeals[1] != "seal-2" {
		t.Fatalf("card seals not sorted: %v", snap.CardSeals)
	}
	if snap.DayAges[0] != 1 || snap.DayAges[2] != 3 {
		t.Fatalf("day ages not sorted: %v", snap.DayAges)
	}
	if snap.Generation != 1 || snap.CatalogVersion != 3 {
		t.Fatalf("snapshot metadata wrong: %+v", snap)
	}
}

func hasReason(err error, code string) bool {
	de, ok := err.(*domain.DomainError)
	if !ok {
		return false
	}
	for _, r := range de.Reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}
