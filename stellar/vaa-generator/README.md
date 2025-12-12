# Wormhole VAA Generator for Stellar/Soroban

Generates **cryptographically signed test VAAs** (Verifiable Action Approvals) for testing the Wormhole Core Contract on Stellar.

## What It Does

This TypeScript tool generates complete test suites of signed VAAs for all 4 Wormhole governance actions:

1. **Contract Upgrade** (Action 1) - WASM hash updates
2. **Guardian Set Upgrade** (Action 2) - Guardian key rotation
3. **Set Message Fee** (Action 3) - Fee configuration
4. **Transfer Fees** (Action 4) - Fee withdrawals

Each action includes:
- ✅ Valid test cases (happy paths)
- ❌ Invalid test cases (validation errors)
- ⚠️  Edge cases (boundary conditions)

**Total Generated**: ~50+ signed VAAs across all governance actions

## Why This Exists

**Problem**: Testing Wormhole governance requires valid guardian signatures. Getting 13 real guardian signatures for every test is impossible.

**Solution**: Use `MockGuardians` from Wormhole SDK to sign test VAAs with deterministic test keys.

**Result**: Complete test coverage for all governance actions with real signatures.

## Quick Start

```bash
# Install dependencies
cd vaa-generator
npm install

# Generate all VAAs
npm run generate
```

**Output**: All VAAs generated in `generated/` directory as JSON files.

## Generated Files

### Guardian Sets

Three guardian sets for testing upgrade paths:

- **`guardian_keys_gs0.json`** - 3 guardians (initial deployment)
- **`guardian_keys_gs1.json`** - 7 guardians (first upgrade)
- **`guardian_keys_gs2.json`** - 19 guardians (production scale)
- **`guardian_keys.json`** - Legacy file (= GS2 for backward compatibility)

### VAA Test Suites

- **`contract_upgrade_vaas.json`** - Contract WASM updates (10 test cases)
- **`guardian_set_upgrade_vaas.json`** - Guardian key rotation (GS0→GS1→GS2 + invalid)
- **`set_message_fee_vaas.json`** - Fee configuration (0, 0.5, 1, 10 XLM, max u64)
- **`transfer_fees_vaas.json`** - Fee withdrawals (various amounts)
- **`transfer_fees_testnet_vaas.json`** - Testnet-specific transfers (with real addresses)
- **`edge_cases_vaas.json`** - Edge cases (wrong governance, signature issues, etc.)

## Integration with Tests

The integration test script (`../run-integration-tests.sh` at project root) automatically:

1. **Builds V2 WASM** with modified `GOVERNANCE_CHAIN_ID`
2. **Calculates V2 hash** via `shasum`
3. **Checks if VAA hash matches** V2 hash
4. **Auto-regenerates VAAs** if hash mismatch detected:
   ```bash
   # Updates contract-upgrade-tests.ts with new hash
   sed -i '' 's/const v2WasmHash = "[a-f0-9]*"/const v2WasmHash = "$V2_HASH"/' \
     vaa-generator/src/test-cases/contract-upgrade-tests.ts

   # Regenerates all VAAs
   cd vaa-generator && npm run generate
   ```

**Key Point**: When contract code changes, V2 hash changes. The integration test detects this and **automatically regenerates VAAs** with the correct hash.

## Architecture

```
vaa-generator/
├── src/
│   ├── index.ts                      # Main generator (orchestrates all phases)
│   ├── config.ts                     # Constants (chain IDs, governance, etc.)
│   ├── types.ts                      # TypeScript types (TestVAA, GuardianSet, etc.)
│   ├── guardian.ts                   # Guardian utilities (signer indices, etc.)
│   │
│   ├── builders/                     # VAA Construction
│   │   ├── vaa-builder.ts           # Core VAA signing logic
│   │   ├── governance-vaa-builder.ts # Governance-specific builders
│   │   └── contract-upgrade.ts       # Payload builders for Action 1
│   │
│   ├── guardian-sets/                # Guardian Set Management
│   │   └── generator.ts             # Generate GS0, GS1, GS2
│   │
│   ├── test-cases/                   # VAA Generators by Action
│   │   ├── contract-upgrade-tests.ts # Action 1 test cases
│   │   ├── guardian-upgrade.ts       # Action 2 test cases
│   │   ├── set-message-fee.ts        # Action 3 test cases
│   │   ├── transfer-fees.ts          # Action 4 test cases (generic)
│   │   ├── transfer-fees-testnet.ts  # Action 4 test cases (testnet)
│   │   └── edge-cases.ts             # Cross-action edge cases
│   │
│   └── output/                       # Output Writers
│       ├── json-generator.ts        # Write VAAs to JSON
│       └── rust-generator.ts        # Write VAAs to Rust (legacy)
│
├── generated/                         # Output directory
│   ├── guardian_keys_gs*.json        # Guardian sets
│   └── *_vaas.json                   # VAA test suites
│
├── package.json
├── tsconfig.json
└── .gitignore
```

## How It Works

### Phase 1: Guardian Set Generation

Generates 3 deterministic guardian sets using Wormhole SDK:

```typescript
const gs0 = new MockGuardians(0, [/* 3 test keys */]);
const gs1 = new MockGuardians(1, [/* 7 test keys */]);
const gs2 = new MockGuardians(2, [/* 19 test keys */]);
```

**Quorum**: `(numGuardians * 2 / 3) + 1`
- GS0: 3 guardians → 3 signatures required (100%)
- GS1: 7 guardians → 5 signatures required
- GS2: 19 guardians → 13 signatures required

### Phase 2-6: VAA Generation

For each governance action:

1. **Build Payload** - Action-specific data (module, action, chain, payload)
2. **Sign with MockGuardians** - Get 13+ guardian signatures
3. **Create TestVAA** - Metadata + hex + base64 encodings
4. **Write to JSON** - Output for integration tests

Example (Contract Upgrade):

```typescript
const payload = buildContractUpgradePayload({
  chain: CHAIN_ID_STELLAR,  // 61
  newContractHash: v2WasmHash,
});

const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(13), {
  payload,
  timestamp: 1700000000,  // Fixed for determinism
  nonce: 1,
  emitterChainId: GOVERNANCE_CHAIN_ID,  // 1 (Solana)
  emitterAddress: GOVERNANCE_EMITTER,    // 0x...04
  sequence: 1,
});
```

## Test Guardian Keys

**⚠️ CRITICAL: FOR TESTING ONLY**

The guardian keys are **deterministic test keys** from Wormhole SDK:

```typescript
const guardians = new MockGuardians(guardianSetIndex, [/* keys */]);
```

**Properties**:
- ✅ Deterministic (same keys every time)
- ✅ Valid ECDSA secp256k1 keys
- ✅ Real signatures (not mocked)
- ❌ **NEVER use in production**

**Integration**: Initialize contract with matching keys from `guardian_keys_gs0.json` for tests to pass.

## Generated Test Cases

### Contract Upgrade (Action 1)

| Test Case | Type | Expected | Purpose |
|-----------|------|----------|---------|
| `valid_stellar_specific` | Valid | Success | Stellar chain (61) |
| `valid_all_chains` | Valid | Success | All chains (0) |
| `wrong_module` | Invalid | Error #30 | Module validation |
| `wrong_action` | Invalid | Error #31 | Action validation |
| `wrong_chain_ethereum` | Invalid | Error #32 | Chain validation |
| `wrong_chain_random` | Invalid | Error #32 | Chain validation |
| `payload_too_short` | Invalid | Error #40 | Size validation |
| `payload_too_long` | Edge | Success | Extra bytes ignored |
| `zero_hash` | Edge | Success | Boundary test (0x00...00) |
| `max_hash` | Edge | Success | Boundary test (0xff...ff) |

### Guardian Set Upgrade (Action 2)

| Test Case | Type | Expected | Purpose |
|-----------|------|----------|---------|
| `guardian_set_upgrade_0_to_1` | Valid | Success | GS0 → GS1 (3 → 7) |
| `guardian_set_upgrade_1_to_2` | Valid | Success | GS1 → GS2 (7 → 19) |
| `skip_index` | Invalid | Error #34 | Sequential only |
| `empty_guardian_set` | Invalid | Error #35 | Non-empty check |
| `duplicate_keys` | Edge | Success | Deduplication test |

### Set Message Fee (Action 3)

| Test Case | Type | Expected | Purpose |
|-----------|------|----------|---------|
| `set_message_fee_zero_fee` | Valid | Success | Free messages |
| `set_message_fee_0.5_xlm` | Valid | Success | 5M stroops |
| `set_message_fee_1_xlm` | Valid | Success | 10M stroops |
| `set_message_fee_10_xlm` | Valid | Success | 100M stroops |
| `set_message_fee_max_u64` | Edge | Success | u64::MAX |
| Invalid cases | Invalid | Errors | Module/action/chain checks |

### Transfer Fees (Action 4)

| Test Case | Type | Expected | Purpose |
|-----------|------|----------|---------|
| `transfer_fees_0.5_xlm` | Valid | Success | 5M stroops |
| `transfer_fees_1_xlm` | Valid | Success | 10M stroops |
| `transfer_fees_10_xlm` | Valid | Success | 100M stroops |
| `transfer_fees_100_xlm` | Valid | Success | 1B stroops |
| Testnet variants | Valid | Success | Real testnet addresses |
| Invalid cases | Invalid | Errors | Validation checks |

## Technical Details

### Dependencies

- **`@certusone/wormhole-sdk`** - MockGuardians for signing
- **`ethers@5`** - Signature recovery (v5 required for Wormhole SDK compatibility)

### Constants

```typescript
// Governance (Solana)
GOVERNANCE_CHAIN_ID = 1
GOVERNANCE_EMITTER = "0x0000...0004"
MODULE_CORE = "0x0000...436f7265" // Right-padded "Core"

// Stellar
CHAIN_ID_STELLAR = 61

// Signatures
QUORUM_SIGNATURES = 13  // For GS2 (19 guardians)
```

### Determinism

All VAAs use fixed values for reproducibility:
- **Timestamp**: `1700000000` (Nov 14, 2023)
- **Guardian indices**: `[0,1,2,...,12]` (first 13 guardians)
- **Sequence**: Incremented per action type

## Commands

```bash
# Install dependencies
npm install

# Build TypeScript
npm run build

# Generate all VAAs
npm run generate

# Clean output
npm run clean
```

## Troubleshooting

### "VAA hash mismatch" during integration tests

**Expected behavior**: The integration test script detects the mismatch and **auto-regenerates** VAAs with the correct V2 hash.

**If auto-regeneration fails**:
```bash
# Manually regenerate
cd vaa-generator
npm run generate

# Check V2 hash matches
shasum -a 256 ../wormhole_contract_v2.wasm
jq -r '.testCases[0].payload.newContractHash' generated/contract_upgrade_vaas.json
```

### VAAs don't verify in contract

**Cause**: Contract initialized with different guardian keys.

**Solution**: Use matching keys from `guardian_keys_gs0.json`:
```rust
// In integration tests
let guardian1 = "c6ef12cbab104f611924c1a66b542231b7261b13";
let guardian2 = "371d9330a9d31b0ce7a063abadad0e2978a691f3";
let guardian3 = "fee1c1102079fd09ce8984b52e00dab8c29d1e14";

contract.initialize(vec![guardian1, guardian2, guardian3], gov_emitter);
```

### Error: "InvalidGovernanceModule" (#30)

**Cause**: MODULE_CORE must be exactly 32 bytes.

**Correct format**: `00000000000000000000000000000000000000000000000000000000436f7265`
- 28 zero bytes (56 hex chars)
- "Core" (8 hex chars: 436f7265)
- Total: 64 hex chars = 32 bytes

### "Cannot find module '@certusone/wormhole-sdk'"

```bash
npm install
```

## For Production Use

**IMPORTANT**: This generator is for **testing only**.

For production:
1. ❌ **DO NOT** use these guardian keys
2. ❌ **DO NOT** use MockGuardians
3. ✅ **DO** use real Wormhole guardian network
4. ✅ **DO** use real governance VAAs from chain

## Related Files

- **Integration Tests**: `../run-integration-tests.sh` (at project root)
- **Generated VAAs**: `./generated/*.json` (single source of truth)
- **Contract**: `../contracts/wormhole-contract/src/`

## License

Same as parent project
