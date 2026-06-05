/**
 * Guardian Set Upgrade VAA generator for testing
 */

import { GuardianSet, TestVAA } from "../types";
import {
  GOVERNANCE_CHAIN_ID,
  GOVERNANCE_EMITTER,
  MODULE_CORE,
  ACTION_GUARDIAN_SET_UPGRADE,
  CHAIN_ID_STELLAR,
  DEFAULT_VAA_PARAMS,
} from "../config";
import { getMockGuardiansForSet, getQuorumSignerIndices } from "../guardian-sets/generator";
import { createGovernanceVAA } from "../builders/governance-vaa-builder";

/**
 * Build guardian set upgrade payload
 */
export function buildGuardianSetUpgradePayload(
  newIndex: number,
  newGuardianAddresses: string[],
  targetChain: number = CHAIN_ID_STELLAR
): Buffer {
  const payloadArr: number[] = [];

  // Module (32 bytes)
  const moduleBytes = Buffer.from(MODULE_CORE, "hex");
  for (const byte of moduleBytes) {
    payloadArr.push(byte);
  }

  // Action (1 byte)
  payloadArr.push(ACTION_GUARDIAN_SET_UPGRADE);

  // Chain ID (2 bytes, big-endian)
  payloadArr.push((targetChain >> 8) & 0xff);
  payloadArr.push(targetChain & 0xff);

  // New guardian set index (4 bytes, big-endian)
  payloadArr.push((newIndex >> 24) & 0xff);
  payloadArr.push((newIndex >> 16) & 0xff);
  payloadArr.push((newIndex >> 8) & 0xff);
  payloadArr.push(newIndex & 0xff);

  // Number of guardians (1 byte)
  payloadArr.push(newGuardianAddresses.length);

  // Guardian addresses (20 bytes each)
  for (const address of newGuardianAddresses) {
    const addressBytes = Buffer.from(address.substring(2), "hex"); // Remove 0x
    for (const byte of addressBytes) {
      payloadArr.push(byte);
    }
  }

  return Buffer.from(payloadArr);
}

/**
 * Generate a guardian set upgrade VAA
 */
export function generateGuardianSetUpgradeVAA(
  fromSet: GuardianSet,
  toSet: GuardianSet,
  targetChain: number = CHAIN_ID_STELLAR
): TestVAA {
  // Get MockGuardians for the current set (signing the upgrade)
  const mockGuardians = getMockGuardiansForSet(fromSet);

  // Get quorum signers for the current set
  const signerIndices = getQuorumSignerIndices(fromSet.guardians.length);

  // Build upgrade payload
  const payload = buildGuardianSetUpgradePayload(
    toSet.index,
    toSet.guardians.map(g => g.ethereumAddress),
    targetChain
  );

  // Create VAA using helper
  const vaaBytes = createGovernanceVAA(
    mockGuardians,
    signerIndices,
    payload,
    toSet.index // Use new index as sequence for uniqueness
  );

  return {
    name: `guardian_set_upgrade_${fromSet.index}_to_${toSet.index}`,
    description: `Upgrade guardian set from index ${fromSet.index} (${fromSet.guardians.length} guardians) to index ${toSet.index} (${toSet.guardians.length} guardians)`,
    category: "happy_path",
    expectedResult: "success",
    expectedError: null,
    vaa: {
      base64: vaaBytes.toString("base64"),
      hex: vaaBytes.toString("hex"),
      length: vaaBytes.length,
    },
    payload: {
      module: "Core",
      action: ACTION_GUARDIAN_SET_UPGRADE,
      chain: targetChain,
      newGuardianSetIndex: toSet.index,
      numGuardians: toSet.guardians.length,
      guardianAddresses: toSet.guardians.map(g => g.ethereumAddress),
    },
    metadata: {
      guardianSetIndex: fromSet.index,
      numSignatures: signerIndices.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: DEFAULT_VAA_PARAMS.emitterChain,
      sequence: toSet.index,
    },
  };
}

/**
 * Generate invalid guardian set upgrade VAAs for testing
 */
export function generateInvalidGuardianUpgradeVAAs(
  currentSet: GuardianSet
): TestVAA[] {
  const testCases: TestVAA[] = [];
  const mockGuardians = getMockGuardiansForSet(currentSet);
  const signerIndices = getQuorumSignerIndices(currentSet.guardians.length);

  // 1. Skip index (try to jump from current to current+2)
  const skipIndexPayload = buildGuardianSetUpgradePayload(
    currentSet.index + 2,
    ["0x1111111111111111111111111111111111111111"],
    CHAIN_ID_STELLAR
  );

  const skipIndexVaa = createGovernanceVAA(
    mockGuardians,
    signerIndices,
    skipIndexPayload,
    100 // Unique sequence
  );

  testCases.push({
    name: "guardian_upgrade_skip_index",
    description: "Attempt to skip guardian set index (non-sequential)",
    category: "validation_error",
    expectedResult: "error",
    expectedError: "InvalidGuardianSetIndex",
    vaa: {
      base64: skipIndexVaa.toString("base64"),
      hex: skipIndexVaa.toString("hex"),
      length: skipIndexVaa.length,
    },
    payload: {
      module: "Core",
      action: ACTION_GUARDIAN_SET_UPGRADE,
      chain: CHAIN_ID_STELLAR,
      newGuardianSetIndex: currentSet.index + 2,
      numGuardians: 1,
      guardianAddresses: ["0x1111111111111111111111111111111111111111"],
    },
    metadata: {
      guardianSetIndex: currentSet.index,
      numSignatures: signerIndices.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: DEFAULT_VAA_PARAMS.emitterChain,
      sequence: 100,
    },
  });

  // 2. Empty guardian set
  const emptySetPayload = buildGuardianSetUpgradePayload(
    currentSet.index + 1,
    [], // No guardians
    CHAIN_ID_STELLAR
  );

  const emptySetVaa = createGovernanceVAA(
    mockGuardians,
    signerIndices,
    emptySetPayload,
    101 // Unique sequence
  );

  testCases.push({
    name: "guardian_upgrade_empty_set",
    description: "Attempt to upgrade to empty guardian set",
    category: "validation_error",
    expectedResult: "error",
    expectedError: "EmptyGuardianSet",
    vaa: {
      base64: emptySetVaa.toString("base64"),
      hex: emptySetVaa.toString("hex"),
      length: emptySetVaa.length,
    },
    payload: {
      module: "Core",
      action: ACTION_GUARDIAN_SET_UPGRADE,
      chain: CHAIN_ID_STELLAR,
      newGuardianSetIndex: currentSet.index + 1,
      numGuardians: 0,
      guardianAddresses: [],
    },
    metadata: {
      guardianSetIndex: currentSet.index,
      numSignatures: signerIndices.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: DEFAULT_VAA_PARAMS.emitterChain,
      sequence: 101,
    },
  });

  // 3. Wrong chain (not Stellar or 0)
  const wrongChainPayload = buildGuardianSetUpgradePayload(
    currentSet.index + 1,
    ["0x2222222222222222222222222222222222222222"],
    2 // Ethereum chain ID
  );

  const wrongChainVaa = createGovernanceVAA(
    mockGuardians,
    signerIndices,
    wrongChainPayload,
    102 // Unique sequence
  );

  testCases.push({
    name: "guardian_upgrade_wrong_chain",
    description: "Guardian upgrade for wrong chain (Ethereum instead of Stellar)",
    category: "validation_error",
    expectedResult: "error",
    expectedError: "InvalidGovernanceChain",
    vaa: {
      base64: wrongChainVaa.toString("base64"),
      hex: wrongChainVaa.toString("hex"),
      length: wrongChainVaa.length,
    },
    payload: {
      module: "Core",
      action: ACTION_GUARDIAN_SET_UPGRADE,
      chain: 2,
      newGuardianSetIndex: currentSet.index + 1,
      numGuardians: 1,
      guardianAddresses: ["0x2222222222222222222222222222222222222222"],
    },
    metadata: {
      guardianSetIndex: currentSet.index,
      numSignatures: signerIndices.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: DEFAULT_VAA_PARAMS.emitterChain,
      sequence: 102,
    },
  });

  return testCases;
}