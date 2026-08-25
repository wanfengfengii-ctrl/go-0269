package app

import (
	"context"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/store"
)

// buildDetail assembles the full aggregate view returned by GET /v1/tasks/{id}:
// the task aggregate, its lock snapshot, sample seals, leases, coverage
// summary, evidence chain, recheck, confirmations, reviews, and terminal
// decision.
func (s *Service) buildDetail(ctx context.Context, tx store.Tx, t *domain.StorageTask) (map[string]any, error) {
	seals, err := tx.ListSampleSeals(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	leases, err := tx.ListLeases(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	cells, err := tx.ListObservations(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	evidence, err := tx.ListEvidence(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	confirmations, err := tx.ListConfirmations(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	reviews, err := tx.ListReviews(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	decision, err := tx.GetFinalDecision(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	recheck, err := tx.GetRecheck(ctx, t.ID, t.Generation)
	if err != nil {
		return nil, err
	}

	v := taskView(t)
	if t.Snapshot != nil {
		v["catalogVersion"] = t.Snapshot.CatalogVersion
		v["mothBagDigest"] = t.MothBagDigest
		v["snapshot"] = map[string]any{
			"lineageCode": t.Snapshot.LineageCode,
			"batchCode":   t.Snapshot.BatchCode,
			"cardSeals":   t.Snapshot.CardSeals,
			"blindCodes":  t.Snapshot.BlindCodes,
			"dayAges":     t.Snapshot.DayAges,
			"slideNos":    t.Snapshot.SlideNos,
			"reviewers":   t.Snapshot.Reviewers,
			"sampleSize":  t.Snapshot.SampleSize,
		}
	}
	v["seals"] = sealViews(seals)
	v["leases"] = leaseViews(leases)
	v["coverage"] = map[string]any{
		"cellCount": len(cells),
		"cells":     cellViews(cells),
	}
	v["evidence"] = evidenceViews(evidence)
	v["confirmations"] = confirmationViews(confirmations)
	v["reviews"] = reviewViews(reviews)
	if recheck != nil {
		v["recheck"] = map[string]any{
			"generation":      recheck.Generation,
			"triggerSummary":  recheck.TriggerSummary,
			"affectedCards":   recheck.AffectedCards,
			"affectedBlinds":  recheck.AffectedBlinds,
			"affectedDayAges": recheck.AffectedDayAges,
			"affectedSlides":  recheck.AffectedSlides,
			"evidenceClosed":  recheck.EvidenceClosed,
			"conclusion":      recheck.Conclusion,
		}
	}
	if decision != nil {
		v["decision"] = decisionView(*decision)
	}
	return v, nil
}

func sealViews(seals []domain.SampleSeal) []map[string]any {
	out := make([]map[string]any, 0, len(seals))
	for _, s := range seals {
		out = append(out, map[string]any{
			"cardSeal":   s.CardSeal,
			"sampleNo":   s.SampleNo,
			"triplet":    s.Triplet,
			"sealDigest": s.SealDigest,
			"blindCode":  s.BlindCode,
			"revealed":   s.Revealed,
		})
	}
	return out
}

func leaseViews(leases []domain.ResourceLease) []map[string]any {
	out := make([]map[string]any, 0, len(leases))
	for _, l := range leases {
		out = append(out, leaseView(l))
	}
	return out
}

func evidenceViews(evidence []domain.EvidenceVersion) []map[string]any {
	out := make([]map[string]any, 0, len(evidence))
	for _, e := range evidence {
		out = append(out, evidenceView(e))
	}
	return out
}

func confirmationViews(cs []domain.Confirmation) []map[string]any {
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, map[string]any{
			"role":       c.Role,
			"personId":   c.PersonID,
			"generation": c.Generation,
			"conclusion": c.Conclusion,
		})
	}
	return out
}

func reviewViews(rs []domain.Review) []map[string]any {
	out := make([]map[string]any, 0, len(rs))
	for _, r := range rs {
		out = append(out, map[string]any{
			"personId":   r.PersonID,
			"generation": r.Generation,
			"conclusion": r.Conclusion,
		})
	}
	return out
}
