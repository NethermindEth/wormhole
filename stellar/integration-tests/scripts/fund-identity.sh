#!/bin/bash
set -e

# Environment
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/../.env.localnet"

if [ -f "$ENV_FILE" ]; then
  set -a
  source "$ENV_FILE"
  set +a
else
  echo "Environment file $ENV_FILE not found"
  exit 1
fi

echo "Configuring stellar network: $STELLAR_NETWORK"
stellar network rm "$STELLAR_NETWORK" >/dev/null 2>&1 || true
stellar network add "$STELLAR_NETWORK" \
  --rpc-url "$SOROBAN_RPC_URL" \
  --network-passphrase "$STELLAR_NETWORK_PASSPHRASE"

if ! stellar keys address "$STELLAR_IDENTITY" >/dev/null 2>&1; then
  echo "Generating identity: $STELLAR_IDENTITY"
  stellar keys generate --network "$STELLAR_NETWORK" "$STELLAR_IDENTITY"
else
  echo "Identity $STELLAR_IDENTITY already exists"
  IDENTITY_ADDR=$(stellar keys address "$STELLAR_IDENTITY")
  echo "Funding identity $STELLAR_IDENTITY ($IDENTITY_ADDR) via friendbot..."
  curl -s "$STELLAR_FRIENDBOT_URL?addr=$IDENTITY_ADDR" > /dev/null
fi

echo "Identity $STELLAR_IDENTITY is ready and funded!"
