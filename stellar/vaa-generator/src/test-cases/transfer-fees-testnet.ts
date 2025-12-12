/**
 * Transfer Fees VAA generator for testnet integration testing
 */

import { GuardianSet, TestVAA } from "../types";
import {
  GOVERNANCE_CHAIN_ID,
  MODULE_CORE,
  ACTION_TRANSFER_FEES,
  CHAIN_ID_STELLAR,
  DEFAULT_VAA_PARAMS,
} from "../config";
import { getMockGuardiansForSet, getQuorumSignerIndices } from "../guardian-sets/generator";
import { createGovernanceVAA } from "../builders/governance-vaa-builder";

/**
 * Build transfer fees payload with testnet recipient
 * Format: module(32) + action(1) + chain(2) + padding(24) + amount(8) + recipient(32)
 */
export function buildTransferFeesPayloadTestnet(
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
 * Testnet recipient public keys
 * fee_recipient: GCCTH6Q3CKNFRGGUUFUWHJURF2XJTF77BLJCYG2E3VQGEYXMQRV6XPA2
 *
 * ED25519 public key decoded from Stellar address (G-prefix = ED25519 public key)
 */
export const TESTNET_RECIPIENTS = {
  FEE_RECIPIENT: Buffer.from([
    0x85, 0x33, 0xfa, 0x1b, 0x12, 0x9a, 0x58, 0x98,
    0xd4, 0xa1, 0x69, 0x63, 0xa6, 0x91, 0x2e, 0xae,
    0x99, 0x97, 0xff, 0x0a, 0xd2, 0x2c, 0x1b, 0x44,
    0xdd, 0x60, 0x62, 0x62, 0xec, 0x84, 0x6b, 0xeb
  ]),
};

/**
 * Common transfer amounts in stroops
 */
export const TRANSFER_AMOUNTS = {
  HALF_XLM: BigInt(5_000_000),     // 0.5 XLM
  ONE_XLM: BigInt(10_000_000),     // 1 XLM
  TEN_XLM: BigInt(100_000_000),    // 10 XLM
  HUNDRED_XLM: BigInt(1_000_000_000), // 100 XLM
};

/**
 * Generate a transfer fees VAA with testnet recipient
 */
export function generateTransferFeesVAATestnet(
  guardianSet: GuardianSet,
  amount: bigint,
  recipientPublicKey: Buffer,
  description: string,
  sequence: number = 300
): TestVAA {
  const mockGuardians = getMockGuardiansForSet(guardianSet);
  const signerIndices = getQuorumSignerIndices(guardianSet.guardians.length);

  const payload = buildTransferFeesPayloadTestnet(amount, recipientPublicKey, CHAIN_ID_STELLAR);

  const vaaBytes = createGovernanceVAA(
    mockGuardians,
    signerIndices,
    payload,
    sequence
  );

  return {
    name: `transfer_fees_${description.toLowerCase().replace(/\s+/g, "_")}`,
    description: `Transfer ${description} to testnet recipient`,
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
 * Generate transfer fees test VAAs for testnet integration testing
 */
export function generateTransferFeesTestCasesTestnet(guardianSet: GuardianSet): TestVAA[] {
  const testCases: TestVAA[] = [];
  let sequence = 400; // Start from different sequence to avoid conflicts

  // 1. Transfer 0.5 XLM to fee recipient
  testCases.push(
    generateTransferFeesVAATestnet(
      guardianSet,
      TRANSFER_AMOUNTS.HALF_XLM,
      TESTNET_RECIPIENTS.FEE_RECIPIENT,
      "0.5 XLM",
      sequence++
    )
  );

  // 2. Transfer 1 XLM to fee recipient
  testCases.push(
    generateTransferFeesVAATestnet(
      guardianSet,
      TRANSFER_AMOUNTS.ONE_XLM,
      TESTNET_RECIPIENTS.FEE_RECIPIENT,
      "1 XLM",
      sequence++
    )
  );

  // 3. Transfer 10 XLM to fee recipient
  testCases.push(
    generateTransferFeesVAATestnet(
      guardianSet,
      TRANSFER_AMOUNTS.TEN_XLM,
      TESTNET_RECIPIENTS.FEE_RECIPIENT,
      "10 XLM",
      sequence++
    )
  );

  return testCases;
}