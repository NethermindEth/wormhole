/**
 * Transfer Fees VAA generator for testing
 */

import { GuardianSet, TestVAA } from "../types";
import {
  GOVERNANCE_CHAIN_ID,
  GOVERNANCE_EMITTER,
  MODULE_CORE,
  ACTION_TRANSFER_FEES,
  CHAIN_ID_STELLAR,
  DEFAULT_VAA_PARAMS,
} from "../config";
import { getMockGuardiansForSet, getQuorumSignerIndices } from "../guardian-sets/generator";
import { createGovernanceVAA } from "../builders/governance-vaa-builder";
import * as crypto from "crypto";

/**
 * Build transfer fees payload
 * Format: module(32) + action(1) + chain(2) + padding(24) + amount(8) + recipient(32)
 */
export function buildTransferFeesPayload(
  amount: bigint,
  recipientED25519PublicKey: Buffer,
  targetChain: number = CHAIN_ID_STELLAR
): Buffer {
  const payloadArr: number[] = [];

  // Module (32 bytes)
  const moduleBytes = Buffer.from(MODULE_CORE, "hex");
  for (const byte of moduleBytes) {
    payloadArr.push(byte);
  }

  // Action (1 byte)
  payloadArr.push(ACTION_TRANSFER_FEES);

  // Chain ID (2 bytes, big-endian)
  payloadArr.push((targetChain >> 8) & 0xff);
  payloadArr.push(targetChain & 0xff);

  // Padding (24 bytes) - U256 compatibility with Ethereum
  for (let i = 0; i < 24; i++) {
    payloadArr.push(0);
  }

  // Amount (8 bytes, big-endian) - Last 8 bytes of U256
  for (let i = 7; i >= 0; i--) {
    payloadArr.push(Number((amount >> BigInt(i * 8)) & BigInt(0xff)));
  }

  // Recipient ED25519 public key (32 bytes)
  for (const byte of recipientED25519PublicKey) {
    payloadArr.push(byte);
  }

  return Buffer.from(payloadArr);
}

/**
 * Generate a deterministic ED25519 public key for testing
 * NOTE: This is for testing only - generates a fake but valid-format public key
 */
export function generateTestED25519PublicKey(seed: string): Buffer {
  // Use crypto hash to generate deterministic 32 bytes
  return crypto.createHash("sha256").update(seed).digest();
}

/**
 * Common transfer amounts in stroops
 */
export const TRANSFER_AMOUNTS = {
  HALF_XLM: BigInt(5_000_000),     // 0.5 XLM
  ONE_XLM: BigInt(10_000_000),     // 1 XLM
  TEN_XLM: BigInt(100_000_000),    // 10 XLM
  HUNDRED_XLM: BigInt(1_000_000_000), // 100 XLM
  EXCESSIVE: BigInt("1000000000000000000"), // Way too much
};

/**
 * Test recipients with deterministic ED25519 public keys
 */
export const TEST_RECIPIENTS = {
  FEE_RECIPIENT: generateTestED25519PublicKey("stellar_fee_recipient_test"),
  TREASURY: generateTestED25519PublicKey("stellar_treasury_test"),
  OPERATIONS: generateTestED25519PublicKey("stellar_operations_test"),
};

/**
 * Generate a transfer fees VAA
 */
export function generateTransferFeesVAA(
  guardianSet: GuardianSet,
  amount: bigint,
  recipientPublicKey: Buffer,
  description: string,
  sequence: number = 300
): TestVAA {
  const mockGuardians = getMockGuardiansForSet(guardianSet);
  const signerIndices = getQuorumSignerIndices(guardianSet.guardians.length);

  const payload = buildTransferFeesPayload(amount, recipientPublicKey, CHAIN_ID_STELLAR);

  const vaaBytes = createGovernanceVAA(
    mockGuardians,
    signerIndices,
    payload,
    sequence
  );

  return {
    name: `transfer_fees_${description.toLowerCase().replace(/\s+/g, "_")}`,
    description: `Transfer ${description} to recipient`,
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
      action: ACTION_TRANSFER_FEES,
      chain: CHAIN_ID_STELLAR,
      amount: amount.toString(),
      amountInXLM: Number(amount) / 10_000_000,
      recipientPublicKey: recipientPublicKey.toString("hex"),
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
 * Generate all transfer fees test VAAs
 */
export function generateTransferFeesTestCases(guardianSet: GuardianSet): TestVAA[] {
  const testCases: TestVAA[] = [];
  let sequence = 300;

  // 1. Transfer 0.5 XLM to fee recipient
  testCases.push(
    generateTransferFeesVAA(
      guardianSet,
      TRANSFER_AMOUNTS.HALF_XLM,
      TEST_RECIPIENTS.FEE_RECIPIENT,
      "0.5 XLM",
      sequence++
    )
  );

  // 2. Transfer 1 XLM to treasury
  testCases.push(
    generateTransferFeesVAA(
      guardianSet,
      TRANSFER_AMOUNTS.ONE_XLM,
      TEST_RECIPIENTS.TREASURY,
      "1 XLM",
      sequence++
    )
  );

  // 3. Transfer 10 XLM to operations
  testCases.push(
    generateTransferFeesVAA(
      guardianSet,
      TRANSFER_AMOUNTS.TEN_XLM,
      TEST_RECIPIENTS.OPERATIONS,
      "10 XLM",
      sequence++
    )
  );

  // 4. Transfer 100 XLM (larger amount)
  testCases.push(
    generateTransferFeesVAA(
      guardianSet,
      TRANSFER_AMOUNTS.HUNDRED_XLM,
      TEST_RECIPIENTS.FEE_RECIPIENT,
      "100 XLM",
      sequence++
    )
  );

  return testCases;
}

/**
 * Generate invalid transfer fees VAAs for testing
 */
export function generateInvalidTransferFeesVAAs(guardianSet: GuardianSet): TestVAA[] {
  const testCases: TestVAA[] = [];
  const mockGuardians = getMockGuardiansForSet(guardianSet);
  const signerIndices = getQuorumSignerIndices(guardianSet.guardians.length);

  // 1. Excessive amount (would exceed balance)
  const excessiveVaa = generateTransferFeesVAA(
    guardianSet,
    TRANSFER_AMOUNTS.EXCESSIVE,
    TEST_RECIPIENTS.FEE_RECIPIENT,
    "Excessive Amount",
    399
  );
  excessiveVaa.category = "validation_error";
  excessiveVaa.expectedResult = "error";
  excessiveVaa.expectedError = "InsufficientFunds";
  excessiveVaa.name = "transfer_fees_excessive_amount";
  excessiveVaa.description = "Attempt to transfer more than contract balance";
  testCases.push(excessiveVaa);

  // 2. Amount that would violate minimum balance (leaves < 1 XLM)
  // This needs to be calculated based on actual contract balance during testing
  const violateMinVaa = generateTransferFeesVAA(
    guardianSet,
    BigInt(9_500_000), // Would leave only 0.5 XLM if contract has 10 XLM
    TEST_RECIPIENTS.FEE_RECIPIENT,
    "Violate Minimum",
    398
  );
  violateMinVaa.category = "validation_error";
  violateMinVaa.expectedResult = "error";
  violateMinVaa.expectedError = "InsufficientContractBalance";
  violateMinVaa.name = "transfer_fees_violate_minimum";
  violateMinVaa.description = "Attempt to transfer amount that would leave < 1 XLM";
  testCases.push(violateMinVaa);

  // 3. Wrong chain (not Stellar or 0)
  const wrongChainPayload = buildTransferFeesPayload(
    TRANSFER_AMOUNTS.ONE_XLM,
    TEST_RECIPIENTS.FEE_RECIPIENT,
    2 // Ethereum chain ID
  );

  const wrongChainVaa = createGovernanceVAA(
    mockGuardians,
    signerIndices,
    wrongChainPayload,
    397
  );

  testCases.push({
    name: "transfer_fees_wrong_chain",
    description: "Transfer fees for wrong chain (Ethereum instead of Stellar)",
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
      action: ACTION_TRANSFER_FEES,
      chain: 2,
      amount: TRANSFER_AMOUNTS.ONE_XLM.toString(),
      recipientPublicKey: TEST_RECIPIENTS.FEE_RECIPIENT.toString("hex"),
    },
    metadata: {
      guardianSetIndex: guardianSet.index,
      numSignatures: signerIndices.length,
      timestamp: DEFAULT_VAA_PARAMS.timestamp,
      emitterChain: DEFAULT_VAA_PARAMS.emitterChain,
      sequence: 397,
    },
  });

  // 4. Zero amount transfer (edge case - should succeed)
  const zeroAmountVaa = generateTransferFeesVAA(
    guardianSet,
    BigInt(0),
    TEST_RECIPIENTS.FEE_RECIPIENT,
    "Zero Amount",
    396
  );
  zeroAmountVaa.name = "transfer_fees_zero_amount";
  zeroAmountVaa.description = "Transfer zero amount (edge case)";
  testCases.push(zeroAmountVaa);

  return testCases;
}