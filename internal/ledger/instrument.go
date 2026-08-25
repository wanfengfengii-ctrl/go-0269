package ledger

import "silkworm-egg-cold-storage-gate/internal/domain"

// MaxAttempts is the fixed retry budget for an instrument call. After this
// many attempts a call is left in the failed state and never fabricates a
// reading, reveals a blind code, or releases a lease.
const MaxAttempts = 5

// RetryDelay returns the deterministic logical-time backoff before the next
// attempt for a call that has already attempted attemptNo times: 1, 2, 4, 8,
// 16 logical units. The schedule is fixed so recovery after a restart
// reproduces exactly the same next-retry instants.
func RetryDelay(attemptNo int) int64 {
	if attemptNo < 1 {
		attemptNo = 1
	}
	return int64(1) << uint(attemptNo-1)
}

// NextRetryAt computes the logical instant at which the next attempt may run.
func NextRetryAt(attemptNo int, logicalTime int64) int64 {
	return logicalTime + RetryDelay(attemptNo)
}

// IsRetryable reports whether a failure category permits another attempt
// within the fixed budget. All four failure categories are retryable; the
// budget, not the category, terminates the sequence.
func IsRetryable(f domain.FailureCategory) bool {
	switch f {
	case domain.FailureRejected, domain.FailureDisconnected, domain.FailureTimeout, domain.FailureFormatError:
		return true
	default:
		return false
	}
}

// RecordAttempt appends an attempt to a call and advances its state and next
// retry instant. On success the call is closed; on a retryable failure the
// next retry instant is scheduled; on a non-retryable or budget-exhausted
// failure the call stays failed with no further retry scheduled.
func RecordAttempt(call *domain.InstrumentCall, attempt domain.InstrumentAttempt, logicalTime int64) {
	call.Attempts = append(call.Attempts, attempt)
	switch attempt.State {
	case domain.CallSucceeded:
		call.State = domain.CallSucceeded
		call.NextRetryAt = 0
	case domain.CallFailed:
		if IsRetryable(attempt.Failure) && len(call.Attempts) < MaxAttempts {
			call.State = domain.CallFailed
			call.NextRetryAt = NextRetryAt(len(call.Attempts), logicalTime)
		} else {
			call.State = domain.CallFailed
			call.NextRetryAt = 0
		}
	}
}

// NewCall builds a fresh pending instrument call box for the given task,
// generation, instrument, target object, and normalized request digest.
func NewCall(id, taskID string, generation int64, it domain.InstrumentType, target, digest string, logicalTime int64) domain.InstrumentCall {
	return domain.InstrumentCall{
		ID:             id,
		InstrumentType: it,
		TargetObject:   target,
		RequestDigest:  digest,
		TaskID:         taskID,
		Generation:     generation,
		State:          domain.CallPending,
		NextRetryAt:    logicalTime,
	}
}
