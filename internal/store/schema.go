package store

// schema is the full DDL for the silkworm egg cold-storage gate. Every table
// carries the unique constraints that arbitrate concurrent locking, cabin
// moves, and final decisions; the database — not process-local mutexes — is
// the source of truth for resource exclusivity.
const schema = `
CREATE TABLE IF NOT EXISTS tasks (
    id               TEXT PRIMARY KEY,
    lineage_code     TEXT NOT NULL,
    batch_code       TEXT NOT NULL,
    moth_bag_digest  TEXT NOT NULL,
    generation       INTEGER NOT NULL,
    state            INTEGER NOT NULL,
    version          INTEGER NOT NULL,
    snapshot_json    TEXT,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS operations (
    operation_id  TEXT PRIMARY KEY,
    content_hash  TEXT NOT NULL,
    response_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sample_seals (
    task_id     TEXT NOT NULL,
    card_seal   TEXT NOT NULL,
    sample_no   INTEGER NOT NULL,
    triplet     INTEGER NOT NULL,
    seal_digest TEXT NOT NULL,
    blind_code  TEXT NOT NULL,
    revealed    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (task_id, card_seal)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_seal_blind
    ON sample_seals (task_id, blind_code);

CREATE TABLE IF NOT EXISTS leases (
    task_id        TEXT NOT NULL,
    resource_type  INTEGER NOT NULL,
    resource_no    TEXT NOT NULL,
    generation     INTEGER NOT NULL,
    state          INTEGER NOT NULL,
    acquired_at    INTEGER NOT NULL,
    expires_at     INTEGER NOT NULL,
    release_reason TEXT NOT NULL,
    PRIMARY KEY (task_id, resource_type, resource_no)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_lease_active
    ON leases (resource_type, resource_no) WHERE state = 0;

CREATE TABLE IF NOT EXISTS observations (
    task_id      TEXT NOT NULL,
    card_seal    TEXT NOT NULL,
    day_age      INTEGER NOT NULL,
    hatched      INTEGER NOT NULL,
    unhatched    INTEGER NOT NULL,
    dead         INTEGER NOT NULL,
    damaged      INTEGER NOT NULL,
    rechecked    INTEGER NOT NULL,
    submitter    TEXT NOT NULL,
    evidence_ver INTEGER NOT NULL,
    PRIMARY KEY (task_id, card_seal, day_age)
);

CREATE TABLE IF NOT EXISTS evidence (
    task_id         TEXT NOT NULL,
    type            INTEGER NOT NULL,
    card_seal       TEXT NOT NULL,
    day_age         INTEGER NOT NULL,
    slide_no        TEXT NOT NULL,
    generation      INTEGER NOT NULL,
    version_no      INTEGER NOT NULL,
    reading_n       INTEGER NOT NULL,
    reading_scale   INTEGER NOT NULL,
    source_call_id  TEXT NOT NULL,
    verified        INTEGER NOT NULL,
    prev_version    INTEGER NOT NULL,
    PRIMARY KEY (task_id, type, card_seal, day_age, slide_no, version_no)
);

CREATE TABLE IF NOT EXISTS instrument_calls (
    id               TEXT PRIMARY KEY,
    instrument_type  INTEGER NOT NULL,
    target_object    TEXT NOT NULL,
    request_digest   TEXT NOT NULL,
    task_id          TEXT NOT NULL,
    generation       INTEGER NOT NULL,
    state            INTEGER NOT NULL,
    attempts_json    TEXT NOT NULL,
    next_retry_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS rechecks (
    task_id        TEXT NOT NULL,
    generation     INTEGER NOT NULL,
    trigger_summary TEXT NOT NULL,
    affected_json  TEXT NOT NULL,
    evidence_closed INTEGER NOT NULL,
    conclusion     TEXT NOT NULL,
    PRIMARY KEY (task_id, generation)
);

CREATE TABLE IF NOT EXISTS confirmations (
    task_id     TEXT NOT NULL,
    role        TEXT NOT NULL,
    person_id   TEXT NOT NULL,
    generation  INTEGER NOT NULL,
    digest      TEXT NOT NULL,
    conclusion  TEXT NOT NULL,
    at_logical  INTEGER NOT NULL,
    PRIMARY KEY (task_id, role, person_id)
);

CREATE TABLE IF NOT EXISTS reviews (
    task_id     TEXT NOT NULL,
    person_id   TEXT NOT NULL,
    generation  INTEGER NOT NULL,
    digest      TEXT NOT NULL,
    conclusion  TEXT NOT NULL,
    at_logical  INTEGER NOT NULL,
    PRIMARY KEY (task_id, person_id)
);

CREATE TABLE IF NOT EXISTS final_decisions (
    task_id          TEXT PRIMARY KEY,
    type             INTEGER NOT NULL,
    decision_ver     INTEGER NOT NULL,
    admit_credential TEXT NOT NULL,
    decided_by       TEXT NOT NULL,
    at_logical       INTEGER NOT NULL
);
`
