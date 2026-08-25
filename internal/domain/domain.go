// Package domain defines the stable domain types, business states, resource
// kinds, and instrumentation vocabulary shared across the silkworm egg
// cold-storage gate service. These types form the backbone of the task
// aggregate, catalog, ledgers, and arbiter documented in the project spec.
package domain

// TaskState enumerates the twelve business states of a storage task. A task
// advances strictly along the linear sequence from pending lock through to
// pending independent review, then reaches exactly one terminal state.
type TaskState int

const (
	StatePendingLock TaskState = iota
	StatePendingProductionConfirmation
	StateSealingSamples
	StateOccupyingCabins
	StateObservingHatching
	StateVerifyingMicroscopy
	StateRetestingPhysicochemical
	StatePendingIndependentReview
	StateReadyToAdmit
	StateAdmitted
	StateIsolated
	StateCancelled
)

// String returns the stable wire name of a state.
func (s TaskState) String() string {
	switch s {
	case StatePendingLock:
		return "PENDING_LOCK"
	case StatePendingProductionConfirmation:
		return "PENDING_PRODUCTION_CONFIRMATION"
	case StateSealingSamples:
		return "SEALING_SAMPLES"
	case StateOccupyingCabins:
		return "OCCUPYING_CABINS"
	case StateObservingHatching:
		return "OBSERVING_HATCHING"
	case StateVerifyingMicroscopy:
		return "VERIFYING_MICROSCOPY"
	case StateRetestingPhysicochemical:
		return "RETESTING_PHYSICOCHEMICAL"
	case StatePendingIndependentReview:
		return "PENDING_INDEPENDENT_REVIEW"
	case StateReadyToAdmit:
		return "READY_TO_ADMIT"
	case StateAdmitted:
		return "ADMITTED"
	case StateIsolated:
		return "ISOLATED"
	case StateCancelled:
		return "CANCELLED"
	default:
		return "UNKNOWN"
	}
}

// Terminal reports whether the state rejects all late business writes.
func (s TaskState) Terminal() bool {
	return s == StateAdmitted || s == StateIsolated || s == StateCancelled
}

// ResourceType enumerates the three exclusive resource kinds rented by a task.
type ResourceType int

const (
	ResourceIncubationCabin ResourceType = iota
	ResourceColdStorageCell
	ResourceSlide
)

func (r ResourceType) String() string {
	switch r {
	case ResourceIncubationCabin:
		return "INCUBATION_CABIN"
	case ResourceColdStorageCell:
		return "COLD_STORAGE_CELL"
	case ResourceSlide:
		return "SLIDE"
	default:
		return "UNKNOWN"
	}
}

// InstrumentType enumerates the three scriptable instrument adapters.
type InstrumentType int

const (
	InstrumentMicroscope InstrumentType = iota
	InstrumentTempHumidityProbe
	InstrumentMoistureMeter
)

func (i InstrumentType) String() string {
	switch i {
	case InstrumentMicroscope:
		return "MICROSCOPE"
	case InstrumentTempHumidityProbe:
		return "TEMP_HUMIDITY_PROBE"
	case InstrumentMoistureMeter:
		return "MOISTURE_METER"
	default:
		return "UNKNOWN"
	}
}

// EvidenceType enumerates the append-only evidence chains a task accumulates.
type EvidenceType int

const (
	EvidenceMicroscopy EvidenceType = iota
	EvidenceEnvironment
	EvidenceMoisture
	EvidenceDamage
)

func (e EvidenceType) String() string {
	switch e {
	case EvidenceMicroscopy:
		return "MICROSCOPY"
	case EvidenceEnvironment:
		return "ENVIRONMENT"
	case EvidenceMoisture:
		return "MOISTURE"
	case EvidenceDamage:
		return "DAMAGE"
	default:
		return "UNKNOWN"
	}
}

// LeaseState is the lifecycle state of a resource lease.
type LeaseState int

const (
	LeaseActive LeaseState = iota
	LeaseExpired
	LeaseReleased
)

func (l LeaseState) String() string {
	switch l {
	case LeaseActive:
		return "ACTIVE"
	case LeaseExpired:
		return "EXPIRED"
	case LeaseReleased:
		return "RELEASED"
	default:
		return "UNKNOWN"
	}
}

// CallState is the lifecycle state of an instrument call box.
type CallState int

const (
	CallPending CallState = iota
	CallSucceeded
	CallFailed
)

func (c CallState) String() string {
	switch c {
	case CallPending:
		return "PENDING"
	case CallSucceeded:
		return "SUCCEEDED"
	case CallFailed:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}

// FailureCategory classifies a deterministic instrument failure for retry
// policy decisions.
type FailureCategory int

const (
	FailureRejected FailureCategory = iota
	FailureDisconnected
	FailureTimeout
	FailureFormatError
)

func (f FailureCategory) String() string {
	switch f {
	case FailureRejected:
		return "REJECTED"
	case FailureDisconnected:
		return "DISCONNECTED"
	case FailureTimeout:
		return "TIMEOUT"
	case FailureFormatError:
		return "FORMAT_ERROR"
	default:
		return "UNKNOWN"
	}
}

// FinalDecisionType is the type of the single terminal decision a task may
// reach through the final write barrier.
type FinalDecisionType int

const (
	FinalAdmit FinalDecisionType = iota
	FinalIsolate
	FinalCancel
)

func (d FinalDecisionType) String() string {
	switch d {
	case FinalAdmit:
		return "ADMIT"
	case FinalIsolate:
		return "ISOLATE"
	case FinalCancel:
		return "CANCEL"
	default:
		return "UNKNOWN"
	}
}
