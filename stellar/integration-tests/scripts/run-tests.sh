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

cd "$SCRIPT_DIR/../.."
PROJECT_ROOT=$(pwd)

# Convert to absolute path, only Unix
if [[ "$WORMHOLE_WASM_PATH" != /* ]]; then
  export WORMHOLE_WASM_PATH="$PROJECT_ROOT/$WORMHOLE_WASM_PATH"
fi

echo "Building contracts using stellar contract build..."
rm -f target/wasm32v1-none/release/wormhole_contract.wasm
stellar contract build
# Keep a copy of the original WASM
cp target/wasm32v1-none/release/wormhole_contract.wasm target/wasm32v1-none/release/wormhole_contract_original.wasm

# Build a version for upgrade test, with different chain ID
echo "Building upgraded contract version for integration tests..."
LIB_RS="contracts/wormhole-contract/src/lib.rs"
cp "$LIB_RS" "$LIB_RS.bak"
sed -i '' 's/u32::from(CHAIN_ID_STELLAR)/999u32/' "$LIB_RS"
stellar contract build
cp target/wasm32v1-none/release/wormhole_contract.wasm target/wasm32v1-none/release/wormhole_contract_upgrade.wasm

# Restore original source and WASM file
mv "$LIB_RS.bak" "$LIB_RS"
touch "$LIB_RS"
cp target/wasm32v1-none/release/wormhole_contract_original.wasm target/wasm32v1-none/release/wormhole_contract.wasm

export WORMHOLE_UPGRADE_WASM_PATH="$PROJECT_ROOT/target/wasm32v1-none/release/wormhole_contract_upgrade.wasm"

echo "Running integration tests..."
# localnet tests are ignored with cargo test
cargo test -p integration-tests -- --ignored --nocapture
