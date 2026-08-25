package arbiter

import (
	"fmt"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

// DecisionVersion is the single terminal decision version: exactly one final
// decision may ever exist for a task, so its version is fixed at one.
const DecisionVersion int64 = 1

// Credential builds the deterministic, globally unique cold-storage admission
// credential minted when a task is admitted. It is unique because the task ID
// is the database primary key and admits happen at most once per task.
func Credential(taskID string, decisionVer int64) string {
	return fmt.Sprintf("ADM-%s-%d", taskID, decisionVer)
}

// NewDecision constructs a terminal decision. Admit decisions carry the
// unique admission credential; isolate and cancel carry an empty credential.
func NewDecision(taskID string, typ domain.FinalDecisionType, decidedBy string, atLogical int64) domain.FinalDecision {
	d := domain.FinalDecision{
		TaskID:      taskID,
		Type:        typ,
		DecisionVer: DecisionVersion,
		DecidedBy:   decidedBy,
		AtLogical:   atLogical,
	}
	if typ == domain.FinalAdmit {
		d.AdmitCredential = Credential(taskID, DecisionVersion)
	}
	return d
}

// ReviewConclusion is the conclusion a reviewer submits.
type ReviewConclusion string

const (
	ReviewPass ReviewConclusion = "PASS"
	ReviewFail ReviewConclusion = "FAIL"
)

// Concurring reviews report every reviewer's conclusion is PASS.
func Concurring(reviews []domain.Review) bool {
	if len(reviews) < 2 {
		return false
	}
	for _, r := range reviews {
		if r.Conclusion != string(ReviewPass) {
			return false
		}
	}
	return true
}
