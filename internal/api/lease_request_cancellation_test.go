package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/store"
)

func TestModel_LeaseCancellationBeforeTransactionHasNoEffects(t *testing.T) {
	type leaseState struct {
		state     domain.LeaseState
		expiresAt int64
	}
	tests := []struct {
		name         string
		operation    string
		payload      string
		seedResource string
		wantCanceled map[string]leaseState
		wantSuccess  map[string]leaseState
	}{
		{
			name:         "acquire",
			operation:    "op-acquire",
			payload:      `{"generation":1,"resourceType":"INCUBATION_CABIN","resourceNo":"cabin-new","expiresAt":7000}`,
			wantCanceled: map[string]leaseState{},
			wantSuccess: map[string]leaseState{
				"cabin-new": {state: domain.LeaseActive, expiresAt: 7000},
			},
		},
		{
			name:         "move",
			operation:    "op-move",
			payload:      `{"generation":1,"resourceType":"INCUBATION_CABIN","resourceNo":"cabin-old","toResourceNo":"cabin-new","expiresAt":8000}`,
			seedResource: "cabin-old",
			wantCanceled: map[string]leaseState{
				"cabin-old": {state: domain.LeaseActive, expiresAt: 2000},
			},
			wantSuccess: map[string]leaseState{
				"cabin-old": {state: domain.LeaseReleased, expiresAt: 2000},
				"cabin-new": {state: domain.LeaseActive, expiresAt: 8000},
			},
		},
		{
			name:         "renew",
			operation:    "op-renew",
			payload:      `{"generation":1,"resourceType":"INCUBATION_CABIN","resourceNo":"cabin-old","expiresAt":9000}`,
			seedResource: "cabin-old",
			wantCanceled: map[string]leaseState{
				"cabin-old": {state: domain.LeaseActive, expiresAt: 2000},
			},
			wantSuccess: map[string]leaseState{
				"cabin-old": {state: domain.LeaseActive, expiresAt: 9000},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer(t)
			taskID := "task-" + tc.name

			send := func(ctx context.Context, path, operation, body string) *httptest.ResponseRecorder {
				t.Helper()
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Operation-Id", operation)
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, req)
				return rec
			}

			create := `{"taskId":"` + taskID + `","lineageCode":"lineage-001","batchCode":"batch-001"}`
			if rec := send(context.Background(), "/v1/tasks", "op-create-"+tc.name, create); rec.Code != http.StatusCreated {
				t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
			}
			lock := `{"ruleVersion":1,"generation":1,"cardSeals":["seal-1","seal-2"],"blindCodes":["blind-1","blind-2"],"dayAges":[1,2],"slideNos":["slide-1"],"reviewers":["person-c","person-d"]}`
			if rec := send(context.Background(), "/v1/tasks/"+taskID+"/lock", "op-lock-"+tc.name, lock); rec.Code != http.StatusOK {
				t.Fatalf("lock status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if tc.seedResource != "" {
				seed := `{"generation":1,"resourceType":"INCUBATION_CABIN","resourceNo":"` + tc.seedResource + `","expiresAt":2000}`
				if rec := send(context.Background(), "/v1/tasks/"+taskID+"/leases/acquire", "op-seed-"+tc.name, seed); rec.Code != http.StatusOK {
					t.Fatalf("seed lease status = %d, body = %s", rec.Code, rec.Body.String())
				}
			}

			readState := func(operation string) (map[string]leaseState, bool) {
				t.Helper()
				got := make(map[string]leaseState)
				operationRecorded := false
				err := s.svc.Store().WithTx(context.Background(), func(tx store.Tx) error {
					leases, err := tx.ListLeases(context.Background(), taskID)
					if err != nil {
						return err
					}
					for _, lease := range leases {
						got[lease.ResourceNo] = leaseState{state: lease.State, expiresAt: lease.ExpiresAt}
					}
					record, err := tx.GetOperation(context.Background(), operation)
					if err != nil {
						return err
					}
					operationRecorded = record != nil
					return nil
				})
				if err != nil {
					t.Fatalf("read persisted state: %v", err)
				}
				return got, operationRecorded
			}
			assertState := func(stage string, want map[string]leaseState, wantOperation bool) {
				t.Helper()
				got, operationRecorded := readState(tc.operation)
				if len(got) != len(want) {
					t.Fatalf("%s leases = %#v, want %#v", stage, got, want)
				}
				for resource, wantLease := range want {
					if gotLease, ok := got[resource]; !ok || gotLease != wantLease {
						t.Fatalf("%s lease %q = %#v (present %v), want %#v", stage, resource, gotLease, ok, wantLease)
					}
				}
				if operationRecorded != wantOperation {
					t.Fatalf("%s operation recorded = %v, want %v", stage, operationRecorded, wantOperation)
				}
			}

			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			send(canceled, "/v1/tasks/"+taskID+"/leases/"+tc.name, tc.operation, tc.payload)
			assertState("after canceled request", tc.wantCanceled, false)

			path := "/v1/tasks/" + taskID + "/leases/" + tc.name
			first := send(context.Background(), path, tc.operation, tc.payload)
			if first.Code != http.StatusOK {
				t.Fatalf("active request status = %d, body = %s", first.Code, first.Body.String())
			}
			second := send(context.Background(), path, tc.operation, tc.payload)
			if second.Code != first.Code || second.Body.String() != first.Body.String() {
				t.Fatalf("idempotent replay = (%d, %q), want (%d, %q)", second.Code, second.Body.String(), first.Code, first.Body.String())
			}
			assertState("after active request and replay", tc.wantSuccess, true)
		})
	}
}
