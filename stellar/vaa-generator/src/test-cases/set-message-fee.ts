/**
 * Set Message Fee VAA generator for testing
 */

import { GuardianSet, TestVAA } from "../types";
import {
  GOVERNANCE_CHAIN_ID,
  GOVERNANCE_EMITTER,
  MODULE_CORE,
  ACTION_SET_MESSAGE_FEE,
  CHAIN_ID_STELLAR,
  DEFAULT_VAA_PARAMS,
} from "../config";
import { getMockGuardiansForSet, getQuorumSignerIndices } from "../guardian-sets/generator";
import { createGovernanceVAA } from "../builders/governance-vaa-builder";

/**
 * Build set message fee payload
 * Format: module(32) + action(1) + chain(2) + padding(24) + fee(8)
 */
export function buildSetMessageFeePayload(
  fee: bigint,
  targetChain: number = CHAIN_ID_STELLAR
): Buffer {
  const payloadArr: number[] = [];

  // Module (32 bytes)
  const moduleBytes = Buffer.from(MODULE_CORE, "hex");
  for (const byte of moduleBytes) {
    payloadArr.push(byte);
  }

  // Action (1 byte)
  payloadArr.push(ACTION_SET_MESSAGE_FEE);

  // Chain ID (2 bytes, big-endian)
  payloadArr.push((targetChain >> 8) & 0xff);
  payloadArr.push(targetChain & 0xff);

  // Padding (24 bytes) - U256 compatibility with Ethereum
  for (let i = 0; i < 24; i++) {
    payloadArr.push(0);
  }

  // Fee (8 bytes, big-endian) - Last 8 bytes of U256
  for (let i = 7; i >= 0; i--) {
    payloadArr.push(Number((fee >> BigInt(i * 8)) & BigInt(0xff)));
  }

  return Buffer.from(payloadArr);
}

/**
 * Common fee amounts in stroops
 */
export const FEE_AMOUNTS = {
  ZERO: BigInt(0),
  ONE_STROOP: BigInt(1),
  ONE_XLM: BigInt(10_000_000), // 10^7 stroops
  HALF_XLM: BigInt(5_000_000),
  TEN_XLM: BigInt(100_000_000),
  MAX_U64: BigInt("18446744073709551615"), // 2^64 - 1
};

/**
 * Generate a set message fee VAA
 */
export function generateSetMessageFeeVAA(
  guardianSet: GuardianSet,
  fee: bigint,
  feeName: string,
  sequence: number = 200
): TestVAA {
  const mockGuardians = getMockGuardiansForSet(guardianSet);
  const signerIndices = getQuorumSignerIndices(guardianSet.guardians.length);

  const payload = buildSetMessageFeePayload(fee, CHAIN_ID_STELLAR);

  const vaaBytes = createGovernanceVAA(
    mockGuardians,
    signerIndices,
    payload,
    sequence
  );

  return {
    name: `set_message_fee_${feeName.toLowerCase().replace(/\s+/g, "_")}`,
    description: `Set message fee to ${feeName} (${fee.toString()} stroops)`,
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
      action: ACTION_SET_MESSAGE_FEE,
      chain: CHAIN_ID_STELLAR,
      fee: fee.toString(),
      feeInXLM: Number(fee) / 10_000_000,
    },
    metadata: {
      guardianSetIndex: guardianSet.index,
      numSignatures: signerIndices.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: DEFAULT_VAA_PARAMS.emitterChain,
      sequence,
    },
  };
}

/**
 * Generate all set message fee test VAAs
 */
export function generateSetMessageFeeTestCases(guardianSet: GuardianSet): TestVAA[] {
  const testCases: TestVAA[] = [];
  let sequence = 200;

  // 1. Set fee to 0 (disable fees)
  testCases.push(
    generateSetMessageFeeVAA(guardianSet, FEE_AMOUNTS.ZERO, "Zero Fee", sequence++)
  );

  // 2. Set fee to 1 XLM
  testCases.push(
    generateSetMessageFeeVAA(guardianSet, FEE_AMOUNTS.ONE_XLM, "1 XLM", sequence++)
  );

  // 3. Set fee to 0.5 XLM
  testCases.push(
    generateSetMessageFeeVAA(guardianSet, FEE_AMOUNTS.HALF_XLM, "0.5 XLM", sequence++)
  );

  // 4. Set fee to 10 XLM
  testCases.push(
    generateSetMessageFeeVAA(guardianSet, FEE_AMOUNTS.TEN_XLM, "10 XLM", sequence++)
  );

  // 5. Set fee to max u64
  testCases.push(
    generateSetMessageFeeVAA(guardianSet, FEE_AMOUNTS.MAX_U64, "Max U64", sequence++)
  );

  // 6. Set fee to 1 stroop (minimum non-zero)
  testCases.push(
    generateSetMessageFeeVAA(guardianSet, FEE_AMOUNTS.ONE_STROOP, "1 Stroop", sequence++)
  );

  return testCases;
}

/**
 * Generate invalid set message fee VAAs for testing
 */
export function generateInvalidSetMessageFeeVAAs(guardianSet: GuardianSet): TestVAA[] {
  const testCases: TestVAA[] = [];
  const mockGuardians = getMockGuardiansForSet(guardianSet);
  const signerIndices = getQuorumSignerIndices(guardianSet.guardians.length);

  // 1. Wrong chain (not Stellar or 0)
  const wrongChainPayload = buildSetMessageFeePayload(
    FEE_AMOUNTS.ONE_XLM,
    2 // Ethereum chain ID
  );

  const wrongChainVaa = createGovernanceVAA(
    mockGuardians,
    signerIndices,
    wrongChainPayload,
    299 // Unique sequence
  );

  testCases.push({
    name: "set_fee_wrong_chain",
    description: "Set message fee for wrong chain (Ethereum instead of Stellar)",
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
      action: ACTION_SET_MESSAGE_FEE,
      chain: 2,
      fee: FEE_AMOUNTS.ONE_XLM.toString(),
    },
    metadata: {
      guardianSetIndex: guardianSet.index,
      numSignatures: signerIndices.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: DEFAULT_VAA_PARAMS.emitterChain,
      sequence: 299,
    },
  });

  // 2. All chains (chain = 0) - should succeed
  const allChainsPayload = buildSetMessageFeePayload(
    FEE_AMOUNTS.ONE_XLM,
    0 // All chains
  );

  const allChainsVaa = createGovernanceVAA(
    mockGuardians,
    signerIndices,
    allChainsPayload,
    298 // Unique sequence
  );

  testCases.push({
    name: "set_fee_all_chains",
    description: "Set message fee for all chains (chain = 0)",
    category: "happy_path",
    expectedResult: "success",
    expectedError: null,
    vaa: {
      base64: allChainsVaa.toString("base64"),
      hex: allChainsVaa.toString("hex"),
      length: allChainsVaa.length,
    },
    payload: {
      module: "Core",
      action: ACTION_SET_MESSAGE_FEE,
      chain: 0,
      fee: FEE_AMOUNTS.ONE_XLM.toString(),
    },
    metadata: {
      guardianSetIndex: guardianSet.index,
      numSignatures: signerIndices.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: DEFAULT_VAA_PARAMS.emitterChain,
      sequence: 298,
    },
  });

  return testCases;
}