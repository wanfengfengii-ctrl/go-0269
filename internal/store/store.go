// Package store defines the persistence boundary for the silkworm egg
// cold-storage gate. The production implementation uses SQLite in WAL mode
// with unique indexes, optimistic versioning, and a fixed busy-retry policy;
// every application command runs inside a single transaction so a failed
// command leaves no partial samples, leases, coverage cells, blind-code
// reveals, evidence, or decisions behind.
package store

import (
	"context"
	"errors"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

// Sentinel errors mapped to stable business error codes by the API layer.
var (
	// ErrDuplicate maps a unique-constraint violation (batch, seal, blind code,
	// cabin, cell, slide, operation ID, or final decision).
	ErrDuplicate = errors.New("store: duplicate resource")
	// ErrConcurrentVersion maps an optimistic version conflict.
	ErrConcurrentVersion = errors.New("store: concurrent modification")
	// ErrNotFound maps a missing aggregate or record.
	ErrNotFound = errors.New("store: not found")
)

// Store is the persistence boundary consumed exclusively by the application
// service. Every mutation is performed inside a transaction provided by
// WithTx so that a command is atomic end to end.
type Store interface {
	// Migrate applies the schema and runs the deterministic recovery scan.
	Migrate(ctx context.Context) error
	// Ready reports whether migration and recovery have completed.
	Ready() bool
	// Close releases the underlying database.
	Close() error
	// WithTx runs fn inside a single database transaction, committing on a
	// nil error and rolling back otherwise.
	WithTx(ctx context.Context, fn func(Tx) error) error
}

// Tx is the transactional view of the store handed to an application command.
// All methods read and write within the enclosing transaction.
type Tx interface {
	// GetTask loads a task aggregate by ID.
	GetTask(ctx context.Context, id string) (*domain.StorageTask, error)
	// CreateTask inserts a pending-lock task.
	CreateTask(ctx context.Context, t *domain.StorageTask) error
	// UpdateTask persists a task mutation with optimistic version checking.
	UpdateTask(ctx context.Context, t *domain.StorageTask) error

	// GetOperation returns an idempotency record, or nil when absent.
	GetOperation(ctx context.Context, opID string) (*domain.OperationRecord, error)
	// RecordOperation persists an idempotency record and its response.
	RecordOperation(ctx context.Context, opID, contentHash string, response []byte) error

	// SaveSampleSeals persists a set of egg-card sample seals.
	SaveSampleSeals(ctx context.Context, seals []domain.SampleSeal) error
	// ListSampleSeals returns the sample seals of a task in seal order.
	ListSampleSeals(ctx context.Context, taskID string) ([]domain.SampleSeal, error)
	// RevealBlindCode marks a card's blind code revealed.
	RevealBlindCode(ctx context.Context, taskID, cardSeal string) error

	// AcquireLease inserts a lease; the conditional unique index rejects a
	// second active lease on the same resource.
	AcquireLease(ctx context.Context, l domain.ResourceLease) error
	// ReleaseLease marks a lease released.
	ReleaseLease(ctx context.Context, taskID string, rt domain.ResourceType, no string, reason string) error
	// RenewLease extends a lease's expiry logical time.
	RenewLease(ctx context.Context, taskID string, rt domain.ResourceType, no string, newExpiresAt int64) error
	// ListLeases returns all leases of a task.
	ListLeases(ctx context.Context, taskID string) ([]domain.ResourceLease, error)
	// ActiveLease returns the active lease for a resource, or nil.
	ActiveLease(ctx context.Context, rt domain.ResourceType, no string) (*domain.ResourceLease, error)
	// ExpireLeases marks overdue leases expired (recovery scan).
	ExpireLeases(ctx context.Context, logicalTime int64) (int, error)

	// SaveObservation upserts a coverage cell.
	SaveObservation(ctx context.Context, c domain.ObservationCell) error
	// ListObservations returns a task's coverage cells sorted by seal then age.
	ListObservations(ctx context.Context, taskID string) ([]domain.ObservationCell, error)

	// AppendEvidence inserts an immutable evidence version.
	AppendEvidence(ctx context.Context, e domain.EvidenceVersion) error
	// ListEvidence returns a task's evidence chain sorted by type, seal, age.
	ListEvidence(ctx context.Context, taskID string) ([]domain.EvidenceVersion, error)
	// NextEvidenceVersion returns the next version number for an evidence key.
	NextEvidenceVersion(ctx context.Context, e domain.EvidenceVersion) (int64, error)

	// SaveInstrumentCall persists an instrument call outbox entry.
	SaveInstrumentCall(ctx context.Context, c domain.InstrumentCall) error
	// GetInstrumentCall loads a call by ID.
	GetInstrumentCall(ctx context.Context, id string) (*domain.InstrumentCall, error)
	// ListPendingCalls returns unfinished calls for the recovery scan.
	ListPendingCalls(ctx context.Context) ([]domain.InstrumentCall, error)

	// SaveRecheck upserts a recheck case for a generation.
	SaveRecheck(ctx context.Context, r domain.RecheckCase) error
	// GetRecheck loads a recheck for a generation, or nil.
	GetRecheck(ctx context.Context, taskID string, generation int64) (*domain.RecheckCase, error)

	// SaveConfirmation inserts a production confirmation (unique role+person).
	SaveConfirmation(ctx context.Context, c domain.Confirmation) error
	// ListConfirmations returns a task's confirmations.
	ListConfirmations(ctx context.Context, taskID string) ([]domain.Confirmation, error)

	// SaveReview inserts an independent review (unique person).
	SaveReview(ctx context.Context, r domain.Review) error
	// ListReviews returns a task's reviews.
	ListReviews(ctx context.Context, taskID string) ([]domain.Review, error)

	// SaveFinalDecision inserts the single terminal decision (task-level
	// primary key arbitrates concurrent final writes).
	SaveFinalDecision(ctx context.Context, d domain.FinalDecision) error
	// GetFinalDecision returns the terminal decision, or nil.
	GetFinalDecision(ctx context.Context, taskID string) (*domain.FinalDecision, error)
}
