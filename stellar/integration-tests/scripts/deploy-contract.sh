#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ENV_FILE="$SCRIPT_DIR/../.env.localnet"

die() {
  echo "$*" >&2
  exit 1
}

[ -f "$ENV_FILE" ] || die "Environment file $ENV_FILE not found"

# Load localnet defaults for deploy-time network, identity, and wasm path.
# shellcheck disable=SC1091
source "$ENV_FILE"

: "${STELLAR_NETWORK:?STELLAR_NETWORK not set}"
: "${STELLAR_IDENTITY:?STELLAR_IDENTITY not set}"
: "${SOROBAN_RPC_URL:?SOROBAN_RPC_URL not set}"
: "${STELLAR_NETWORK_PASSPHRASE:?STELLAR_NETWORK_PASSPHRASE not set}"
: "${WORMHOLE_WASM_PATH:?WORMHOLE_WASM_PATH not set}"

if [[ "$WORMHOLE_WASM_PATH" != /* ]]; then
  # Resolve the configured wasm path relative to the workspace root.
  WORMHOLE_WASM_PATH="$PROJECT_ROOT/$WORMHOLE_WASM_PATH"
fi

# Constructor args: one guardian (20-byte hex) and governance emitter
# (32-byte hex) for localnet.
INITIAL_GUARDIANS='["0101010101010101010101010101010101010101"]'
GOVERNANCE_EMITTER="0404040404040404040404040404040404040404040404040404040404040404"

echo "Configuring stellar network: $STELLAR_NETWORK"
stellar network rm "$STELLAR_NETWORK" >/dev/null 2>&1 || true
stellar network add "$STELLAR_NETWORK" \
  --rpc-url "$SOROBAN_RPC_URL" \
  --network-passphrase "$STELLAR_NETWORK_PASSPHRASE"

if [ ! -f "$WORMHOLE_WASM_PATH" ]; then
  echo "WASM file not found at $WORMHOLE_WASM_PATH. Building contracts..."
  (
    cd "$PROJECT_ROOT"
    stellar contract build
  )
fi

echo "Deploying contract to $STELLAR_NETWORK..."
CONTRACT_ID=$(stellar contract deploy \
  --network "$STELLAR_NETWORK" \
  --source "$STELLAR_IDENTITY" \
  --wasm "$WORMHOLE_WASM_PATH" \
  -- \
  --initial_guardians "$INITIAL_GUARDIANS" \
  --governance_emitter "$GOVERNANCE_EMITTER")

echo "------------------------------------------------------"
echo "Contract deployed successfully!"
echo "Contract ID: $CONTRACT_ID"
echo "------------------------------------------------------"
echo "You can use this ID to interact with the contract."
