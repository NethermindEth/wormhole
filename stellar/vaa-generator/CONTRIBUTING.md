# Contributing to VAA Generator

## Adding New Test Cases

### For Existing Actions

Edit the appropriate file in `src/test-cases/`:

```typescript
// Example: Add new contract upgrade test
export function generateMyNewTest(guardians: MockGuardians): TestVAA {
  const payload = buildContractUpgradePayload({
    chain: CHAIN_ID_STELLAR,
    newContractHash: "your_hash_here",
  });

  const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(13), {
    payload,
    emitterChain: GOVERNANCE_CHAIN_ID,
    emitterAddress: GOVERNANCE_EMITTER,
    sequence: 99,
  });

  return {
    name: "my_new_test",
    description: "Tests XYZ scenario",
    vaa: {
      hex: toHex(vaaBuffer),
      base64: toBase64(vaaBuffer),
    },
    payload: parsePayload(payload),
    expectedResult: "success" or "error",
  };
}
```

Then add to the test suite in `generateContractUpgradeTests()`.

### For New Actions

1. Create file in `src/test-cases/new-action.ts`
2. Implement test generators
3. Add to `src/index.ts` main() function
4. Run `npm run generate`

## Updating V2 WASM Hash

When contract code changes:

```bash
# Build V2
cd contracts/wormhole-contract
stellar contract build

# Get hash
shasum -a 256 ../../wormhole_contract_v2.wasm

# Update hash in src/test-cases/contract-upgrade-tests.ts
const v2WasmHash = "new_hash_here";

# Regenerate VAAs
cd ../../vaa-generator
npm run generate
```

**Or** just run the integration tests - they auto-update!

## Code Style

- Use descriptive test names (snake_case)
- Add comments explaining test purpose
- Follow existing patterns for consistency
- Run `npm run build` before committing

## Testing Your Changes

```bash
# Build
npm run build

# Generate
npm run generate

# Verify output
ls -lh generated/

# Check VAA counts
jq '.testCases | length' generated/*_vaas.json
```

## Common Patterns

### Invalid Governance VAA

```typescript
const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(13), {
  payload,
  emitterChain: 99,  // Wrong chain!
  emitterAddress: GOVERNANCE_EMITTER,
});
```

### Wrong Module

```typescript
const payload = Buffer.concat([
  Buffer.from("0000...0000436f7264", "hex"),  // "Cord" not "Core"
  // ... rest of payload
]);
```

### Insufficient Signatures

```typescript
const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(5), {
  // Only 5 signatures (need 13 for quorum)
  payload,
});
```
