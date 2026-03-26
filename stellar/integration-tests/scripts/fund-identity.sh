#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/../.env.localnet"

die() {
  echo "$*" >&2
  exit 1
}

[ -f "$ENV_FILE" ] || die "Environment file $ENV_FILE not found"

# Load localnet defaults for funding the configured identity.
# shellcheck disable=SC1091
source "$ENV_FILE"

: "${STELLAR_NETWORK:?STELLAR_NETWORK not set}"
: "${STELLAR_IDENTITY:?STELLAR_IDENTITY not set}"
: "${SOROBAN_RPC_URL:?SOROBAN_RPC_URL not set}"
: "${STELLAR_NETWORK_PASSPHRASE:?STELLAR_NETWORK_PASSPHRASE not set}"

echo "Configuring stellar network: $STELLAR_NETWORK"
stellar network rm "$STELLAR_NETWORK" >/dev/null 2>&1 || true
stellar network add "$STELLAR_NETWORK" \
  --rpc-url "$SOROBAN_RPC_URL" \
  --network-passphrase "$STELLAR_NETWORK_PASSPHRASE"

if ! stellar keys address "$STELLAR_IDENTITY" >/dev/null 2>&1; then
  echo "Generating identity: $STELLAR_IDENTITY"
  stellar keys generate --network "$STELLAR_NETWORK" "$STELLAR_IDENTITY"
fi

IDENTITY_ADDR="$(stellar keys address "$STELLAR_IDENTITY")"
IDENTITY_ADDR="$(echo "$IDENTITY_ADDR" | tr -d '\r\n')"
echo "Funding identity $STELLAR_IDENTITY ($IDENTITY_ADDR) via friendbot..."
# Retry on 502 (friendbot may still be starting)
FRIENDBOT_RESPONSE=""
HTTP_CODE=""
for i in 1 2 3 4 5 6 7 8 9 10; do
  FRIENDBOT_RESPONSE=$(curl -s -w "\n%{http_code}" "$STELLAR_FRIENDBOT_URL?addr=$IDENTITY_ADDR")
  HTTP_CODE=$(echo "$FRIENDBOT_RESPONSE" | tail -n1)
  if [[ "$HTTP_CODE" == "200" ]]; then
    break
  fi
  if [[ "$HTTP_CODE" != "502" ]]; then
    break
  fi
  echo "Friendbot not ready (502), retrying in 3s..."
  sleep 3
done
BODY=$(echo "$FRIENDBOT_RESPONSE" | sed '$d')
if [[ "$HTTP_CODE" != "200" ]]; then
  echo "Friendbot request failed (HTTP $HTTP_CODE). Response: $BODY"
  exit 1
fi
if echo "$BODY" | grep -qE '"successful"[[:space:]]*:[[:space:]]*false'; then
  echo "Friendbot reported failure. Response: $BODY"
  exit 1
fi
echo "Waiting for account to be available on ledger..."
sleep 2

echo "Identity $STELLAR_IDENTITY is ready and funded!"
