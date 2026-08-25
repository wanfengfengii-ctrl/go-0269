package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	_ "modernc.org/sqlite"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

// SQLiteStore is the production Store backed by a SQLite database in WAL mode.
// It is safe for concurrent use: the underlying pool serializes writers while
// unique indexes and optimistic versions resolve application-level races.
type SQLiteStore struct {
	db    *sql.DB
	mu    sync.RWMutex
	ready bool
}

// OpenSQLite opens (or creates) a SQLite database at path, applies pragmas,
// and returns a store whose Migrate method must be called before use.
func OpenSQLite(path string) (*SQLiteStore, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return &SQLiteStore{db: db}, nil
}

// Migrate applies the schema and marks the store ready.
func (s *SQLiteStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()
	return nil
}

// Ready reports whether migration has completed.
func (s *SQLiteStore) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ready
}

// Close releases the underlying database connection.
func (s *SQLiteStore) Close() error { return s.db.Close() }

// WithTx runs fn inside a transaction.
func (s *SQLiteStore) WithTx(ctx context.Context, fn func(Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(&sqliteTx{tx: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

// sqliteTx implements Tx against a single database transaction.
type sqliteTx struct{ tx *sql.Tx }

func (t *sqliteTx) GetTask(ctx context.Context, id string) (*domain.StorageTask, error) {
	row := t.tx.QueryRowContext(ctx,
		`SELECT id, lineage_code, batch_code, moth_bag_digest, generation, state, version, snapshot_json, created_at, updated_at
		 FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

func (t *sqliteTx) CreateTask(ctx context.Context, tk *domain.StorageTask) error {
	snap, err := marshalSnapshot(tk.Snapshot)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx,
		`INSERT INTO tasks (id, lineage_code, batch_code, moth_bag_digest, generation, state, version, snapshot_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tk.ID, tk.LineageCode, tk.BatchCode, tk.MothBagDigest, tk.Generation, int(tk.State), tk.Version, snap, tk.CreatedAt, tk.UpdatedAt)
	return mapErr(err)
}

func (t *sqliteTx) UpdateTask(ctx context.Context, tk *domain.StorageTask) error {
	snap, err := marshalSnapshot(tk.Snapshot)
	if err != nil {
		return err
	}
	res, err := t.tx.ExecContext(ctx,
		`UPDATE tasks SET lineage_code=?, batch_code=?, moth_bag_digest=?, generation=?, state=?, version=version+1, snapshot_json=?, updated_at=?
		 WHERE id=? AND version=?`,
		tk.LineageCode, tk.BatchCode, tk.MothBagDigest, tk.Generation, int(tk.State), snap, tk.UpdatedAt, tk.ID, tk.Version)
	if err != nil {
		return mapErr(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Distinguish "missing" from "stale version".
		var one int
		if e := t.tx.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE id=?`, tk.ID).Scan(&one); e == sql.ErrNoRows {
			return ErrNotFound
		}
		return ErrConcurrentVersion
	}
	tk.Version++
	return nil
}

func (t *sqliteTx) GetOperation(ctx context.Context, opID string) (*domain.OperationRecord, error) {
	row := t.tx.QueryRowContext(ctx, `SELECT operation_id, content_hash, response_json FROM operations WHERE operation_id=?`, opID)
	var r domain.OperationRecord
	var resp []byte
	if err := row.Scan(&r.OperationID, &r.ContentHash, &resp); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.Response = resp
	return &r, nil
}

func (t *sqliteTx) RecordOperation(ctx context.Context, opID, hash string, resp []byte) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO operations (operation_id, content_hash, response_json) VALUES (?, ?, ?)`, opID, hash, string(resp))
	return mapErr(err)
}

func (t *sqliteTx) SaveSampleSeals(ctx context.Context, seals []domain.SampleSeal) error {
	for _, s := range seals {
		if _, err := t.tx.ExecContext(ctx,
			`INSERT INTO sample_seals (task_id, card_seal, sample_no, triplet, seal_digest, blind_code, revealed)
			 VALUES (?, ?, ?, ?, ?, ?, 0)`,
			s.TaskID, s.CardSeal, s.SampleNo, tripletOf(s), s.SealDigest, s.BlindCode); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

// tripletOf derives the triple-sample group from the sample number so each
// card joins one of three sealed groups without carrying the value separately.
func tripletOf(s domain.SampleSeal) int {
	if s.Triplet > 0 {
		return s.Triplet
	}
	return (s.SampleNo-1)%3 + 1
}

func (t *sqliteTx) ListSampleSeals(ctx context.Context, taskID string) ([]domain.SampleSeal, error) {
	rows, err := t.tx.QueryContext(ctx,
		`SELECT task_id, card_seal, sample_no, triplet, seal_digest, blind_code, revealed
		 FROM sample_seals WHERE task_id=? ORDER BY sample_no`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SampleSeal
	for rows.Next() {
		var s domain.SampleSeal
		var revealed int
		if err := rows.Scan(&s.TaskID, &s.CardSeal, &s.SampleNo, &s.Triplet, &s.SealDigest, &s.BlindCode, &revealed); err != nil {
			return nil, err
		}
		s.Revealed = revealed != 0
		out = append(out, s)
	}
	return out, rows.Err()
}

func (t *sqliteTx) RevealBlindCode(ctx context.Context, taskID, cardSeal string) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE sample_seals SET revealed=1 WHERE task_id=? AND card_seal=?`, taskID, cardSeal)
	return err
}

func (t *sqliteTx) AcquireLease(ctx context.Context, l domain.ResourceLease) error {
	_, err := t.tx.ExecContext(ctx,
		`INSERT INTO leases (task_id, resource_type, resource_no, generation, state, acquired_at, expires_at, release_reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		l.TaskID, int(l.ResourceType), l.ResourceNo, l.Generation, int(l.State), l.AcquiredAt, l.ExpiresAt, l.ReleaseReason)
	return mapErr(err)
}

func (t *sqliteTx) ReleaseLease(ctx context.Context, taskID string, rt domain.ResourceType, no, reason string) error {
	_, err := t.tx.ExecContext(ctx,
		`UPDATE leases SET state=?, release_reason=? WHERE task_id=? AND resource_type=? AND resource_no=? AND state=0`,
		int(domain.LeaseReleased), reason, taskID, int(rt), no)
	return err
}

func (t *sqliteTx) RenewLease(ctx context.Context, taskID string, rt domain.ResourceType, no string, newExpiresAt int64) error {
	res, err := t.tx.ExecContext(ctx,
		`UPDATE leases SET expires_at=? WHERE task_id=? AND resource_type=? AND resource_no=? AND state=0`,
		newExpiresAt, taskID, int(rt), no)
	if err != nil {
		return mapErr(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (t *sqliteTx) ListLeases(ctx context.Context, taskID string) ([]domain.ResourceLease, error) {
	rows, err := t.tx.QueryContext(ctx,
		`SELECT task_id, resource_type, resource_no, generation, state, acquired_at, expires_at, release_reason
		 FROM leases WHERE task_id=? ORDER BY resource_type, resource_no`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ResourceLease
	for rows.Next() {
		var l domain.ResourceLease
		if err := rows.Scan(&l.TaskID, &l.ResourceType, &l.ResourceNo, &l.Generation, &l.State, &l.AcquiredAt, &l.ExpiresAt, &l.ReleaseReason); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (t *sqliteTx) ActiveLease(ctx context.Context, rt domain.ResourceType, no string) (*domain.ResourceLease, error) {
	row := t.tx.QueryRowContext(ctx,
		`SELECT task_id, resource_type, resource_no, generation, state, acquired_at, expires_at, release_reason
		 FROM leases WHERE resource_type=? AND resource_no=? AND state=0`, int(rt), no)
	var l domain.ResourceLease
	if err := row.Scan(&l.TaskID, &l.ResourceType, &l.ResourceNo, &l.Generation, &l.State, &l.AcquiredAt, &l.ExpiresAt, &l.ReleaseReason); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &l, nil
}

func (t *sqliteTx) ExpireLeases(ctx context.Context, logicalTime int64) (int, error) {
	res, err := t.tx.ExecContext(ctx,
		`UPDATE leases SET state=? WHERE state=0 AND expires_at < ?`, int(domain.LeaseExpired), logicalTime)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (t *sqliteTx) SaveObservation(ctx context.Context, c domain.ObservationCell) error {
	_, err := t.tx.ExecContext(ctx,
		`INSERT INTO observations (task_id, card_seal, day_age, hatched, unhatched, dead, damaged, rechecked, submitter, evidence_ver)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(task_id, card_seal, day_age) DO UPDATE SET
		   hatched=excluded.hatched, unhatched=excluded.unhatched, dead=excluded.dead, damaged=excluded.damaged,
		   rechecked=excluded.rechecked, submitter=excluded.submitter, evidence_ver=excluded.evidence_ver`,
		c.TaskID, c.CardSeal, c.DayAge, c.Hatched, c.Unhatched, c.Dead, c.Damaged, boolInt(c.Rechecked), c.Submitter, c.EvidenceVer)
	return mapErr(err)
}

func (t *sqliteTx) ListObservations(ctx context.Context, taskID string) ([]domain.ObservationCell, error) {
	rows, err := t.tx.QueryContext(ctx,
		`SELECT task_id, card_seal, day_age, hatched, unhatched, dead, damaged, rechecked, submitter, evidence_ver
		 FROM observations WHERE task_id=? ORDER BY card_seal, day_age`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ObservationCell
	for rows.Next() {
		var c domain.ObservationCell
		var re int
		if err := rows.Scan(&c.TaskID, &c.CardSeal, &c.DayAge, &c.Hatched, &c.Unhatched, &c.Dead, &c.Damaged, &re, &c.Submitter, &c.EvidenceVer); err != nil {
			return nil, err
		}
		c.Rechecked = re != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

func (t *sqliteTx) AppendEvidence(ctx context.Context, e domain.EvidenceVersion) error {
	_, err := t.tx.ExecContext(ctx,
		`INSERT INTO evidence (task_id, type, card_seal, day_age, slide_no, generation, version_no, reading_n, reading_scale, source_call_id, verified, prev_version)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.TaskID, int(e.Type), e.CardSeal, e.DayAge, e.SlideNo, e.Generation, e.VersionNo, e.Reading.N, e.Reading.Scale, e.SourceCallID, boolInt(e.Verified), e.PrevVersion)
	return mapErr(err)
}

func (t *sqliteTx) ListEvidence(ctx context.Context, taskID string) ([]domain.EvidenceVersion, error) {
	rows, err := t.tx.QueryContext(ctx,
		`SELECT task_id, type, card_seal, day_age, slide_no, generation, version_no, reading_n, reading_scale, source_call_id, verified, prev_version
		 FROM evidence WHERE task_id=? ORDER BY type, card_seal, day_age, slide_no, version_no`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.EvidenceVersion
	for rows.Next() {
		var e domain.EvidenceVersion
		var verified int
		if err := rows.Scan(&e.TaskID, &e.Type, &e.CardSeal, &e.DayAge, &e.SlideNo, &e.Generation, &e.VersionNo, &e.Reading.N, &e.Reading.Scale, &e.SourceCallID, &verified, &e.PrevVersion); err != nil {
			return nil, err
		}
		e.Verified = verified != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

func (t *sqliteTx) NextEvidenceVersion(ctx context.Context, e domain.EvidenceVersion) (int64, error) {
	var v int64
	err := t.tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version_no), 0) FROM evidence WHERE task_id=? AND type=? AND card_seal=? AND day_age=? AND slide_no=?`,
		e.TaskID, int(e.Type), e.CardSeal, e.DayAge, e.SlideNo).Scan(&v)
	return v + 1, err
}

func (t *sqliteTx) SaveInstrumentCall(ctx context.Context, c domain.InstrumentCall) error {
	attempts, err := json.Marshal(c.Attempts)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx,
		`INSERT INTO instrument_calls (id, instrument_type, target_object, request_digest, task_id, generation, state, attempts_json, next_retry_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET state=excluded.state, attempts_json=excluded.attempts_json, next_retry_at=excluded.next_retry_at`,
		c.ID, int(c.InstrumentType), c.TargetObject, c.RequestDigest, c.TaskID, c.Generation, int(c.State), string(attempts), c.NextRetryAt)
	return mapErr(err)
}

func (t *sqliteTx) GetInstrumentCall(ctx context.Context, id string) (*domain.InstrumentCall, error) {
	row := t.tx.QueryRowContext(ctx,
		`SELECT id, instrument_type, target_object, request_digest, task_id, generation, state, attempts_json, next_retry_at
		 FROM instrument_calls WHERE id=?`, id)
	var c domain.InstrumentCall
	var attempts []byte
	if err := row.Scan(&c.ID, &c.InstrumentType, &c.TargetObject, &c.RequestDigest, &c.TaskID, &c.Generation, &c.State, &attempts, &c.NextRetryAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(attempts) > 0 {
		if err := json.Unmarshal(attempts, &c.Attempts); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

func (t *sqliteTx) ListPendingCalls(ctx context.Context) ([]domain.InstrumentCall, error) {
	rows, err := t.tx.QueryContext(ctx,
		`SELECT id, instrument_type, target_object, request_digest, task_id, generation, state, attempts_json, next_retry_at
		 FROM instrument_calls WHERE state != ?`, int(domain.CallSucceeded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.InstrumentCall
	for rows.Next() {
		var c domain.InstrumentCall
		var attempts []byte
		if err := rows.Scan(&c.ID, &c.InstrumentType, &c.TargetObject, &c.RequestDigest, &c.TaskID, &c.Generation, &c.State, &attempts, &c.NextRetryAt); err != nil {
			return nil, err
		}
		if len(attempts) > 0 {
			_ = json.Unmarshal(attempts, &c.Attempts)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (t *sqliteTx) SaveRecheck(ctx context.Context, r domain.RecheckCase) error {
	affected, err := json.Marshal(recheckAffected{
		Cards:  r.AffectedCards,
		Blinds: r.AffectedBlinds,
		Days:   r.AffectedDayAges,
		Slides: r.AffectedSlides,
	})
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx,
		`INSERT INTO rechecks (task_id, generation, trigger_summary, affected_json, evidence_closed, conclusion)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(task_id, generation) DO UPDATE SET trigger_summary=excluded.trigger_summary, affected_json=excluded.affected_json, evidence_closed=excluded.evidence_closed, conclusion=excluded.conclusion`,
		r.TaskID, r.Generation, r.TriggerSummary, string(affected), boolInt(r.EvidenceClosed), r.Conclusion)
	return mapErr(err)
}

func (t *sqliteTx) GetRecheck(ctx context.Context, taskID string, generation int64) (*domain.RecheckCase, error) {
	row := t.tx.QueryRowContext(ctx,
		`SELECT task_id, generation, trigger_summary, affected_json, evidence_closed, conclusion
		 FROM rechecks WHERE task_id=? AND generation=?`, taskID, generation)
	var r domain.RecheckCase
	var affected []byte
	var closed int
	if err := row.Scan(&r.TaskID, &r.Generation, &r.TriggerSummary, &affected, &closed, &r.Conclusion); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.EvidenceClosed = closed != 0
	if len(affected) > 0 {
		var ra recheckAffected
		if err := json.Unmarshal(affected, &ra); err != nil {
			return nil, err
		}
		r.AffectedCards = ra.Cards
		r.AffectedBlinds = ra.Blinds
		r.AffectedDayAges = ra.Days
		r.AffectedSlides = ra.Slides
	}
	return &r, nil
}

func (t *sqliteTx) SaveConfirmation(ctx context.Context, c domain.Confirmation) error {
	_, err := t.tx.ExecContext(ctx,
		`INSERT INTO confirmations (task_id, role, person_id, generation, digest, conclusion, at_logical)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.TaskID, c.Role, c.PersonID, c.Generation, c.Digest, c.Conclusion, c.AtLogical)
	return mapErr(err)
}

func (t *sqliteTx) ListConfirmations(ctx context.Context, taskID string) ([]domain.Confirmation, error) {
	rows, err := t.tx.QueryContext(ctx,
		`SELECT task_id, role, person_id, generation, digest, conclusion, at_logical
		 FROM confirmations WHERE task_id=? ORDER BY role, person_id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Confirmation
	for rows.Next() {
		var c domain.Confirmation
		if err := rows.Scan(&c.TaskID, &c.Role, &c.PersonID, &c.Generation, &c.Digest, &c.Conclusion, &c.AtLogical); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (t *sqliteTx) SaveReview(ctx context.Context, r domain.Review) error {
	_, err := t.tx.ExecContext(ctx,
		`INSERT INTO reviews (task_id, person_id, generation, digest, conclusion, at_logical)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.TaskID, r.PersonID, r.Generation, r.Digest, r.Conclusion, r.AtLogical)
	return mapErr(err)
}

func (t *sqliteTx) ListReviews(ctx context.Context, taskID string) ([]domain.Review, error) {
	rows, err := t.tx.QueryContext(ctx,
		`SELECT task_id, person_id, generation, digest, conclusion, at_logical
		 FROM reviews WHERE task_id=? ORDER BY person_id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Review
	for rows.Next() {
		var r domain.Review
		if err := rows.Scan(&r.TaskID, &r.PersonID, &r.Generation, &r.Digest, &r.Conclusion, &r.AtLogical); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (t *sqliteTx) SaveFinalDecision(ctx context.Context, d domain.FinalDecision) error {
	_, err := t.tx.ExecContext(ctx,
		`INSERT INTO final_decisions (task_id, type, decision_ver, admit_credential, decided_by, at_logical)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		d.TaskID, int(d.Type), d.DecisionVer, d.AdmitCredential, d.DecidedBy, d.AtLogical)
	return mapErr(err)
}

func (t *sqliteTx) GetFinalDecision(ctx context.Context, taskID string) (*domain.FinalDecision, error) {
	row := t.tx.QueryRowContext(ctx,
		`SELECT task_id, type, decision_ver, admit_credential, decided_by, at_logical
		 FROM final_decisions WHERE task_id=?`, taskID)
	var d domain.FinalDecision
	if err := row.Scan(&d.TaskID, &d.Type, &d.DecisionVer, &d.AdmitCredential, &d.DecidedBy, &d.AtLogical); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// recheckAffected is the JSON payload stored for a recheck's coverage set.
type recheckAffected struct {
	Cards  []string `json:"cards"`
	Blinds []string `json:"blinds"`
	Days   []int    `json:"days"`
	Slides []string `json:"slides"`
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// mapErr converts a SQLite constraint or optimistic failure into a stable
// store error. Unique/PK violations become ErrDuplicate.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "PRIMARY KEY constraint") {
		return ErrDuplicate
	}
	return err
}

// scanTask decodes a task row, tolerating a nil snapshot.
func scanTask(row *sql.Row) (*domain.StorageTask, error) {
	var tk domain.StorageTask
	var snap sql.NullString
	if err := row.Scan(&tk.ID, &tk.LineageCode, &tk.BatchCode, &tk.MothBagDigest, &tk.Generation, &tk.State, &tk.Version, &snap, &tk.CreatedAt, &tk.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if snap.Valid && snap.String != "" {
		var s domain.LockSnapshot
		if err := json.Unmarshal([]byte(snap.String), &s); err != nil {
			return nil, err
		}
		tk.Snapshot = &s
	}
	return &tk, nil
}

func marshalSnapshot(s *domain.LockSnapshot) (any, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}

// compile-time interface assertions.
var (
	_ Store = (*SQLiteStore)(nil)
	_ Tx    = (*sqliteTx)(nil)
)
