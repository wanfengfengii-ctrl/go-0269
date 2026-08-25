package domain

import (
	"fmt"
	"sort"
)

// ErrorCode is a stable, machine-readable rejection code surfaced to clients.
type ErrorCode string

const (
	ErrInvalidInput           ErrorCode = "INVALID_INPUT"
	ErrNotFound               ErrorCode = "NOT_FOUND"
	ErrDuplicateResource      ErrorCode = "DUPLICATE_RESOURCE"
	ErrStaleGeneration        ErrorCode = "STALE_GENERATION"
	ErrInvalidStateTransition ErrorCode = "INVALID_STATE_TRANSITION"
	ErrOperationConflict      ErrorCode = "OPERATION_CONFLICT"
	ErrConcurrentModification ErrorCode = "CONCURRENT_MODIFICATION"
	ErrArithmeticInvalid      ErrorCode = "ARITHMETIC_INVALID"
	ErrFinalDecisionConflict  ErrorCode = "FINAL_DECISION_CONFLICT"
	ErrInternal               ErrorCode = "INTERNAL_ERROR"
)

// Reason is a single ordered rejection cause. The sort key follows the
// documented ordering: lineage, batch, egg-card seal, hatching day-age,
// slide number, then reason code.
type Reason struct {
	Code     string `json:"code"`
	Lineage  string `json:"lineage,omitempty"`
	Batch    string `json:"batch,omitempty"`
	CardSeal string `json:"cardSeal,omitempty"`
	DayAge   int    `json:"dayAge,omitempty"`
	SlideNo  string `json:"slideNo,omitempty"`
	Message  string `json:"message,omitempty"`
}

// DomainError is the unified error object returned by every rejected command.
type DomainError struct {
	Code    ErrorCode `json:"code"`
	State   TaskState `json:"state"`
	Reasons []Reason  `json:"reasons"`
}

// Error implements the error interface with a stable, deterministic string.
func (e *DomainError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s(state=%s reasons=%d)", e.Code, e.State, len(e.Reasons))
}

// NewError builds a DomainError with a single reason.
func NewError(code ErrorCode, state TaskState, reason Reason) *DomainError {
	return &DomainError{Code: code, State: state, Reasons: []Reason{reason}}
}

// SortReasons orders reasons deterministically by lineage, batch, card seal,
// day-age, slide number, and finally reason code.
func SortReasons(rs []Reason) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if a.Lineage != b.Lineage {
			return a.Lineage < b.Lineage
		}
		if a.Batch != b.Batch {
			return a.Batch < b.Batch
		}
		if a.CardSeal != b.CardSeal {
			return a.CardSeal < b.CardSeal
		}
		if a.DayAge != b.DayAge {
			return a.DayAge < b.DayAge
		}
		if a.SlideNo != b.SlideNo {
			return a.SlideNo < b.SlideNo
		}
		return a.Code < b.Code
	})
}

// OpStatus classifies an incoming operation against an existing record.
type OpStatus int

const (
	OpNew OpStatus = iota
	OpIdempotent
	OpConflict
)

func (s OpStatus) String() string {
	switch s {
	case OpNew:
		return "NEW"
	case OpIdempotent:
		return "IDEMPOTENT"
	case OpConflict:
		return "CONFLICT"
	default:
		return "UNKNOWN"
	}
}
