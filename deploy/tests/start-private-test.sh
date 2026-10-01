#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT
mkdir -p "$TEMP_DIR/bin"
touch "$TEMP_DIR/.env" "$TEMP_DIR/compose.yml"

cat > "$TEMP_DIR/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$CALL_LOG"
if [[ "$1" == login ]]; then
    cat > "$LOGIN_STDIN"
    exit "${LOGIN_STATUS:-0}"
fi
if [[ "$*" == *'config --format json'* ]]; then
    printf '%s\n' '{"services":{"sub2api":{"environment":{"UPDATE_GITHUB_TOKEN":"test-update-token"}}}}'
fi
EOF
chmod +x "$TEMP_DIR/bin/docker"
export PATH="$TEMP_DIR/bin:$PATH"
export SUB2API_ENV_FILE="$TEMP_DIR/.env"
export SUB2API_COMPOSE_FILE="$TEMP_DIR/compose.yml"
export CALL_LOG="$TEMP_DIR/calls" LOGIN_STDIN="$TEMP_DIR/token"

bash "$ROOT_DIR/deploy/start-private.sh" > "$TEMP_DIR/output"
test "$(cat "$LOGIN_STDIN")" = test-update-token
grep -Fq 'login ghcr.io -u mizaawa --password-stdin' "$CALL_LOG"
grep -Fq 'pull sub2api' "$CALL_LOG"
grep -Fq 'up -d' "$CALL_LOG"
if grep -Fq test-update-token "$CALL_LOG" "$TEMP_DIR/output"; then
    echo 'Registry token leaked into arguments or console output.' >&2
    exit 1
fi

: > "$CALL_LOG"
export LOGIN_STATUS=1
if bash "$ROOT_DIR/deploy/start-private.sh" > "$TEMP_DIR/output" 2>&1; then
    echo 'Deployment started after registry authentication failed.' >&2
    exit 1
fi
if grep -Eq 'pull sub2api|up -d' "$CALL_LOG"; then
    echo 'Deployment mutated services after registry authentication failed.' >&2
    exit 1
fi

echo 'Private deployment startup checks passed.'
