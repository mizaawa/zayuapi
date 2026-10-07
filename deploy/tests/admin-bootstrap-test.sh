#!/bin/bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)

# Load helper functions without starting deployment or touching an existing .env.
source <(head -n -1 "$ROOT_DIR/deploy/docker-deploy.sh")

first_email=$(generate_admin_email)
second_email=$(generate_admin_email)
[[ "$first_email" =~ ^admin-[0-9a-f]{12}@sub2api\.local$ ]]
[[ "$second_email" =~ ^admin-[0-9a-f]{12}@sub2api\.local$ ]]
[[ "$first_email" != "$second_email" ]]

if (openssl() { return 1; }; generate_admin_email); then
    echo "admin email generation accepted a failed random source" >&2
    exit 1
fi
if (openssl() { return 0; }; generate_admin_email); then
    echo "admin email generation accepted an empty random value" >&2
    exit 1
fi

grep -Fxq 'ADMIN_EMAIL=' "$ROOT_DIR/deploy/.env.example"
for compose_file in docker-compose.yml docker-compose.local.yml docker-compose.dev.yml docker-compose.standalone.yml; do
    grep -Fq 'ADMIN_EMAIL=${ADMIN_EMAIL:-}' "$ROOT_DIR/deploy/$compose_file"
done

echo "Admin bootstrap deployment checks passed."
