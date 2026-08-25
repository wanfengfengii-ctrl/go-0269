package domain

import "silkworm-egg-cold-storage-gate/internal/fixedpoint"

// Thresholds holds the fixed-point thresholds locked into a catalog rule.
type Thresholds struct {
	MinHatchRate   fixedpoint.Value
	MaxDeadEggRate fixedpoint.Value
	MaxMoisture    fixedpoint.Value
	MinTemperature fixedpoint.Value
	MaxTemperature fixedpoint.Value
	MinHumidity    fixedpoint.Value
	MaxHumidity    fixedpoint.Value
	MaxSporeCount  fixedpoint.Value
}

// RolePair is a mutual-exclusion rule between two confirmation or review
// roles.
type RolePair struct {
	RoleA string
	RoleB string
}

// CatalogRule is the versioned lineage rule that governs a lock snapshot.
type CatalogRule struct {
	LineageCode        string
	RuleVersion        int64
	AllowedBatches     []string
	IncubationDayAges  []int
	SampleSize         int
	Thresholds         Thresholds
	RoleQualifications map[string][]string // role -> qualified person IDs
	MutualExclusions   []RolePair
}

// LockSnapshot is the immutable set of entities and thresholds captured at
// lock time. It cannot be modified once written.
type LockSnapshot struct {
	CatalogVersion int64
	Generation     int64
	LineageCode    string
	BatchCode      string
	MothBagDigest  string
	CardSeals      []string
	BlindCodes     []string
	DayAges        []int
	SlideNos       []string
	Reviewers      []string
	SampleSize     int
	Thresholds     Thresholds
}

// StorageTask is the aggregate root for a single cold-storage admission flow.
type StorageTask struct {
	ID            string
	LineageCode   string
	BatchCode     string
	MothBagDigest string
	Snapshot      *LockSnapshot
	Generation    int64
	State         TaskState
	Version       int64
	CreatedAt     int64
	UpdatedAt     int64
}

// SampleSeal records the triple-sample sealing summary for one egg card: its
// sample number, triple-sample group, seal digest, blind code, and reveal
// state. The triplet group and blind code together back the triple-sample
// divergence recheck trigger.
type SampleSeal struct {
	TaskID     string
	CardSeal   string
	SampleNo   int
	Triplet    int
	SealDigest string
	BlindCode  string
	Revealed   bool
}

// ResourceLease is a unique, conditional lease over an exclusive resource.
type ResourceLease struct {
	TaskID        string
	ResourceType  ResourceType
	ResourceNo    string
	Generation    int64
	State         LeaseState
	AcquiredAt    int64
	ExpiresAt     int64
	ReleaseReason string
}

// ObservationCell is one egg-card x day-age coverage cell.
type ObservationCell struct {
	TaskID      string
	CardSeal    string
	DayAge      int
	Hatched     int64
	Unhatched   int64
	Dead        int64
	Damaged     int64
	Rechecked   bool
	Submitter   string
	EvidenceVer int64
}

// EvidenceVersion is an immutable append-only evidence record.
type EvidenceVersion struct {
	Type         EvidenceType
	TaskID       string
	CardSeal     string
	DayAge       int
	SlideNo      string
	Generation   int64
	VersionNo    int64
	Reading      fixedpoint.Value
	SourceCallID string
	Verified     bool
	PrevVersion  int64
}

// InstrumentCall is a persisted outbox entry for an external instrument call.
type InstrumentCall struct {
	ID             string
	InstrumentType InstrumentType
	TargetObject   string
	RequestDigest  string
	TaskID         string
	Generation     int64
	State          CallState
	Attempts       []InstrumentAttempt
	NextRetryAt    int64
}

// InstrumentAttempt records one deterministic attempt of an instrument call.
type InstrumentAttempt struct {
	AttemptNo      int
	State          CallState
	Failure        FailureCategory
	RawResponseSum string
	AtLogicalTime  int64
}

// RecheckCase captures a single recheck for the current generation.
type RecheckCase struct {
	TaskID          string
	Generation      int64
	TriggerSummary  string
	AffectedCards   []string
	AffectedBlinds  []string
	AffectedDayAges []int
	AffectedSlides  []string
	EvidenceClosed  bool
	Conclusion      string
}

// Confirmation records a production confirmation by one qualified person.
type Confirmation struct {
	TaskID     string
	Role       string
	PersonID   string
	Generation int64
	Digest     string
	Conclusion string
	AtLogical  int64
}

// Review records an independent terminal review by one qualified person.
type Review struct {
	TaskID     string
	PersonID   string
	Generation int64
	Digest     string
	Conclusion string
	AtLogical  int64
}

// OperationRecord stores an idempotency record for one operation ID.
type OperationRecord struct {
	OperationID string
	ContentHash string
	Response    []byte
}

// FinalDecision is the single terminal decision of a task.
type FinalDecision struct {
	TaskID          string
	Type            FinalDecisionType
	DecisionVer     int64
	AdmitCredential string
	DecidedBy       string
	AtLogical       int64
}
