#!/usr/bin/env bash
# Smoke test for the silkworm egg cold-storage gate service. It builds the
# server binary, starts it against a temporary SQLite database, drives a real
# create/lock flow over HTTP, asserts the responses, and tears everything down.
#
# Fail-fast throughout; no external network access is required (the module
# cache is used for the build).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK="$(mktemp -d)"
PORT="${BENZHI_SMOKE_PORT:-18087}"
BASE="http://127.0.0.1:${PORT}"
SERVER_PID=""

cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -rf "${WORK}"
}
trap cleanup EXIT

log() { printf 'smoke: %s\n' "$*"; }

# 1. Build the server binary using the local module cache (no network).
log "building server binary"
(
  cd "${ROOT}"
  GOPROXY=off go build -o "${WORK}/server" ./cmd/server
)

# 2. Start the service against a temporary database.
log "starting service on ${PORT}"
DB_PATH="${WORK}/benzhi.db" ADDR="127.0.0.1:${PORT}" "${WORK}/server" &
SERVER_PID=$!

# 3. Wait for readiness, probing locally with a bounded retry loop.
ready=""
for _ in $(seq 1 50); do
  if resp="$(curl -fsS "${BASE}/readyz" 2>/dev/null || true)"; then
    ready="${resp}"
  fi
  if printf '%s' "${ready}" | grep -q '"status":"ready"'; then
    break
  fi
  sleep 0.1
done
if ! printf '%s' "${ready}" | grep -q '"status":"ready"'; then
  log "service did not become ready; last response: ${ready}"
  exit 1
fi
log "service ready"

# 4. Liveness probe.
health="$(curl -fsS "${BASE}/healthz")"
if ! printf '%s' "${health}" | grep -q '"status":"ok"'; then
  log "unexpected healthz response: ${health}"
  exit 1
fi
log "healthz ok"

# 5. Create a task.
create_body='{"taskId":"smoke-task-1","lineageCode":"lineage-001","batchCode":"batch-001","mothBagDigest":"digest-smoke"}'
create_resp="$(curl -fsS -X POST "${BASE}/v1/tasks" \
  -H 'Content-Type: application/json' \
  -H 'Operation-Id: op-smoke-create' \
  -d "${create_body}")"
if ! printf '%s' "${create_resp}" | grep -q '"state":"PENDING_LOCK"'; then
  log "unexpected create response: ${create_resp}"
  exit 1
fi
log "task created"

# 6. Lock the task.
lock_body='{"ruleVersion":1,"generation":1,"cardSeals":["seal-1","seal-2"],"blindCodes":["blind-1","blind-2"],"dayAges":[1,2],"slideNos":["slide-1","slide-2"],"reviewers":["person-c","person-d"]}'
lock_resp="$(curl -fsS -X POST "${BASE}/v1/tasks/smoke-task-1/lock" \
  -H 'Content-Type: application/json' \
  -H 'Operation-Id: op-smoke-lock' \
  -d "${lock_body}")"
if ! printf '%s' "${lock_resp}" | grep -q '"state":"PENDING_PRODUCTION_CONFIRMATION"'; then
  log "unexpected lock response: ${lock_resp}"
  exit 1
fi
log "task locked"

# 7. Read back the aggregate and confirm the lock snapshot survived.
detail="$(curl -fsS "${BASE}/v1/tasks/smoke-task-1")"
if ! printf '%s' "${detail}" | grep -q '"locked":true'; then
  log "task detail missing locked flag: ${detail}"
  exit 1
fi
log "task detail verified"

log "smoke test passed"
