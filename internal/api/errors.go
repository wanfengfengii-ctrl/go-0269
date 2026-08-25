package api

import (
	"encoding/json"
	"log"
	"net/http"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

// statusFor maps a stable error code to an HTTP status code.
func statusFor(code domain.ErrorCode) int {
	switch code {
	case domain.ErrInvalidInput:
		return http.StatusUnprocessableEntity
	case domain.ErrNotFound:
		return http.StatusNotFound
	case domain.ErrDuplicateResource:
		return http.StatusConflict
	case domain.ErrStaleGeneration:
		return http.StatusConflict
	case domain.ErrInvalidStateTransition:
		return http.StatusConflict
	case domain.ErrOperationConflict:
		return http.StatusConflict
	case domain.ErrConcurrentModification:
		return http.StatusConflict
	case domain.ErrArithmeticInvalid:
		return http.StatusUnprocessableEntity
	case domain.ErrFinalDecisionConflict:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// writeError renders the unified error object; internal (non-domain) errors
// are logged with their detail while only a stable INTERNAL_ERROR is returned.
func writeError(w http.ResponseWriter, err error) {
	var de *domain.DomainError
	if e, ok := err.(*domain.DomainError); ok {
		de = e
	} else {
		log.Printf("internal error: %v", err)
		de = &domain.DomainError{Code: domain.ErrInternal, State: domain.StatePendingLock}
	}
	writeJSON(w, statusFor(de.Code), de)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
