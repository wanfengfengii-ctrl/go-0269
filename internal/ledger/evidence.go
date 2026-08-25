package ledger

import "silkworm-egg-cold-storage-gate/internal/domain"

// InGeneration filters an evidence chain to a single task generation. Late
// results from older generations remain in the store for audit but are never
// part of the current decision chain; the arbiter and application service use
// this filter when computing recheck triggers and evidence closure.
func InGeneration(chain []domain.EvidenceVersion, generation int64) []domain.EvidenceVersion {
	out := make([]domain.EvidenceVersion, 0, len(chain))
	for _, e := range chain {
		if e.Generation == generation {
			out = append(out, e)
		}
	}
	return out
}
