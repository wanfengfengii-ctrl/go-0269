package api

import (
	"net/http"
	"strings"

	"silkworm-egg-cold-storage-gate/internal/app"
	"silkworm-egg-cold-storage-gate/internal/domain"
)

// handleTasks handles POST /v1/tasks (create).
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	var req struct {
		TaskID        string `json:"taskId"`
		LineageCode   string `json:"lineageCode"`
		BatchCode     string `json:"batchCode"`
		MothBagDigest string `json:"mothBagDigest"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	status, body, err := s.svc.CreateTask(r.Context(), opID(r), app.CreateTaskRequest{
		TaskID:        req.TaskID,
		LineageCode:   req.LineageCode,
		BatchCode:     req.BatchCode,
		MothBagDigest: req.MothBagDigest,
	})
	respond(w, status, body, err)
}

// handleTaskByID dispatches the sub-resource endpoints under /v1/tasks/{id}.
func (s *Server) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
	if rest == "" {
		writeError(w, &domain.DomainError{Code: domain.ErrNotFound, State: domain.StatePendingLock})
		return
	}
	seg := strings.Split(rest, "/")
	id := seg[0]
	sub := seg[1:]

	switch {
	case len(sub) == 0:
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
			return
		}
		status, body, err := s.svc.GetTask(r.Context(), id)
		respond(w, status, body, err)

	case len(sub) == 1 && sub[0] == "lock" && r.Method == http.MethodPost:
		s.handleLock(w, r, id)
	case len(sub) == 1 && sub[0] == "production-confirmations" && r.Method == http.MethodPost:
		s.handleConfirm(w, r, id)
	case len(sub) == 1 && sub[0] == "observations" && r.Method == http.MethodPost:
		s.handleObservations(w, r, id)
	case len(sub) == 1 && sub[0] == "coverage" && r.Method == http.MethodGet:
		status, body, err := s.svc.GetCoverage(r.Context(), id)
		respond(w, status, body, err)
	case len(sub) == 1 && sub[0] == "instrument-calls" && r.Method == http.MethodPost:
		s.handleStartInstrumentCall(w, r, id)
	case len(sub) == 1 && sub[0] == "rechecks" && r.Method == http.MethodPost:
		s.handleRecheck(w, r, id)
	case len(sub) == 1 && sub[0] == "reviews" && r.Method == http.MethodPost:
		s.handleReview(w, r, id)

	case len(sub) == 2 && sub[0] == "samples" && sub[1] == "seal" && r.Method == http.MethodPost:
		s.handleSeal(w, r, id)
	case len(sub) == 2 && sub[0] == "leases" && r.Method == http.MethodPost:
		s.handleLease(w, r, id, sub[1])
	case len(sub) == 2 && sub[0] == "blind-codes" && sub[1] == "reveal" && r.Method == http.MethodPost:
		s.handleReveal(w, r, id)
	case len(sub) == 2 && sub[0] == "evidence" && sub[1] == "verify" && r.Method == http.MethodPost:
		s.handleVerifyEvidence(w, r, id)
	case len(sub) == 2 && sub[0] == "decisions" && r.Method == http.MethodPost:
		s.handleDecision(w, r, id, sub[1])
	default:
		writeError(w, &domain.DomainError{Code: domain.ErrNotFound, State: domain.StatePendingLock})
	}
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		RuleVersion int64    `json:"ruleVersion"`
		Generation  int64    `json:"generation"`
		CardSeals   []string `json:"cardSeals"`
		BlindCodes  []string `json:"blindCodes"`
		DayAges     []int    `json:"dayAges"`
		SlideNos    []string `json:"slideNos"`
		Reviewers   []string `json:"reviewers"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	if len(req.CardSeals) > maxArrayLen || len(req.BlindCodes) > maxArrayLen {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	status, body, err := s.svc.LockTask(r.Context(), opID(r), id, app.LockTaskRequest{
		RuleVersion: req.RuleVersion,
		Generation:  req.Generation,
		CardSeals:   req.CardSeals,
		BlindCodes:  req.BlindCodes,
		DayAges:     req.DayAges,
		SlideNos:    req.SlideNos,
		Reviewers:   req.Reviewers,
	})
	respond(w, status, body, err)
}

func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Generation int64  `json:"generation"`
		PersonID   string `json:"personId"`
		Conclusion string `json:"conclusion"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	status, body, err := s.svc.ConfirmProduction(r.Context(), opID(r), id, app.ConfirmProductionRequest{
		Generation: req.Generation,
		PersonID:   req.PersonID,
		Conclusion: req.Conclusion,
	})
	respond(w, status, body, err)
}

func (s *Server) handleSeal(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Generation int64 `json:"generation"`
		Samples    []struct {
			CardSeal   string `json:"cardSeal"`
			SampleNo   int    `json:"sampleNo"`
			Triplet    int    `json:"triplet"`
			SealDigest string `json:"sealDigest"`
			BlindCode  string `json:"blindCode"`
		} `json:"samples"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	if len(req.Samples) > maxArrayLen {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	samples := make([]app.SealSample, 0, len(req.Samples))
	for _, sm := range req.Samples {
		samples = append(samples, app.SealSample{
			CardSeal:   sm.CardSeal,
			SampleNo:   sm.SampleNo,
			Triplet:    sm.Triplet,
			SealDigest: sm.SealDigest,
			BlindCode:  sm.BlindCode,
		})
	}
	status, body, err := s.svc.SealSamples(r.Context(), opID(r), id, app.SealSamplesRequest{
		Generation: req.Generation,
		Samples:    samples,
	})
	respond(w, status, body, err)
}

func (s *Server) handleLease(w http.ResponseWriter, r *http.Request, id, op string) {
	var req struct {
		Generation   int64  `json:"generation"`
		ResourceType string `json:"resourceType"`
		ResourceNo   string `json:"resourceNo"`
		ToResourceNo string `json:"toResourceNo"`
		ExpiresAt    int64  `json:"expiresAt"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	rt, ok := domain.ParseResourceType(req.ResourceType)
	if !ok {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	lreq := app.LeaseRequest{
		Generation:   req.Generation,
		ResourceType: rt,
		ResourceNo:   req.ResourceNo,
		ToResourceNo: req.ToResourceNo,
		ExpiresAt:    req.ExpiresAt,
	}
	var status int
	var body any
	var err error
	ctx := r.Context()
	switch op {
	case "acquire":
		status, body, err = s.svc.AcquireLease(ctx, opID(r), id, lreq)
	case "move":
		status, body, err = s.svc.MoveLease(ctx, opID(r), id, lreq)
	case "renew":
		status, body, err = s.svc.RenewLease(ctx, opID(r), id, lreq)
	default:
		writeError(w, &domain.DomainError{Code: domain.ErrNotFound, State: domain.StatePendingLock})
		return
	}
	respond(w, status, body, err)
}

func (s *Server) handleObservations(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Generation int64 `json:"generation"`
		Cells      []struct {
			CardSeal  string `json:"cardSeal"`
			DayAge    int    `json:"dayAge"`
			Hatched   int64  `json:"hatched"`
			Unhatched int64  `json:"unhatched"`
			Dead      int64  `json:"dead"`
			Damaged   int64  `json:"damaged"`
			Rechecked bool   `json:"rechecked"`
		} `json:"cells"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	if len(req.Cells) > maxArrayLen {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	cells := make([]domain.ObservationCell, 0, len(req.Cells))
	for _, c := range req.Cells {
		cells = append(cells, domain.ObservationCell{
			CardSeal:  c.CardSeal,
			DayAge:    c.DayAge,
			Hatched:   c.Hatched,
			Unhatched: c.Unhatched,
			Dead:      c.Dead,
			Damaged:   c.Damaged,
			Rechecked: c.Rechecked,
		})
	}
	status, body, err := s.svc.SubmitObservations(r.Context(), opID(r), id, app.ObservationRequest{
		Generation: req.Generation,
		Cells:      cells,
	})
	respond(w, status, body, err)
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Generation int64  `json:"generation"`
		CardSeal   string `json:"cardSeal"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	status, body, err := s.svc.RevealBlindCode(r.Context(), opID(r), id, app.RevealBlindCodeRequest{
		Generation: req.Generation,
		CardSeal:   req.CardSeal,
	})
	respond(w, status, body, err)
}

func (s *Server) handleVerifyEvidence(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Generation int64  `json:"generation"`
		Type       string `json:"type"`
		CardSeal   string `json:"cardSeal"`
		DayAge     int    `json:"dayAge"`
		SlideNo    string `json:"slideNo"`
		Reading    string `json:"reading"`
		CallID     string `json:"callId"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	et, ok := domain.ParseEvidenceType(req.Type)
	if !ok {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	status, body, err := s.svc.VerifyEvidence(r.Context(), opID(r), id, app.VerifyEvidenceRequest{
		Generation: req.Generation,
		Type:       et,
		CardSeal:   req.CardSeal,
		DayAge:     req.DayAge,
		SlideNo:    req.SlideNo,
		Reading:    req.Reading,
		CallID:     req.CallID,
	})
	respond(w, status, body, err)
}

func (s *Server) handleRecheck(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Generation int64 `json:"generation"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	status, body, err := s.svc.CreateRecheck(r.Context(), opID(r), id, app.RecheckRequest{Generation: req.Generation})
	respond(w, status, body, err)
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Generation int64  `json:"generation"`
		PersonID   string `json:"personId"`
		Conclusion string `json:"conclusion"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	status, body, err := s.svc.SubmitReview(r.Context(), opID(r), id, app.ReviewRequest{
		Generation: req.Generation,
		PersonID:   req.PersonID,
		Conclusion: req.Conclusion,
	})
	respond(w, status, body, err)
}

func (s *Server) handleDecision(w http.ResponseWriter, r *http.Request, id, decision string) {
	var req struct {
		Generation int64  `json:"generation"`
		DecidedBy  string `json:"decidedBy"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	typ, ok := domain.ParseFinalDecisionType(decision)
	if !ok {
		writeError(w, &domain.DomainError{Code: domain.ErrNotFound, State: domain.StatePendingLock})
		return
	}
	status, body, err := s.svc.Decide(r.Context(), opID(r), id, typ, app.DecideRequest{
		Generation: req.Generation,
		DecidedBy:  req.DecidedBy,
	})
	respond(w, status, body, err)
}

// handleRetryRoute handles POST /v1/instrument-calls/{callID}/retry.
func (s *Server) handleRetryRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/instrument-calls/")
	seg := strings.Split(rest, "/")
	if len(seg) == 2 && seg[1] == "retry" && r.Method == http.MethodPost {
		s.handleRetry(w, r, seg[0])
		return
	}
	writeError(w, &domain.DomainError{Code: domain.ErrNotFound, State: domain.StatePendingLock})
}

func (s *Server) handleStartInstrumentCall(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Generation     int64  `json:"generation"`
		InstrumentType string `json:"instrumentType"`
		TargetObject   string `json:"targetObject"`
		RequestDigest  string `json:"requestDigest"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	it, ok := domain.ParseInstrumentType(req.InstrumentType)
	if !ok {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	status, body, err := s.svc.StartInstrumentCall(r.Context(), opID(r), id, app.StartInstrumentCallRequest{
		Generation:     req.Generation,
		InstrumentType: it,
		TargetObject:   req.TargetObject,
		RequestDigest:  req.RequestDigest,
	})
	respond(w, status, body, err)
}

func (s *Server) handleRetry(w http.ResponseWriter, r *http.Request, callID string) {
	var req struct {
		Generation int64 `json:"generation"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock})
		return
	}
	status, body, err := s.svc.RetryInstrumentCall(r.Context(), opID(r), callID, app.RetryRequest{Generation: req.Generation})
	respond(w, status, body, err)
}

// respond writes a successful app response or a unified error.
func respond(w http.ResponseWriter, status int, body any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, status, body)
}
