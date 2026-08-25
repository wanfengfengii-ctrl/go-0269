package app

import (
	"context"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

func TestOperationIdempotentSameContent(t *testing.T) {
	svc := newTestService(t, nil, nil)
	ctx := context.Background()
	req := CreateTaskRequest{TaskID: "t1", LineageCode: "lineage-001", BatchCode: "batch-001", MothBagDigest: "d1"}
	status1, body1, err := svc.CreateTask(ctx, "op-1", req)
	if err != nil || status1 != 201 {
		t.Fatalf("first create = %d, %v", status1, err)
	}
	status2, body2, err := svc.CreateTask(ctx, "op-1", req)
	if err != nil || status2 != 201 {
		t.Fatalf("replayed create = %d, %v", status2, err)
	}
	m1 := body1.(map[string]any)
	m2 := body2.(map[string]any)
	if m1["id"] != m2["id"] || m1["state"] != m2["state"] {
		t.Fatalf("idempotent bodies differ: %v vs %v", m1, m2)
	}
}

func TestOperationConflictDifferentContent(t *testing.T) {
	svc := newTestService(t, nil, nil)
	ctx := context.Background()
	_, _, err := svc.CreateTask(ctx, "op-1", CreateTaskRequest{TaskID: "t1", LineageCode: "lineage-001", BatchCode: "batch-001"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, _, err = svc.CreateTask(ctx, "op-1", CreateTaskRequest{TaskID: "t2", LineageCode: "lineage-001", BatchCode: "batch-001"})
	if err == nil {
		t.Fatal("expected operation conflict")
	}
	de, ok := err.(*domain.DomainError)
	if !ok || de.Code != domain.ErrOperationConflict {
		t.Fatalf("error = %v, want OPERATION_CONFLICT", err)
	}
}

func TestStaleGenerationRejected(t *testing.T) {
	svc := newTestService(t, nil, nil)
	id := createAndLock(t, svc)
	ctx := context.Background()
	_, _, err := svc.ConfirmProduction(ctx, "op-conf", id, ConfirmProductionRequest{
		Generation: 99, PersonID: "person-a", Conclusion: "PASS",
	})
	if err == nil {
		t.Fatal("expected stale generation error")
	}
	de, ok := err.(*domain.DomainError)
	if !ok || de.Code != domain.ErrStaleGeneration {
		t.Fatalf("error = %v, want STALE_GENERATION", err)
	}
}

func TestUnqualifiedProducerRejected(t *testing.T) {
	svc := newTestService(t, nil, nil)
	id := createAndLock(t, svc)
	ctx := context.Background()
	_, _, err := svc.ConfirmProduction(ctx, "op-conf", id, ConfirmProductionRequest{
		Generation: 1, PersonID: "person-c", Conclusion: "PASS", // person-c is review-qualified only
	})
	if err == nil {
		t.Fatal("expected unqualified producer error")
	}
	de, ok := err.(*domain.DomainError)
	if !ok || de.Code != domain.ErrInvalidInput {
		t.Fatalf("error = %v, want INVALID_INPUT", err)
	}
}
