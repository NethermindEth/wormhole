#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/../.env.localnet"

die() {
  echo "$*" >&2
  exit 1
}

[ -f "$ENV_FILE" ] || die "Environment file $ENV_FILE not found"

# Load RPC and friendbot endpoints for the local Docker network.
# shellcheck disable=SC1091
source "$ENV_FILE"

: "${SOROBAN_RPC_URL:?SOROBAN_RPC_URL not set}"
: "${STELLAR_FRIENDBOT_URL:?STELLAR_FRIENDBOT_URL not set}"

echo "Stopping existing stellar_localnet container if any..."
docker stop stellar_localnet >/dev/null 2>&1 || true

echo "Starting Stellar localnet..."
docker run --rm -d \
  -p 8000:8000 \
  --name stellar_localnet \
  stellar/quickstart:latest \
  --local

echo "Waiting for RPC to be ready..."
# Poll Soroban RPC until ledger queries succeed.
until curl -s "$SOROBAN_RPC_URL" -X POST -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","id":1,"method":"getLatestLedger","params":{}}' | grep -q "result"; do
  printf "."
  sleep 2
done
echo ""

echo "Waiting for friendbot to be ready..."
# Friendbot starts after Horizon; accept both 502 and connection errors as
# transient while the stack is still booting.
until FRIENDBOT_CODE=$(curl -s -o /dev/null -w "%{http_code}" "${STELLAR_FRIENDBOT_URL}?addr=GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAUF"); [[ "$FRIENDBOT_CODE" != "502" && "$FRIENDBOT_CODE" != "000" ]]; do
  printf "."
  sleep 2
done
echo -e "\nLocalnet is ready!"
