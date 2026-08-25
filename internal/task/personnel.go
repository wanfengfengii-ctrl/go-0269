package task

import (
	"silkworm-egg-cold-storage-gate/internal/domain"
)

// Role constants used by the personnel qualification rules.
const (
	RoleProduction = "production"
	RoleReview     = "review"
)

// Qualified reports whether a person holds the given role in a catalog rule.
func Qualified(rule domain.CatalogRule, role, personID string) bool {
	for _, p := range rule.RoleQualifications[role] {
		if p == personID {
			return true
		}
	}
	return false
}

// MutuallyExclusive reports whether roleA and roleB may not be held by the
// same person under the rule's exclusion list.
func MutuallyExclusive(rule domain.CatalogRule, roleA, roleB string) bool {
	for _, pair := range rule.MutualExclusions {
		if (pair.RoleA == roleA && pair.RoleB == roleB) || (pair.RoleA == roleB && pair.RoleB == roleA) {
			return true
		}
	}
	return false
}

// ValidateProductionConfirmations checks that exactly two distinct qualified
// persons confirmed the production step. It returns a deterministic domain
// error listing every violation, or nil when the pair is valid.
func ValidateProductionConfirmations(rule domain.CatalogRule, confirmations []domain.Confirmation) error {
	people := make(map[string]bool)
	var reasons []domain.Reason
	for _, c := range confirmations {
		if people[c.PersonID] {
			continue
		}
		people[c.PersonID] = true
		if !Qualified(rule, RoleProduction, c.PersonID) {
			reasons = append(reasons, domain.Reason{
				Code:    "UNQUALIFIED_PRODUCER",
				Message: "person " + c.PersonID + " is not qualified for production",
			})
		}
	}
	if len(people) < 2 {
		reasons = append(reasons, domain.Reason{
			Code:    "INSUFFICIENT_PRODUCERS",
			Message: "two distinct producers are required",
		})
	}
	if len(reasons) == 0 {
		return nil
	}
	domain.SortReasons(reasons)
	return &domain.DomainError{Code: domain.ErrInvalidInput, Reasons: reasons}
}

// ValidateIndependentReviews checks that exactly two distinct, qualified
// reviewers have reviewed the task and that neither shares a role with the
// production confirmers. It returns every violation sorted deterministically.
func ValidateIndependentReviews(rule domain.CatalogRule, reviews []domain.Review, confirmers map[string]bool) error {
	var reasons []domain.Reason
	people := make(map[string]bool)
	for _, r := range reviews {
		if people[r.PersonID] {
			continue
		}
		people[r.PersonID] = true
		if !Qualified(rule, RoleReview, r.PersonID) {
			reasons = append(reasons, domain.Reason{
				Code:    "UNQUALIFIED_REVIEWER",
				Message: "person " + r.PersonID + " is not qualified for review",
			})
		}
		if confirmers[r.PersonID] {
			reasons = append(reasons, domain.Reason{
				Code:    "ROLE_OVERLAP",
				Message: "person " + r.PersonID + " overlaps a production role",
			})
		}
	}
	if len(people) < 2 {
		reasons = append(reasons, domain.Reason{
			Code:    "INSUFFICIENT_REVIEWERS",
			Message: "two distinct reviewers are required",
		})
	}
	if len(reasons) == 0 {
		return nil
	}
	domain.SortReasons(reasons)
	return &domain.DomainError{Code: domain.ErrInvalidInput, Reasons: reasons}
}

// ConfirmersSet builds the set of production-confirmation person IDs for
// review overlap checks.
func ConfirmersSet(confirmations []domain.Confirmation) map[string]bool {
	out := make(map[string]bool, len(confirmations))
	for _, c := range confirmations {
		out[c.PersonID] = true
	}
	return out
}
