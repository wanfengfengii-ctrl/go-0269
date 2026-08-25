package app

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

// driveToOccupying advances a task to cabin occupation (post-sealing).
func driveToOccupying(t *testing.T, svc *Service, id string) {
	t.Helper()
	ctx := context.Background()
	for _, p := range []string{"person-a", "person-b"} {
		if _, _, err := svc.ConfirmProduction(ctx, "op-conf-"+id+"-"+p, id, ConfirmProductionRequest{Generation: 1, PersonID: p, Conclusion: "PASS"}); err != nil {
			t.Fatalf("confirm %s: %v", p, err)
		}
	}
	_, _, err := svc.SealSamples(ctx, "op-seal-"+id, id, SealSamplesRequest{
		Generation: 1,
		Samples: []SealSample{
			{CardSeal: "seal-1", SampleNo: 1, Triplet: 1, SealDigest: "d1", BlindCode: "blind-1"},
			{CardSeal: "seal-2", SampleNo: 2, Triplet: 2, SealDigest: "d2", BlindCode: "blind-2"},
		},
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
}

func TestConcurrentLeaseCompetitionSingleWinner(t *testing.T) {
	svc := newTestService(t, nil, nil)
	a := "task-a"
	b := "task-b"
	for _, id := range []string{a, b} {
		if _, _, err := svc.CreateTask(context.Background(), "op-create-"+id, CreateTaskRequest{
			TaskID: id, LineageCode: "lineage-001", BatchCode: "batch-001", MothBagDigest: "d",
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		if _, _, err := svc.LockTask(context.Background(), "op-lock-"+id, id, lockManifest()); err != nil {
			t.Fatalf("lock %s: %v", id, err)
		}
		driveToOccupying(t, svc, id)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i, id := range []string{a, b} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			<-start
			_, _, err := svc.AcquireLease(context.Background(), "op-race-"+id, id, LeaseRequest{
				Generation: 1, ResourceType: domain.ResourceIncubationCabin, ResourceNo: "cabin-shared", ExpiresAt: 10000,
			})
			results[i] = err
		}(i, id)
	}
	close(start)
	wg.Wait()

	winners, losers := 0, 0
	for _, err := range results {
		if err == nil {
			winners++
		} else {
			losers++
			de, ok := err.(*domain.DomainError)
			if !ok || de.Code != domain.ErrDuplicateResource {
				t.Fatalf("loser error = %v, want DUPLICATE_RESOURCE", err)
			}
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("winners=%d losers=%d, want exactly one of each", winners, losers)
	}
}

func TestRestartValidLeaseStillBlocksAcquisition(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "restart.db")
	svc := newTestServiceDB(t, dbPath, nil, nil)
	ctx := context.Background()
	id := "task-owner"
	if _, _, err := svc.CreateTask(ctx, "op-create", CreateTaskRequest{TaskID: id, LineageCode: "lineage-001", BatchCode: "batch-001"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := svc.LockTask(ctx, "op-lock", id, lockManifest()); err != nil {
		t.Fatalf("lock: %v", err)
	}
	driveToOccupying(t, svc, id)
	if _, _, err := svc.AcquireLease(ctx, "op-acquire", id, LeaseRequest{Generation: 1, ResourceType: domain.ResourceIncubationCabin, ResourceNo: "cabin-1", ExpiresAt: 10000}); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// Simulate a restart: a fresh service over the same database file.
	svc2 := newTestServiceDB(t, dbPath, nil, nil)
	id2 := "task-intruder"
	if _, _, err := svc2.CreateTask(ctx, "op-create-2", CreateTaskRequest{TaskID: id2, LineageCode: "lineage-001", BatchCode: "batch-001"}); err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if _, _, err := svc2.LockTask(ctx, "op-lock-2", id2, lockManifest()); err != nil {
		t.Fatalf("lock 2: %v", err)
	}
	driveToOccupying(t, svc2, id2)
	_, _, err := svc2.AcquireLease(ctx, "op-acquire-2", id2, LeaseRequest{Generation: 1, ResourceType: domain.ResourceIncubationCabin, ResourceNo: "cabin-1", ExpiresAt: 10000})
	if err == nil {
		t.Fatal("expected the persisted lease to block acquisition after restart")
	}
	de, ok := err.(*domain.DomainError)
	if !ok || de.Code != domain.ErrDuplicateResource {
		t.Fatalf("error = %v, want DUPLICATE_RESOURCE", err)
	}
}
