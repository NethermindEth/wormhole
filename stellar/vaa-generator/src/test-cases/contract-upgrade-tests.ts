/**
 * Contract Upgrade test case generators
 */

import { MockGuardians } from "@certusone/wormhole-sdk/lib/cjs/mock";
import { TestVAA } from "../types";
import {
  ACTION_CONTRACT_UPGRADE,
  CHAIN_ID_STELLAR,
  GOVERNANCE_CHAIN_ID,
  QUORUM_SIGNATURES,
  DEFAULT_VAA_PARAMS,
} from "../config";
import { buildSignedVAA, toBase64, toHex } from "../builders/vaa-builder";
import {
  buildContractUpgradePayload,
  createTestHash,
  createZeroHash,
  createMaxHash,
} from "../builders/contract-upgrade";
import { getSignerIndices } from "../guardian";

/**
 * Generate all Contract Upgrade test VAAs
 * @param guardians - Mock guardians for signing
 * @param v2WasmHash - Optional V2 WASM hash (if not provided, uses a test pattern)
 */
export function generateContractUpgradeTests(guardians: MockGuardians, v2WasmHash?: string): TestVAA[] {
  return [
    generateValidStellarSpecific(guardians, v2WasmHash),
    generateValidAllChains(guardians, v2WasmHash),
    generateWrongModule(guardians),
    generateWrongAction(guardians),
    generateWrongChainEthereum(guardians),
    generateWrongChainRandom(guardians),
    generatePayloadTooShort(guardians),
    generatePayloadTooLong(guardians),
    generateZeroHash(guardians),
    generateMaxHash(guardians),
  ];
}

function generateValidStellarSpecific(guardians: MockGuardians, providedHash?: string): TestVAA {
  // Use provided V2 hash or default test pattern
  const v2WasmHash = providedHash || createTestHash(0x42);

  const payload = buildContractUpgradePayload({
    chain: CHAIN_ID_STELLAR,
    newContractHash: v2WasmHash,
  });

  const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(QUORUM_SIGNATURES), {
    payload,
  });

  return {
    name: "valid_stellar_specific",
    description: "Valid contract upgrade VAA for Stellar (chain 61) with real WASM hash",
    category: "happy_path",
    expectedResult: "success",
    expectedError: null,
    vaa: {
      base64: toBase64(vaaBuffer),
      hex: toHex(vaaBuffer),
      length: vaaBuffer.length,
    },
    payload: {
      module: "Core",
      action: ACTION_CONTRACT_UPGRADE,
      chain: CHAIN_ID_STELLAR,
      newContractHash: v2WasmHash,
    },
    metadata: {
      guardianSetIndex: DEFAULT_VAA_PARAMS.guardianSetIndex,
      numSignatures: QUORUM_SIGNATURES,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: GOVERNANCE_CHAIN_ID,
      sequence: DEFAULT_VAA_PARAMS.sequence,
    },
  };
}

function generateValidAllChains(guardians: MockGuardians, providedHash?: string): TestVAA {
  // Use provided V2 hash or default test pattern
  const v2WasmHash = providedHash || createTestHash(0x42);

  const payload = buildContractUpgradePayload({
    chain: 0, // All chains
    newContractHash: v2WasmHash,
  });

  const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(QUORUM_SIGNATURES), {
    payload,
  });

  return {
    name: "valid_all_chains",
    description: "Valid contract upgrade VAA for all chains (chain 0) with real WASM hash",
    category: "happy_path",
    expectedResult: "success",
    expectedError: null,
    vaa: {
      base64: toBase64(vaaBuffer),
      hex: toHex(vaaBuffer),
      length: vaaBuffer.length,
    },
    payload: {
      module: "Core",
      action: ACTION_CONTRACT_UPGRADE,
      chain: 0,
      newContractHash: v2WasmHash,
    },
    metadata: {
      guardianSetIndex: DEFAULT_VAA_PARAMS.guardianSetIndex,
      numSignatures: QUORUM_SIGNATURES,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: GOVERNANCE_CHAIN_ID,
      sequence: DEFAULT_VAA_PARAMS.sequence,
    },
  };
}

function generateWrongModule(guardians: MockGuardians): TestVAA {
  const wrongModule = Buffer.alloc(32, 0x99).toString("hex");
  const payload = buildContractUpgradePayload({
    module: wrongModule,
    newContractHash: createTestHash(0x42),
  });

  const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(QUORUM_SIGNATURES), {
    payload,
  });

  return {
    name: "wrong_module",
    description: "Contract upgrade with wrong module (not 'Core')",
    category: "validation_error",
    expectedResult: "error",
    expectedError: "InvalidGovernanceModule",
    vaa: {
      base64: toBase64(vaaBuffer),
      hex: toHex(vaaBuffer),
      length: vaaBuffer.length,
    },
    payload: {
      module: "Wrong",
      action: ACTION_CONTRACT_UPGRADE,
      chain: CHAIN_ID_STELLAR,
      newContractHash: createTestHash(0x42),
    },
    metadata: {
      guardianSetIndex: DEFAULT_VAA_PARAMS.guardianSetIndex,
      numSignatures: QUORUM_SIGNATURES,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: GOVERNANCE_CHAIN_ID,
      sequence: DEFAULT_VAA_PARAMS.sequence,
    },
  };
}

function generateWrongAction(guardians: MockGuardians): TestVAA {
  const payload = buildContractUpgradePayload({
    action: 2, // Guardian set upgrade action
    newContractHash: createTestHash(0x42),
  });

  const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(QUORUM_SIGNATURES), {
    payload,
  });

  return {
    name: "wrong_action",
    description: "Contract upgrade with wrong action (2 instead of 1)",
    category: "validation_error",
    expectedResult: "error",
    expectedError: "InvalidGovernanceAction",
    vaa: {
      base64: toBase64(vaaBuffer),
      hex: toHex(vaaBuffer),
      length: vaaBuffer.length,
    },
    payload: {
      module: "Core",
      action: 2,
      chain: CHAIN_ID_STELLAR,
      newContractHash: createTestHash(0x42),
    },
    metadata: {
      guardianSetIndex: DEFAULT_VAA_PARAMS.guardianSetIndex,
      numSignatures: QUORUM_SIGNATURES,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: GOVERNANCE_CHAIN_ID,
      sequence: DEFAULT_VAA_PARAMS.sequence,
    },
  };
}

function generateWrongChainEthereum(guardians: MockGuardians): TestVAA {
  const payload = buildContractUpgradePayload({
    chain: 2, // Ethereum
    newContractHash: createTestHash(0x42),
  });

  const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(QUORUM_SIGNATURES), {
    payload,
  });

  return {
    name: "wrong_chain_ethereum",
    description: "Contract upgrade for wrong chain (Ethereum = 2)",
    category: "validation_error",
    expectedResult: "error",
    expectedError: "InvalidGovernanceChain",
    vaa: {
      base64: toBase64(vaaBuffer),
      hex: toHex(vaaBuffer),
      length: vaaBuffer.length,
    },
    payload: {
      module: "Core",
      action: ACTION_CONTRACT_UPGRADE,
      chain: 2,
      newContractHash: createTestHash(0x42),
    },
    metadata: {
      guardianSetIndex: DEFAULT_VAA_PARAMS.guardianSetIndex,
      numSignatures: QUORUM_SIGNATURES,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: GOVERNANCE_CHAIN_ID,
      sequence: DEFAULT_VAA_PARAMS.sequence,
    },
  };
}

function generateWrongChainRandom(guardians: MockGuardians): TestVAA {
  const payload = buildContractUpgradePayload({
    chain: 999, // Invalid chain
    newContractHash: createTestHash(0x42),
  });

  const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(QUORUM_SIGNATURES), {
    payload,
  });

  return {
    name: "wrong_chain_random",
    description: "Contract upgrade for invalid chain (999)",
    category: "validation_error",
    expectedResult: "error",
    expectedError: "InvalidGovernanceChain",
    vaa: {
      base64: toBase64(vaaBuffer),
      hex: toHex(vaaBuffer),
      length: vaaBuffer.length,
    },
    payload: {
      module: "Core",
      action: ACTION_CONTRACT_UPGRADE,
      chain: 999,
      newContractHash: createTestHash(0x42),
    },
    metadata: {
      guardianSetIndex: DEFAULT_VAA_PARAMS.guardianSetIndex,
      numSignatures: QUORUM_SIGNATURES,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: GOVERNANCE_CHAIN_ID,
      sequence: DEFAULT_VAA_PARAMS.sequence,
    },
  };
}

function generatePayloadTooShort(guardians: MockGuardians): TestVAA {
  // Create payload with only 66 bytes (missing 1 byte from hash)
  const payload = Buffer.alloc(66);

  const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(QUORUM_SIGNATURES), {
    payload,
  });

  return {
    name: "payload_too_short",
    description: "Contract upgrade with payload too short (66 bytes instead of 67)",
    category: "parsing_error",
    expectedResult: "error",
    expectedError: "InvalidPayload",
    vaa: {
      base64: toBase64(vaaBuffer),
      hex: toHex(vaaBuffer),
      length: vaaBuffer.length,
    },
    payload: {
      note: "Payload is 66 bytes, missing 1 byte from contract hash",
    },
    metadata: {
      guardianSetIndex: DEFAULT_VAA_PARAMS.guardianSetIndex,
      numSignatures: QUORUM_SIGNATURES,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: GOVERNANCE_CHAIN_ID,
      sequence: DEFAULT_VAA_PARAMS.sequence,
    },
  };
}

function generatePayloadTooLong(guardians: MockGuardians): TestVAA {
  // Create payload with 68 bytes (1 extra byte)
  const payload = Buffer.alloc(68);
  // Fill with valid data for first 67 bytes
  const validPayload = buildContractUpgradePayload({
    newContractHash: createTestHash(0x42),
  });
  validPayload.copy(payload, 0);
  // Extra byte at the end
  payload.writeUInt8(0xFF, 67);

  const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(QUORUM_SIGNATURES), {
    payload,
  });

  return {
    name: "payload_too_long",
    description: "Contract upgrade with payload too long (68 bytes, extra ignored)",
    category: "edge_case",
    expectedResult: "success",
    expectedError: null,
    vaa: {
      base64: toBase64(vaaBuffer),
      hex: toHex(vaaBuffer),
      length: vaaBuffer.length,
    },
    payload: {
      module: "Core",
      action: ACTION_CONTRACT_UPGRADE,
      chain: CHAIN_ID_STELLAR,
      newContractHash: createTestHash(0x42),
      note: "Extra byte at end should be ignored",
    },
    metadata: {
      guardianSetIndex: DEFAULT_VAA_PARAMS.guardianSetIndex,
      numSignatures: QUORUM_SIGNATURES,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: GOVERNANCE_CHAIN_ID,
      sequence: DEFAULT_VAA_PARAMS.sequence,
    },
  };
}

function generateZeroHash(guardians: MockGuardians): TestVAA {
  const payload = buildContractUpgradePayload({
    newContractHash: createZeroHash(),
  });

  const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(QUORUM_SIGNATURES), {
    payload,
  });

  return {
    name: "zero_hash",
    description: "Contract upgrade with all-zero contract hash",
    category: "edge_case",
    expectedResult: "success",
    expectedError: null,
    vaa: {
      base64: toBase64(vaaBuffer),
      hex: toHex(vaaBuffer),
      length: vaaBuffer.length,
    },
    payload: {
      module: "Core",
      action: ACTION_CONTRACT_UPGRADE,
      chain: CHAIN_ID_STELLAR,
      newContractHash: createZeroHash(),
    },
    metadata: {
      guardianSetIndex: DEFAULT_VAA_PARAMS.guardianSetIndex,
      numSignatures: QUORUM_SIGNATURES,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: GOVERNANCE_CHAIN_ID,
      sequence: DEFAULT_VAA_PARAMS.sequence,
    },
  };
}

function generateMaxHash(guardians: MockGuardians): TestVAA {
  const payload = buildContractUpgradePayload({
    newContractHash: createMaxHash(),
  });

  const vaaBuffer = buildSignedVAA(guardians, getSignerIndices(QUORUM_SIGNATURES), {
    payload,
  });

  return {
    name: "max_hash",
    description: "Contract upgrade with all-FF contract hash",
    category: "edge_case",
    expectedResult: "success",
    expectedError: null,
    vaa: {
      base64: toBase64(vaaBuffer),
      hex: toHex(vaaBuffer),
      length: vaaBuffer.length,
    },
    payload: {
      module: "Core",
      action: ACTION_CONTRACT_UPGRADE,
      chain: CHAIN_ID_STELLAR,
      newContractHash: createMaxHash(),
    },
    metadata: {
      guardianSetIndex: DEFAULT_VAA_PARAMS.guardianSetIndex,
      numSignatures: QUORUM_SIGNATURES,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: GOVERNANCE_CHAIN_ID,
      sequence: DEFAULT_VAA_PARAMS.sequence,
    },
  };
}
