#!/usr/bin/env bash
set -euo pipefail

# Bootstrap RAG for this repo: sets SQLite path, ensures server is up,
# creates/reuses project, runs streaming index to generate embeddings.

ROOT_DEFAULT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
ROOT="${ROOT:-$ROOT_DEFAULT}"
NAME_DEFAULT=$(basename "$ROOT")
NAME="${NAME:-$NAME_DEFAULT}"
ADDR="${ADDR:-:8089}"
FORCE_RESTART="${FORCE_RESTART:-}"
REINDEX="${REINDEX:-1}"
SERVER_URL="${MYCODER_SERVER_URL:-http://localhost:8089}"

SQLITE_PATH="${MYCODER_SQLITE_PATH:-$ROOT/.mycoder/mycoder.sqlite}"
export MYCODER_SQLITE_PATH="$SQLITE_PATH"
# Force walk-based indexing to honor explicit --include patterns even when .gitignore excludes.
export MYCODER_INDEX_FORCE_WALK="${MYCODER_INDEX_FORCE_WALK:-1}"

log() { printf '[bootstrap] %s\n' "$*"; }
need() { command -v "$1" >/dev/null 2>&1 || { echo "need '$1' in PATH" >&2; exit 1; }; }

# Prefer project-local binary first, then fallback to PATH
if [ -x "$ROOT/bin/mycoder" ]; then
  CMD="$ROOT/bin/mycoder"
else
  CMD="mycoder"
  if ! command -v mycoder >/dev/null 2>&1; then
    log "mycoder binary not found. Run 'make build' first."
    exit 1
  fi
fi

mkdir -p "$ROOT/.mycoder"

# 1) Ensure server is running
health() { curl -fsS "$SERVER_URL/healthz" >/dev/null 2>&1; }

kill_server() {
  # Try to kill by listening port first (macOS/Linux with lsof)
  if command -v lsof >/dev/null 2>&1; then
    PORT="${ADDR##*:}"
    PIDS=$(lsof -nP -iTCP:"$PORT" -sTCP:LISTEN -t 2>/dev/null || true)
    if [ -n "$PIDS" ]; then
      log "stopping existing server on :$PORT (pids: $PIDS)"
      kill $PIDS 2>/dev/null || true
      sleep 1
    fi
  fi
  # Fallback: kill by command pattern
  if command -v pkill >/dev/null 2>&1; then
    pkill -f "$CMD serve --addr" 2>/dev/null || true
  fi
}

if [ -n "$FORCE_RESTART" ]; then
  kill_server
fi

if ! health; then
  log "starting server on $ADDR (log: $ROOT/.mycoder/server.log)"
  nohup "$CMD" serve --addr "$ADDR" >"$ROOT/.mycoder/server.log" 2>&1 &
  # wait up to 15s for health
  for i in $(seq 1 30); do
    if health; then break; fi
    sleep 0.5
  done
  if ! health; then
    echo "server failed to start or not reachable at $SERVER_URL" >&2
    exit 1
  fi
else
  log "server already running at $SERVER_URL"
fi

# 2) Find or create project for this root
PROJECT_ID=""
if command -v python3 >/dev/null 2>&1; then
  PROJECT_ID=$(curl -fsS "$SERVER_URL/projects" | python3 - "$ROOT" <<'PY'
import json,sys
root=sys.argv[1]
try:
    arr=json.load(sys.stdin)
    for p in arr:
        if p.get('rootPath')==root:
            print(p.get('id') or '')
            sys.exit(0)
except Exception:
    pass
print('')
PY
)
fi

if [ -z "$PROJECT_ID" ]; then
  log "creating project name=$NAME root=$ROOT"
  PROJECT_ID=$("$CMD" projects create --name "$NAME" --root "$ROOT" | sed -n 's/.*"projectID":"\([^"]*\)".*/\1/p') || true
  if [ -z "$PROJECT_ID" ]; then
    echo "failed to create project (no projectID returned)" >&2
    exit 1
  fi
  log "created project: $PROJECT_ID"
else
  log "using existing project: $PROJECT_ID"
fi

# 3) Run streaming index (ensures embeddings are generated)
if [ "$REINDEX" != "0" ]; then
  log "indexing (streaming) project=$PROJECT_ID"
  ARGS=(index --project "$PROJECT_ID" --mode full --stream --retries 1 --save-log "$ROOT/.mycoder/index_stream.log")
  if [ -n "${INCLUDE:-}" ]; then ARGS+=(--include "$INCLUDE"); fi
  if [ -n "${EXCLUDE:-}" ]; then ARGS+=(--exclude "$EXCLUDE"); fi
  "$CMD" "${ARGS[@]}" || {
    echo "indexing failed" >&2
    exit 1
  }
else
  log "reindex skipped (REINDEX=0)"
fi

# 4) Quick search smoke test
log "search smoke test"
"$CMD" search "func" --project "$PROJECT_ID" | sed -n '1,5p'

log "done. env=MYCODER_SQLITE_PATH=$MYCODER_SQLITE_PATH project=$PROJECT_ID"
log "next: $CMD chat --project $PROJECT_ID \"질문을 입력하세요\""
