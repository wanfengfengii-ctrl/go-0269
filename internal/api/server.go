// Package api exposes the JSON HTTP interface for the silkworm egg
// cold-storage gate. It limits request size, rejects unknown fields, requires
// an Operation-Id header on writes, and maps every application command to a
// stable JSON response or a unified error object.
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"silkworm-egg-cold-storage-gate/internal/app"
	"silkworm-egg-cold-storage-gate/internal/domain"
)

const (
	maxBodyBytes = 1 << 20 // 1 MiB request body limit
	maxArrayLen  = 4096
)

// Server wires the application service into an HTTP handler.
type Server struct {
	svc *app.Service
}

// NewServer builds a Server over an application service.
func NewServer(svc *app.Service) *Server {
	return &Server{svc: svc}
}

// Handler returns the fully routed handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/v1/tasks", s.handleTasks)
	mux.HandleFunc("/v1/tasks/", s.handleTaskByID)
	mux.HandleFunc("/v1/instrument-calls/", s.handleRetryRoute)
	return withLimits(withOperationID(mux))
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	if !s.svc.Store().Ready() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// opID extracts the required Operation-Id header (the middleware guarantees
// its presence on writes).
func opID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("Operation-Id"))
}

// decodeJSON rejects unknown fields and enforces the body size limit.
func decodeJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	// Reject a second JSON value in the body.
	if dec.More() {
		return &json.SyntaxError{}
	}
	return nil
}

// withOperationID enforces the Operation-Id header on write methods.
func withOperationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			if strings.TrimSpace(r.Header.Get("Operation-Id")) == "" {
				writeError(w, &domain.DomainError{Code: domain.ErrInvalidInput, State: domain.StatePendingLock,
					Reasons: []domain.Reason{{Code: "MISSING_OPERATION_ID", Message: "Operation-Id header is required"}}})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// withLimits caps the request body size at the outer edge.
func withLimits(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}
