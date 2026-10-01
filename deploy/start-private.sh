#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ENV_FILE=${SUB2API_ENV_FILE:-"$SCRIPT_DIR/.env"}
COMPOSE_FILE=${SUB2API_COMPOSE_FILE:-"$SCRIPT_DIR/docker-compose.local.yml"}

command -v docker >/dev/null || { echo 'Docker is required.' >&2; exit 1; }
command -v python3 >/dev/null || { echo 'Python 3 is required.' >&2; exit 1; }
test -f "$ENV_FILE" || { echo "Deployment environment file is missing: $ENV_FILE" >&2; exit 1; }

compose=(docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE")
"${compose[@]}" config -q

registry_token=${GHCR_TOKEN:-}
if [[ -z "$registry_token" ]]; then
    registry_token=$("${compose[@]}" config --format json | python3 -c '
import json, sys
token = json.load(sys.stdin)["services"]["sub2api"].get("environment", {}).get("UPDATE_GITHUB_TOKEN", "")
if not token:
    sys.exit("Set UPDATE_GITHUB_TOKEN in .env or export GHCR_TOKEN before starting.")
sys.stdout.write(token)
')
fi

# Registry credentials go through stdin and remain outside Git and image layers.
printf '%s' "$registry_token" | docker login ghcr.io -u "${GHCR_USERNAME:-mizaawa}" --password-stdin
unset registry_token
"${compose[@]}" pull sub2api
"${compose[@]}" up -d
"${compose[@]}" ps
