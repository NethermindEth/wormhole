/**
 * Edge case VAA generator for testing error conditions
 */

import { GuardianSet, TestVAA } from "../types";
import {
  GOVERNANCE_CHAIN_ID,
  GOVERNANCE_EMITTER,
  MODULE_CORE,
  ACTION_CONTRACT_UPGRADE,
  CHAIN_ID_STELLAR,
  DEFAULT_VAA_PARAMS,
} from "../config";
import { getMockGuardiansForSet, getQuorumSignerIndices } from "../guardian-sets/generator";
import { createGovernanceVAA, createCustomVAA } from "../builders/governance-vaa-builder";
import { buildSetMessageFeePayload } from "./set-message-fee";

/**
 * Generate VAAs with wrong governance source
 */
export function generateWrongGovernanceSourceVAAs(guardianSet: GuardianSet): TestVAA[] {
  const testCases: TestVAA[] = [];
  const mockGuardians = getMockGuardiansForSet(guardianSet);
  const signerIndices = getQuorumSignerIndices(guardianSet.guardians.length);

  // 1. Wrong governance chain (Ethereum instead of Solana)
  const wrongChainPayload = buildSetMessageFeePayload(BigInt(10_000_000), CHAIN_ID_STELLAR);
  const wrongChainVaa = createCustomVAA(
    mockGuardians,
    signerIndices,
    wrongChainPayload,
    2, // Ethereum chain instead of Solana (1)
    DEFAULT_VAA_PARAMS.emitterAddress,
    500
  );

  testCases.push({
    name: "edge_wrong_governance_chain",
    description: "VAA from wrong governance chain (Ethereum instead of Solana)",
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
      action: ACTION_CONTRACT_UPGRADE,
      chain: CHAIN_ID_STELLAR,
    },
    metadata: {
      guardianSetIndex: guardianSet.index,
      numSignatures: signerIndices.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: 2, // Wrong!
      sequence: 500,
    },
  });

  // 2. Wrong governance emitter
  const wrongEmitter = "0000000000000000000000000000000000000000000000000000000000000005"; // Should be 0x...04
  const wrongEmitterVaa = createCustomVAA(
    mockGuardians,
    signerIndices,
    wrongChainPayload,
    GOVERNANCE_CHAIN_ID,
    wrongEmitter,
    501
  );

  testCases.push({
    name: "edge_wrong_governance_emitter",
    description: "VAA from wrong governance emitter (0x...05 instead of 0x...04)",
    category: "validation_error",
    expectedResult: "error",
    expectedError: "InvalidGovernanceEmitter",
    vaa: {
      base64: wrongEmitterVaa.toString("base64"),
      hex: wrongEmitterVaa.toString("hex"),
      length: wrongEmitterVaa.length,
    },
    payload: {
      module: "Core",
      action: ACTION_CONTRACT_UPGRADE,
      chain: CHAIN_ID_STELLAR,
    },
    metadata: {
      guardianSetIndex: guardianSet.index,
      numSignatures: signerIndices.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: GOVERNANCE_CHAIN_ID,
      sequence: 501,
    },
  });

  return testCases;
}

/**
 * Generate VAAs with signature issues
 */
export function generateSignatureIssueVAAs(guardianSet: GuardianSet): TestVAA[] {
  const testCases: TestVAA[] = [];
  const mockGuardians = getMockGuardiansForSet(guardianSet);

  // 1. Insufficient signatures (less than quorum)
  const quorum = Math.floor((guardianSet.guardians.length * 2) / 3) + 1;
  const insufficientSigners = Array.from({ length: quorum - 1 }, (_, i) => i); // One less than quorum

  const payload = buildSetMessageFeePayload(BigInt(10_000_000), CHAIN_ID_STELLAR);
  const insufficientSigsVaa = createGovernanceVAA(
    mockGuardians,
    insufficientSigners,
    payload,
    502
  );

  testCases.push({
    name: "edge_insufficient_signatures",
    description: `VAA with insufficient signatures (${insufficientSigners.length} of ${guardianSet.guardians.length}, need ${quorum})`,
    category: "validation_error",
    expectedResult: "error",
    expectedError: "InsufficientSignatures",
    vaa: {
      base64: insufficientSigsVaa.toString("base64"),
      hex: insufficientSigsVaa.toString("hex"),
      length: insufficientSigsVaa.length,
    },
    payload: {
      module: "Core",
      action: ACTION_CONTRACT_UPGRADE,
      chain: CHAIN_ID_STELLAR,
    },
    metadata: {
      guardianSetIndex: guardianSet.index,
      numSignatures: insufficientSigners.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: DEFAULT_VAA_PARAMS.emitterChain,
      sequence: 502,
    },
  });

  // 2. Signatures out of order
  // Note: MockGuardians automatically sorts signatures, so we can't easily create unordered signatures
  // We'll skip this test case for now

  // 3. No signatures - we can't actually create a VAA with 0 signatures due to SDK limitations
  // We'll test with 1 signature which is still insufficient for quorum
  // This is now covered by the insufficientSigsVaa test case above

  return testCases;
}

/**
 * Generate malformed VAA payloads
 */
export function generateMalformedPayloadVAAs(guardianSet: GuardianSet): TestVAA[] {
  const testCases: TestVAA[] = [];
  const mockGuardians = getMockGuardiansForSet(guardianSet);
  const signerIndices = getQuorumSignerIndices(guardianSet.guardians.length);

  // 1. Wrong module name (not "Core")
  const wrongModulePayload = Buffer.concat([
    Buffer.from("0000000000000000000000000000000000000000000000000000000057726f6e", "hex"), // "Wron" instead of "Core"
    Buffer.from([ACTION_CONTRACT_UPGRADE]),
    Buffer.from([0x00, CHAIN_ID_STELLAR]),
    Buffer.from("4242424242424242424242424242424242424242424242424242424242424242", "hex"),
  ]);

  const wrongModuleVaa = createGovernanceVAA(
    mockGuardians,
    signerIndices,
    wrongModulePayload,
    504
  );

  testCases.push({
    name: "edge_wrong_module",
    description: "VAA with wrong module name (not Core)",
    category: "validation_error",
    expectedResult: "error",
    expectedError: "InvalidGovernanceModule",
    vaa: {
      base64: wrongModuleVaa.toString("base64"),
      hex: wrongModuleVaa.toString("hex"),
      length: wrongModuleVaa.length,
    },
    payload: {
      module: "Wron",
      action: ACTION_CONTRACT_UPGRADE,
      chain: CHAIN_ID_STELLAR,
    },
    metadata: {
      guardianSetIndex: guardianSet.index,
      numSignatures: signerIndices.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: DEFAULT_VAA_PARAMS.emitterChain,
      sequence: 504,
    },
  });

  // 2. Invalid action ID
  const invalidActionPayload = Buffer.concat([
    Buffer.from(MODULE_CORE, "hex"),
    Buffer.from([99]), // Invalid action ID
    Buffer.from([0x00, CHAIN_ID_STELLAR]),
    Buffer.from("4242424242424242424242424242424242424242424242424242424242424242", "hex"),
  ]);

  const invalidActionVaa = createGovernanceVAA(
    mockGuardians,
    signerIndices,
    invalidActionPayload,
    505
  );

  testCases.push({
    name: "edge_invalid_action",
    description: "VAA with invalid governance action ID (99)",
    category: "validation_error",
    expectedResult: "error",
    expectedError: "InvalidGovernanceAction",
    vaa: {
      base64: invalidActionVaa.toString("base64"),
      hex: invalidActionVaa.toString("hex"),
      length: invalidActionVaa.length,
    },
    payload: {
      module: "Core",
      action: 99,
      chain: CHAIN_ID_STELLAR,
    },
    metadata: {
      guardianSetIndex: guardianSet.index,
      numSignatures: signerIndices.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: DEFAULT_VAA_PARAMS.emitterChain,
      sequence: 505,
    },
  });

  // 3. Truncated payload (too short)
  const truncatedPayload = Buffer.concat([
    Buffer.from(MODULE_CORE, "hex"),
    Buffer.from([ACTION_CONTRACT_UPGRADE]),
    Buffer.from([0x00]), // Missing chain ID byte and contract hash
  ]);

  const truncatedVaa = createGovernanceVAA(
    mockGuardians,
    signerIndices,
    truncatedPayload,
    506
  );

  testCases.push({
    name: "edge_truncated_payload",
    description: "VAA with truncated payload (missing required fields)",
    category: "parsing_error",
    expectedResult: "error",
    expectedError: "InvalidPayload",
    vaa: {
      base64: truncatedVaa.toString("base64"),
      hex: truncatedVaa.toString("hex"),
      length: truncatedVaa.length,
    },
    payload: {
      module: "Core",
      action: ACTION_CONTRACT_UPGRADE,
      truncated: true,
    },
    metadata: {
      guardianSetIndex: guardianSet.index,
      numSignatures: signerIndices.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: DEFAULT_VAA_PARAMS.emitterChain,
      sequence: 506,
    },
  });

  return testCases;
}

/**
 * Generate all edge case VAAs
 */
export function generateAllEdgeCaseVAAs(guardianSet: GuardianSet): TestVAA[] {
  return [
    ...generateWrongGovernanceSourceVAAs(guardianSet),
    ...generateSignatureIssueVAAs(guardianSet),
    ...generateMalformedPayloadVAAs(guardianSet),
  ];
}