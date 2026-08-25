package app

import (
	"context"
	"path/filepath"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/catalog"
	"silkworm-egg-cold-storage-gate/internal/domain"
	"silkworm-egg-cold-storage-gate/internal/store"
)

// testClock is a manually advanced logical clock for deterministic tests.
type testClock struct{ now int64 }

func (c *testClock) get() int64      { return c.now }
func (c *testClock) advance(n int64) { c.now += n }

func newTestService(t *testing.T, runner Runner, clock *testClock) *Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "benzhi-test.db")
	return newTestServiceDB(t, dbPath, runner, clock)
}

func newTestServiceDB(t *testing.T, dbPath string, runner Runner, clock *testClock) *Service {
	t.Helper()
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if clock == nil {
		clock = &testClock{now: 1000}
	}
	dir := catalog.NewDirectory(catalog.SeedRules())
	return NewService(st, dir, runner, clock.get)
}

// lockManifest returns a valid lock request against the seed catalog lineage.
func lockManifest() LockTaskRequest {
	return LockTaskRequest{
		RuleVersion: 1,
		Generation:  1,
		CardSeals:   []string{"seal-1", "seal-2"},
		BlindCodes:  []string{"blind-1", "blind-2"},
		DayAges:     []int{1, 2},
		SlideNos:    []string{"slide-1", "slide-2"},
		Reviewers:   []string{"person-c", "person-d"},
	}
}

// createAndLock creates a task and locks it, returning the task ID.
func createAndLock(t *testing.T, svc *Service) string {
	t.Helper()
	ctx := context.Background()
	id := "task-" + t.Name()
	_, _, err := svc.CreateTask(ctx, "op-create-"+id, CreateTaskRequest{
		TaskID:        id,
		LineageCode:   "lineage-001",
		BatchCode:     "batch-001",
		MothBagDigest: "digest-1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, _, err = svc.LockTask(ctx, "op-lock-"+id, id, lockManifest())
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	return id
}

// driveToObservation advances a task through confirmation, sealing, and lease
// acquisition so it is ready to receive observations.
func driveToObservation(t *testing.T, svc *Service, id string) {
	t.Helper()
	ctx := context.Background()
	for _, p := range []string{"person-a", "person-b"} {
		_, _, err := svc.ConfirmProduction(ctx, "op-conf-"+id+"-"+p, id, ConfirmProductionRequest{
			Generation: 1, PersonID: p, Conclusion: "PASS",
		})
		if err != nil {
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
	_, _, err = svc.AcquireLease(ctx, "op-lease-cabin-"+id, id, LeaseRequest{
		Generation: 1, ResourceType: domain.ResourceIncubationCabin, ResourceNo: "cabin-1", ExpiresAt: 10000,
	})
	if err != nil {
		t.Fatalf("acquire cabin: %v", err)
	}
	_, _, err = svc.AcquireLease(ctx, "op-lease-slide-"+id, id, LeaseRequest{
		Generation: 1, ResourceType: domain.ResourceSlide, ResourceNo: "slide-1", ExpiresAt: 10000,
	})
	if err != nil {
		t.Fatalf("acquire slide: %v", err)
	}
}
