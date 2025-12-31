#!/bin/bash
set -e

# Load environment variables
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

# Change to project root
cd "$SCRIPT_DIR/../.."
PROJECT_ROOT=$(pwd)

if [[ "$WORMHOLE_WASM_PATH" != /* ]]; then
  export WORMHOLE_WASM_PATH="$PROJECT_ROOT/$WORMHOLE_WASM_PATH"
fi

# Ensure WASM is built
if [ ! -f "$WORMHOLE_WASM_PATH" ]; then
  echo "WASM file not found at $WORMHOLE_WASM_PATH. Building contracts..."
  stellar contract build
fi

echo "Deploying contract to $STELLAR_NETWORK..."
# We use the identity and network from the environment file
CONTRACT_ID=$(stellar contract deploy \
  --network "$STELLAR_NETWORK" \
  --source "$STELLAR_IDENTITY" \
  --wasm "$WORMHOLE_WASM_PATH")

echo "------------------------------------------------------"
echo "Contract deployed successfully!"
echo "Contract ID: $CONTRACT_ID"
echo "------------------------------------------------------"
echo "You can use this ID to interact with the contract."
