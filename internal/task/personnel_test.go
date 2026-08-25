package task

import (
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/fixedpoint"
)

func testRule() domain.CatalogRule {
	th, _ := fixedpoint.Parse("80.00", 2)
	return domain.CatalogRule{
		LineageCode:       "lineage-001",
		RuleVersion:       1,
		AllowedBatches:    []string{"batch-001"},
		IncubationDayAges: []int{1, 2},
		SampleSize:        100,
		Thresholds:        domain.Thresholds{MinHatchRate: th},
		RoleQualifications: map[string][]string{
			"production": {"person-a", "person-b"},
			"review":     {"person-c", "person-d"},
		},
		MutualExclusions: []domain.RolePair{{RoleA: "production", RoleB: "review"}},
	}
}

func TestQualified(t *testing.T) {
	r := testRule()
	if !Qualified(r, RoleProduction, "person-a") {
		t.Fatal("person-a should be production-qualified")
	}
	if Qualified(r, RoleProduction, "person-c") {
		t.Fatal("person-c should not be production-qualified")
	}
	if !Qualified(r, RoleReview, "person-c") {
		t.Fatal("person-c should be review-qualified")
	}
}

func TestMutuallyExclusive(t *testing.T) {
	r := testRule()
	if !MutuallyExclusive(r, RoleProduction, RoleReview) {
		t.Fatal("production and review should be mutually exclusive")
	}
	if MutuallyExclusive(r, RoleReview, RoleReview) {
		t.Fatal("a role is not exclusive with itself")
	}
}

func TestValidateProductionConfirmationsDistinct(t *testing.T) {
	r := testRule()
	conf := []domain.Confirmation{
		{PersonID: "person-a"},
		{PersonID: "person-b"},
	}
	if err := ValidateProductionConfirmations(r, conf); err != nil {
		t.Fatalf("valid pair rejected: %v", err)
	}
	// A single person (even duplicated) is insufficient.
	if err := ValidateProductionConfirmations(r, []domain.Confirmation{{PersonID: "person-a"}, {PersonID: "person-a"}}); err == nil {
		t.Fatal("expected insufficient producers error")
	}
}

func TestValidateIndependentReviewsRoleOverlap(t *testing.T) {
	r := testRule()
	reviews := []domain.Review{
		{PersonID: "person-c"},
		{PersonID: "person-a"}, // overlaps production
	}
	err := ValidateIndependentReviews(r, reviews, ConfirmersSet([]domain.Confirmation{{PersonID: "person-a"}}))
	if err == nil {
		t.Fatal("expected role overlap error")
	}
	if !hasReason(err, "ROLE_OVERLAP") {
		t.Fatalf("missing ROLE_OVERLAP reason: %v", err)
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
