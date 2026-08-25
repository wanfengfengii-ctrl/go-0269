// Package arbiter implements the microsporidiosis recheck and final-decision
// arbitration. It detects the recheck triggers, builds the single current
// generation recheck case, isolates stale evidence, and competes the three
// terminal decisions (admit, isolate, cancel) through the task-level single
// write barrier.
package arbiter

import (
	"sort"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/fixedpoint"
)

// Trigger codes surfaced in a recheck's trigger summary.
const (
	TriggerLowVitality       = "LOW_VITALITY"
	TriggerDeadRateExceeded  = "DEAD_RATE_EXCEEDED"
	TriggerSuspectedPebrine  = "SUSPECTED_PEBRINE"
	TriggerTripletDivergence = "TRIPLET_DIVERGENCE"
)

// DetectTriggers evaluates the fixed-point readings against the locked
// thresholds and returns the sorted set of recheck triggers. Triplet
// divergence and suspected pebrine are passed in by the caller after their
// own evidence has been gathered; hatch and dead-egg rates are compared here
// using fixed-point cross multiplication.
func DetectTriggers(hatchRate, deadRate, thresholds fixedpoint.Value, maxDead, maxSpore, sporeReading fixedpoint.Value, tripletDivergence bool) []string {
	var triggers []string
	if fixedpoint.Cmp(hatchRate, thresholds) < 0 {
		triggers = append(triggers, TriggerLowVitality)
	}
	if fixedpoint.Cmp(deadRate, maxDead) > 0 {
		triggers = append(triggers, TriggerDeadRateExceeded)
	}
	if fixedpoint.Cmp(sporeReading, maxSpore) > 0 {
		triggers = append(triggers, TriggerSuspectedPebrine)
	}
	if tripletDivergence {
		triggers = append(triggers, TriggerTripletDivergence)
	}
	sort.Strings(triggers)
	return triggers
}

// BuildRecheck assembles the single recheck case for the current generation
// from the trigger set and the affected objects. Affected card seals, blind
// codes, day ages, and slides are deduplicated and sorted so the persisted
// coverage set is deterministic.
func BuildRecheck(taskID string, generation int64, triggers []string, cards, blinds []string, days []int, slides []string) domain.RecheckCase {
	return domain.RecheckCase{
		TaskID:          taskID,
		Generation:      generation,
		TriggerSummary:  joinSorted(triggers),
		AffectedCards:   uniqueSorted(cards),
		AffectedBlinds:  uniqueSorted(blinds),
		AffectedDayAges: uniqueSortedInts(days),
		AffectedSlides:  uniqueSorted(slides),
		EvidenceClosed:  false,
		Conclusion:      "",
	}
}

func joinSorted(list []string) string {
	out := ""
	for i, s := range list {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

func uniqueSorted(list []string) []string {
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func uniqueSortedInts(list []int) []int {
	seen := make(map[int]bool, len(list))
	out := make([]int, 0, len(list))
	for _, n := range list {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}
