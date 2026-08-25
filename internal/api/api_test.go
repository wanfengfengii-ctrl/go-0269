package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/app"
	"silkworm-egg-cold-storage-gate/internal/catalog"
	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/store"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc := app.NewService(st, catalog.NewDirectory(catalog.SeedRules()), nil, nil)
	return NewServer(svc)
}

func doRequest(t *testing.T, s *Server, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	rec := doRequest(t, testServer(t), http.MethodGet, "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("healthz body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("healthz status = %q", body["status"])
	}
}

func TestReadyz(t *testing.T) {
	rec := doRequest(t, testServer(t), http.MethodGet, "/readyz", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz status = %d, want 200", rec.Code)
	}
}

func TestCreateTaskRequiresOperationID(t *testing.T) {
	rec := doRequest(t, testServer(t), http.MethodPost, "/v1/tasks",
		`{"taskId":"t1","lineageCode":"lineage-001","batchCode":"batch-001"}`, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var de domain.DomainError
	if err := json.Unmarshal(rec.Body.Bytes(), &de); err != nil {
		t.Fatalf("error body: %v", err)
	}
	if de.Code != domain.ErrInvalidInput {
		t.Fatalf("error code = %q", de.Code)
	}
}

func TestCreateAndLockEndToEnd(t *testing.T) {
	s := testServer(t)
	hdr := map[string]string{"Operation-Id": "op-create-1"}
	rec := doRequest(t, s, http.MethodPost, "/v1/tasks",
		`{"taskId":"t1","lineageCode":"lineage-001","batchCode":"batch-001","mothBagDigest":"d1"}`, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", rec.Code, rec.Body.String())
	}

	lock := `{"ruleVersion":1,"generation":1,"cardSeals":["s1","s2"],"blindCodes":["b1","b2"],"dayAges":[1,2],"slideNos":["sl1"],"reviewers":["c","d"]}`
	rec = doRequest(t, s, http.MethodPost, "/v1/tasks/t1/lock", lock, map[string]string{"Operation-Id": "op-lock-1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("lock status = %d, body %s", rec.Code, rec.Body.String())
	}
	var v map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("lock body: %v", err)
	}
	if v["state"] != "PENDING_PRODUCTION_CONFIRMATION" {
		t.Fatalf("state after lock = %v", v["state"])
	}
	if v["locked"] != true {
		t.Fatalf("locked flag = %v", v["locked"])
	}
}

func TestLockRejectsUnknownField(t *testing.T) {
	s := testServer(t)
	doRequest(t, s, http.MethodPost, "/v1/tasks",
		`{"taskId":"t1","lineageCode":"lineage-001","batchCode":"batch-001"}`, map[string]string{"Operation-Id": "op-c"})
	lock := `{"ruleVersion":1,"generation":1,"cardSeals":["s1"],"blindCodes":["b1"],"dayAges":[1],"reviewers":["c","d"],"bogusField":true}`
	rec := doRequest(t, s, http.MethodPost, "/v1/tasks/t1/lock", lock, map[string]string{"Operation-Id": "op-l"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown field status = %d, want 422", rec.Code)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	rec := doRequest(t, testServer(t), http.MethodGet, "/v1/tasks/nope", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUnifiedErrorObjectShape(t *testing.T) {
	s := testServer(t)
	rec := doRequest(t, s, http.MethodPost, "/v1/tasks",
		`{"taskId":"t1","lineageCode":"lineage-001","batchCode":"batch-001"}`, map[string]string{"Operation-Id": "op-c"})
	// Duplicate task ID returns a unified error object.
	rec = doRequest(t, s, http.MethodPost, "/v1/tasks",
		`{"taskId":"t1","lineageCode":"lineage-001","batchCode":"batch-001"}`, map[string]string{"Operation-Id": "op-c2"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want 409", rec.Code)
	}
	var de domain.DomainError
	if err := json.Unmarshal(rec.Body.Bytes(), &de); err != nil {
		t.Fatalf("error body: %v", err)
	}
	if de.Code != domain.ErrDuplicateResource {
		t.Fatalf("error code = %q, want DUPLICATE_RESOURCE", de.Code)
	}
	if de.Reasons == nil || len(de.Reasons) == 0 {
		t.Fatalf("reasons array missing: %+v", de)
	}
}
