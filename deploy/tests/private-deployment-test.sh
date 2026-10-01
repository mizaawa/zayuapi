#!/bin/bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

mkdir -p "$TEST_DIR/bin" "$TEST_DIR/source" "$TEST_DIR/output" "$TEST_DIR/systemd"
cat > "$TEST_DIR/bin/curl" <<'SH'
#!/bin/bash
set -euo pipefail
index=$(find "$CURL_LOG_DIR" -name '*.args' | wc -l | tr -d ' ')
log="$CURL_LOG_DIR/$index"
printf '%s\n' "$@" > "$log.args"
env > "$log.env"
headers= output= url= authenticated=false
while [ "$#" -gt 0 ]; do
    case "$1" in
        --config) cat > "$log.stdin"; authenticated=true; shift 2 ;;
        -D) headers=$2; shift 2 ;;
        -o) output=$2; shift 2 ;;
        https://*) url=$1; shift ;;
        *) shift ;;
    esac
done
case "$url" in
    https://api.github.com/repos/mizaawa/zayuapi/releases/assets/1)
        [ "$authenticated" = true ]
        printf 'HTTP/1.1 200 Connection established\r\n\r\nHTTP/2 302\r\nLocation: %s\r\n\r\n' \
            "${ASSET_REDIRECT:-https://release-assets.githubusercontent.com/asset?signature=test}" > "$headers"
        printf '302'
        ;;
    https://release-assets.githubusercontent.com/asset\?signature=test)
        [ "$authenticated" = false ]
        printf 'release payload' > "$output"
        ;;
    https://api.github.com/repos/mizaawa/zayuapi/contents/deploy/*)
        [ "$authenticated" = true ]
        printf 'private file content' > "$output"
        printf '%s' "${CONTENTS_STATUS:-200}"
        ;;
    *) echo "Unexpected curl URL: $url" >&2; exit 1 ;;
esac
SH
chmod +x "$TEST_DIR/bin/curl"
export PATH="$TEST_DIR/bin:$PATH"
export CURL_LOG_DIR="$TEST_DIR/log"
export UPDATE_GITHUB_TOKEN=test-update-secret GITHUB_TOKEN=github-fallback GH_TOKEN=gh-fallback
mkdir -p "$CURL_LOG_DIR"

source <(head -n -1 "$ROOT_DIR/deploy/install.sh")
CONFIG_DIR="$TEST_DIR/config"
SYSTEMD_DIR="$TEST_DIR/systemd"
github_release_asset_download 'https://api.github.com/repos/mizaawa/zayuapi/releases/assets/1' "$TEST_DIR/archive"
test "$(cat "$TEST_DIR/archive")" = 'release payload'
grep -Fxq 'header = "Authorization: Bearer test-update-secret"' "$CURL_LOG_DIR/0.stdin"
grep -Fxq 'Accept: application/octet-stream' "$CURL_LOG_DIR/0.args"
test ! -e "$CURL_LOG_DIR/1.stdin"
if grep -Eq 'test-update-secret|github-fallback|gh-fallback' "$CURL_LOG_DIR/"*.args "$CURL_LOG_DIR/"*.env; then
    echo "GitHub token leaked into curl argv or environment" >&2
    exit 1
fi
for unsafe_redirect in 'https://example.com/collect' 'http://release-assets.githubusercontent.com/asset' 'https://api.github.com/other'; do
    export ASSET_REDIRECT="$unsafe_redirect"
    if github_release_asset_download 'https://api.github.com/repos/mizaawa/zayuapi/releases/assets/1' "$TEST_DIR/archive" 2>/dev/null; then
        echo "Accepted an unsafe asset redirect" >&2
        exit 1
    fi
done
unset ASSET_REDIRECT
test "$(find "$CURL_LOG_DIR" -name '*.args' | wc -l | tr -d ' ')" -eq 5

printf '%s\n' '{"assets":[{"name":"wanted.tar.gz","url":"https://api.github.com/repos/mizaawa/zayuapi/releases/assets/42"}]}' > "$TEST_DIR/release.json"
test "$(release_asset_url "$TEST_DIR/release.json" wanted.tar.gz)" = 'https://api.github.com/repos/mizaawa/zayuapi/releases/assets/42'
if release_asset_url "$TEST_DIR/release.json" missing.tar.gz 2>/dev/null; then
    echo "Accepted missing release asset" >&2
    exit 1
fi
printf '%s\n' '{"assets":[{"name":"wanted.tar.gz","url":"https://example.com/collect"}]}' > "$TEST_DIR/release.json"
if release_asset_url "$TEST_DIR/release.json" wanted.tar.gz 2>/dev/null; then
    echo "Accepted untrusted release asset API URL" >&2
    exit 1
fi
printf 'release archive payload' > "$TEST_DIR/archive"
archive_hash=$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest())' "$TEST_DIR/archive")
printf '%s  wanted.tar.gz\n' "$archive_hash" > "$TEST_DIR/checksums.txt"
verify_release_checksum "$TEST_DIR/archive" "$TEST_DIR/checksums.txt" wanted.tar.gz
printf '%064d  wanted.tar.gz\n' 0 > "$TEST_DIR/checksums.txt"
if verify_release_checksum "$TEST_DIR/archive" "$TEST_DIR/checksums.txt" wanted.tar.gz 2>/dev/null; then
    echo "Accepted invalid release checksum" >&2
    exit 1
fi

# Check persistence without requiring root privileges or a running systemd.
chown() { :; }
systemctl() { :; }
persist_update_token
test "$(cat "$CONFIG_DIR/update.env")" = 'UPDATE_GITHUB_TOKEN=test-update-secret'
if [ "$(uname -s)" = Linux ]; then
    test "$(stat -c %a "$CONFIG_DIR/update.env")" = 600
fi
unset UPDATE_GITHUB_TOKEN
load_update_token
test "$UPDATE_GITHUB_TOKEN" = test-update-secret
configure_existing_update_environment
grep -Fxq "EnvironmentFile=-$CONFIG_DIR/update.env" "$SYSTEMD_DIR/sub2api.service.d/update.conf"
SERVER_HOST=127.0.0.1 SERVER_PORT=9090
install_service >/dev/null
grep -Fxq "EnvironmentFile=-$CONFIG_DIR/update.env" "$SYSTEMD_DIR/sub2api.service"
if grep -Fq test-update-secret "$SYSTEMD_DIR/sub2api.service"; then
    echo "Token embedded directly into systemd unit" >&2
    exit 1
fi
grep -Fxq 'EnvironmentFile=-/etc/sub2api/update.env' "$ROOT_DIR/deploy/sub2api.service"

source <(head -n -1 "$ROOT_DIR/deploy/docker-deploy.sh")
SCRIPT_DIR="$TEST_DIR/source"
cd "$TEST_DIR/output"
printf 'local compose' > "$SCRIPT_DIR/docker-compose.local.yml"
printf 'local environment' > "$SCRIPT_DIR/.env.example"
unset UPDATE_GITHUB_TOKEN
prepare_deployment_file docker-compose.local.yml docker-compose.yml
prepare_deployment_file .env.example .env.example
cmp "$SCRIPT_DIR/docker-compose.local.yml" docker-compose.yml
cmp "$SCRIPT_DIR/.env.example" .env.example
SCRIPT_DIR="$TEST_DIR/missing"
if prepare_deployment_file docker-compose.local.yml docker-compose.yml >/dev/null; then
    echo "Private deployment fallback succeeded without token" >&2
    exit 1
fi
export UPDATE_GITHUB_TOKEN=test-update-secret
prepare_deployment_file docker-compose.local.yml docker-compose.yml
test "$(cat docker-compose.yml)" = 'private file content'
export CONTENTS_STATUS=302
if prepare_deployment_file docker-compose.local.yml docker-compose.yml >/dev/null; then
    echo "Private Contents API unexpectedly accepted redirect" >&2
    exit 1
fi
test "$(cat docker-compose.yml)" = 'private file content'
unset CONTENTS_STATUS
if grep -Eq 'test-update-secret|github-fallback|gh-fallback' "$CURL_LOG_DIR/"*.args "$CURL_LOG_DIR/"*.env; then
    echo "Private deployment token leaked into curl argv or environment" >&2
    exit 1
fi
if grep -Fxq -- '-L' "$CURL_LOG_DIR/"*.args; then
    echo "Authenticated curl invocation enabled redirects" >&2
    exit 1
fi

cd "$ROOT_DIR"
echo 'private deployment checks passed'
