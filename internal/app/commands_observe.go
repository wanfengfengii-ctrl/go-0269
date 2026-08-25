package app

import (
	"context"

	"silkworm-egg-cold-storage-gate/internal/arbiter"
	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/fixedpoint"
	"silkworm-egg-cold-storage-gate/internal/ledger"
	"silkworm-egg-cold-storage-gate/internal/store"
	"silkworm-egg-cold-storage-gate/internal/task"
)

// ObservationRequest is one coverage cell submission.
type ObservationRequest struct {
	Generation int64
	Cells      []domain.ObservationCell
}

// SubmitObservations commits coverage cells, enforcing count conservation and
// rejecting duplicate day ages, missing ages, and unauthorized overrides.
func (s *Service) SubmitObservations(ctx context.Context, opID, taskID string, req ObservationRequest) (int, any, error) {
	return s.withOperation(ctx, opID, req, func(tx store.Tx) (int, any, error) {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if t.State != domain.StateObservingHatching {
			return 0, nil, domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{
				Code:    "NOT_OBSERVING",
				Message: "task is not in hatch observation",
			})
		}
		if err := requireGeneration(t, req.Generation); err != nil {
			return 0, nil, err
		}
		if t.Snapshot == nil {
			return 0, nil, domain.NewError(domain.ErrInternal, t.State, domain.Reason{Code: "MISSING_SNAPSHOT"})
		}
		existing, err := tx.ListObservations(ctx, taskID)
		if err != nil {
			return 0, nil, err
		}
		existingMap := make(map[[2]any]domain.ObservationCell, len(existing))
		for _, c := range existing {
			existingMap[[2]any{c.CardSeal, c.DayAge}] = c
		}
		snapshot := *t.Snapshot
		for _, cell := range req.Cells {
			if !containsStr(snapshot.CardSeals, cell.CardSeal) {
				return 0, nil, domain.NewError(domain.ErrInvalidInput, t.State, domain.Reason{
					Code:     "UNKNOWN_CARD_SEAL",
					CardSeal: cell.CardSeal,
					Message:  "card seal not in locked snapshot",
				})
			}
			if !containsInt(snapshot.DayAges, cell.DayAge) {
				return 0, nil, domain.NewError(domain.ErrInvalidInput, t.State, domain.Reason{
					Code:     "UNKNOWN_DAY_AGE",
					CardSeal: cell.CardSeal,
					DayAge:   cell.DayAge,
					Message:  "day age not in locked snapshot",
				})
			}
			key := [2]any{cell.CardSeal, cell.DayAge}
			if prior, ok := existingMap[key]; ok && !prior.Rechecked && !cell.Rechecked {
				return 0, nil, domain.NewError(domain.ErrDuplicateResource, t.State, domain.Reason{
					Code:     "DUPLICATE_COVERAGE",
					CardSeal: cell.CardSeal,
					DayAge:   cell.DayAge,
					Message:  "coverage cell already submitted without recheck authorization",
				})
			}
			if err := ledger.ValidateCounts(cell, snapshot.SampleSize); err != nil {
				return 0, nil, err
			}
			cell.TaskID = taskID
			cell.EvidenceVer = t.Generation
			if err := tx.SaveObservation(ctx, cell); err != nil {
				return 0, nil, mapStoreErr(err)
			}
			existingMap[key] = cell
		}
		all, err := tx.ListObservations(ctx, taskID)
		if err != nil {
			return 0, nil, err
		}
		cov := ledger.Coverage{Snapshot: &snapshot, Cells: all}
		if cov.Complete() {
			if err := task.Advance(t, domain.StateVerifyingMicroscopy); err != nil {
				return 0, nil, err
			}
			t.UpdatedAt = s.now()
			if err := tx.UpdateTask(ctx, t); err != nil {
				return 0, nil, mapStoreErr(err)
			}
		}
		return 200, taskView(t), nil
	})
}

// GetCoverage returns the sorted coverage gaps and committed cells.
func (s *Service) GetCoverage(ctx context.Context, taskID string) (int, any, error) {
	var out any
	err := s.store.WithTx(ctx, func(tx store.Tx) error {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return mapStoreErr(err)
		}
		cells, err := tx.ListObservations(ctx, taskID)
		if err != nil {
			return err
		}
		var snapshot *domain.LockSnapshot
		if t.Snapshot != nil {
			snapshot = t.Snapshot
		}
		cov := ledger.Coverage{Snapshot: snapshot, Cells: cells}
		gaps := make([]map[string]any, 0, len(cov.Gaps()))
		for _, g := range cov.Gaps() {
			gaps = append(gaps, map[string]any{"cardSeal": g[0], "dayAge": g[1]})
		}
		out = map[string]any{
			"taskId":   taskID,
			"complete": cov.Complete(),
			"gaps":     gaps,
			"cells":    cellViews(cells),
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	return 200, out, nil
}

// RevealBlindCodeRequest controls a blind-code reveal.
type RevealBlindCodeRequest struct {
	Generation int64
	CardSeal   string
}

// RevealBlindCode reveals a card's blind code after sealing.
func (s *Service) RevealBlindCode(ctx context.Context, opID, taskID string, req RevealBlindCodeRequest) (int, any, error) {
	return s.withOperation(ctx, opID, req, func(tx store.Tx) (int, any, error) {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if t.State.Terminal() {
			return 0, nil, domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{Code: "TERMINAL"})
		}
		if err := requireGeneration(t, req.Generation); err != nil {
			return 0, nil, err
		}
		if err := tx.RevealBlindCode(ctx, taskID, req.CardSeal); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		return 200, map[string]any{"taskId": taskID, "cardSeal": req.CardSeal, "revealed": true}, nil
	})
}

// VerifyEvidenceRequest appends a verified evidence version.
type VerifyEvidenceRequest struct {
	Generation int64
	Type       domain.EvidenceType
	CardSeal   string
	DayAge     int
	SlideNo    string
	Reading    string // fixed-point decimal, scale 2
	CallID     string
}

// VerifyEvidence appends a verified, immutable evidence version and advances
// the task when the phase's evidence set closes.
func (s *Service) VerifyEvidence(ctx context.Context, opID, taskID string, req VerifyEvidenceRequest) (int, any, error) {
	return s.withOperation(ctx, opID, req, func(tx store.Tx) (int, any, error) {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if t.State != domain.StateVerifyingMicroscopy && t.State != domain.StateRetestingPhysicochemical {
			return 0, nil, domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{
				Code:    "NOT_VERIFYING",
				Message: "task is not in an evidence-verification phase",
			})
		}
		if err := requireGeneration(t, req.Generation); err != nil {
			return 0, nil, err
		}
		reading, err := fixedpoint.Parse(req.Reading, 2)
		if err != nil {
			return 0, nil, domain.NewError(domain.ErrArithmeticInvalid, t.State, domain.Reason{
				Code:    "ARITHMETIC_INVALID",
				Message: "reading is not a valid fixed-point value",
			})
		}
		ev := domain.EvidenceVersion{
			Type:         req.Type,
			TaskID:       taskID,
			CardSeal:     req.CardSeal,
			DayAge:       req.DayAge,
			SlideNo:      req.SlideNo,
			Generation:   req.Generation,
			SourceCallID: req.CallID,
			Verified:     true,
			Reading:      reading,
		}
		ev.VersionNo, err = tx.NextEvidenceVersion(ctx, ev)
		if err != nil {
			return 0, nil, err
		}
		ev.PrevVersion = ev.VersionNo - 1
		if err := tx.AppendEvidence(ctx, ev); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if advanced := s.advanceByEvidence(t, tx, ctx, taskID); advanced {
			t.UpdatedAt = s.now()
			if err := tx.UpdateTask(ctx, t); err != nil {
				return 0, nil, mapStoreErr(err)
			}
		}
		return 200, evidenceView(ev), nil
	})
}

// RecheckRequest triggers a current-generation recheck.
type RecheckRequest struct {
	Generation int64
}

// CreateRecheck detects recheck triggers from the current evidence and, when
// any trigger fires, creates (or merges) the single current-generation recheck
// case and bumps the task generation so stale results cannot affect the
// re-examination.
func (s *Service) CreateRecheck(ctx context.Context, opID, taskID string, req RecheckRequest) (int, any, error) {
	return s.withOperation(ctx, opID, req, func(tx store.Tx) (int, any, error) {
		t, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return 0, nil, mapStoreErr(err)
		}
		if t.State.Terminal() {
			return 0, nil, domain.NewError(domain.ErrInvalidStateTransition, t.State, domain.Reason{
				Code:    "TERMINAL",
				Message: "task is terminal; recheck cannot be written",
			})
		}
		if err := requireGeneration(t, req.Generation); err != nil {
			return 0, nil, err
		}
		rule, ok := s.rule(t)
		if !ok {
			return 0, nil, domain.NewError(domain.ErrInvalidInput, t.State, domain.Reason{Code: "NO_RULE"})
		}
		cells, err := tx.ListObservations(ctx, taskID)
		if err != nil {
			return 0, nil, err
		}
		evidence, err := tx.ListEvidence(ctx, taskID)
		if err != nil {
			return 0, nil, err
		}
		snapshot := *t.Snapshot
		cov := ledger.Coverage{Snapshot: &snapshot, Cells: cells}
		hatchRate, _ := cov.HatchRate(2)
		deadRate, _ := cov.DeadEggRate(2)
		spore := sporeReading(evidence, req.Generation)
		triggers := arbiter.DetectTriggers(hatchRate, deadRate, rule.Thresholds.MinHatchRate, rule.Thresholds.MaxDeadEggRate, rule.Thresholds.MaxSporeCount, spore, false)
		if len(triggers) == 0 {
			return 200, map[string]any{"taskId": taskID, "triggers": []string{}, "recheck": false}, nil
		}
		// Bump the generation and freeze a single recheck for it.
		t.Generation++
		rc := arbiter.BuildRecheck(taskID, t.Generation, triggers,
			snapshot.CardSeals, snapshot.BlindCodes, snapshot.DayAges, snapshot.SlideNos)
		if err := tx.SaveRecheck(ctx, rc); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		t.UpdatedAt = s.now()
		if err := tx.UpdateTask(ctx, t); err != nil {
			return 0, nil, mapStoreErr(err)
		}
		return 200, map[string]any{
			"taskId":     taskID,
			"generation": rc.Generation,
			"triggers":   triggers,
			"recheck":    true,
		}, nil
	})
}

// advanceByEvidence advances the task state when the current phase's evidence
// set closes. It returns true when the task advanced.
func (s *Service) advanceByEvidence(t *domain.StorageTask, tx store.Tx, ctx context.Context, taskID string) bool {
	evidence, err := tx.ListEvidence(ctx, taskID)
	if err != nil {
		return false
	}
	verified := verifiedTypes(evidence, t.Generation)
	switch t.State {
	case domain.StateVerifyingMicroscopy:
		if verified[domain.EvidenceMicroscopy] && verified[domain.EvidenceEnvironment] {
			if task.Advance(t, domain.StateRetestingPhysicochemical) == nil {
				return true
			}
		}
	case domain.StateRetestingPhysicochemical:
		if verified[domain.EvidenceMoisture] && verified[domain.EvidenceDamage] {
			if task.Advance(t, domain.StatePendingIndependentReview) == nil {
				return true
			}
		}
	}
	return false
}

// verifiedTypes maps evidence type to "has a verified version in generation".
func verifiedTypes(evidence []domain.EvidenceVersion, generation int64) map[domain.EvidenceType]bool {
	out := make(map[domain.EvidenceType]bool)
	for _, e := range ledger.InGeneration(evidence, generation) {
		if e.Verified {
			out[e.Type] = true
		}
	}
	return out
}

// sporeReading returns the latest verified microscopy reading for the
// generation, interpreted as the spore count.
func sporeReading(evidence []domain.EvidenceVersion, generation int64) fixedpoint.Value {
	filtered := ledger.InGeneration(evidence, generation)
	var best *domain.EvidenceVersion
	for i := range filtered {
		e := &filtered[i]
		if e.Verified && e.Type == domain.EvidenceMicroscopy {
			if best == nil || e.VersionNo > best.VersionNo {
				best = e
			}
		}
	}
	if best == nil {
		return fixedpoint.Value{N: 0, Scale: 2}
	}
	return best.Reading
}

func evidenceView(e domain.EvidenceVersion) map[string]any {
	return map[string]any{
		"taskId":     e.TaskID,
		"type":       e.Type.String(),
		"cardSeal":   e.CardSeal,
		"dayAge":     e.DayAge,
		"slideNo":    e.SlideNo,
		"generation": e.Generation,
		"versionNo":  e.VersionNo,
		"reading":    e.Reading.String(),
		"verified":   e.Verified,
	}
}

func cellViews(cells []domain.ObservationCell) []map[string]any {
	out := make([]map[string]any, 0, len(cells))
	for _, c := range cells {
		out = append(out, map[string]any{
			"cardSeal":  c.CardSeal,
			"dayAge":    c.DayAge,
			"hatched":   c.Hatched,
			"unhatched": c.Unhatched,
			"dead":      c.Dead,
			"damaged":   c.Damaged,
			"rechecked": c.Rechecked,
		})
	}
	return out
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func containsInt(list []int, v int) bool {
	for _, n := range list {
		if n == v {
			return true
		}
	}
	return false
}
