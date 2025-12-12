/**
 * Guardian key management and MockGuardians wrapper
 */

import { MockGuardians } from "@certusone/wormhole-sdk/lib/cjs/mock";
import { ethers } from "ethers";
import { TEST_GUARDIAN_PRIVATE_KEYS } from "./config";
import { GuardianKey, GuardianSet } from "./types";

/**
 * Derive Ethereum address from private key
 */
function deriveEthereumAddress(privateKey: string): string {
  const wallet = new ethers.Wallet(privateKey);
  return wallet.address;
}

/**
 * Generate guardian keys from private keys
 */
export function generateGuardianKeys(): GuardianKey[] {
  return TEST_GUARDIAN_PRIVATE_KEYS.map((pk, index) => ({
    index,
    privateKey: pk,
    ethereumAddress: deriveEthereumAddress(pk),
  }));
}

/**
 * Create a guardian set for testing
 */
export function createGuardianSet(index: number): GuardianSet {
  return {
    index,
    guardians: generateGuardianKeys(),
  };
}

/**
 * Create MockGuardians instance for signing VAAs
 */
export function createMockGuardians(guardianSetIndex: number): MockGuardians {
  return new MockGuardians(guardianSetIndex, TEST_GUARDIAN_PRIVATE_KEYS);
}

/**
 * Get guardian indices for signing (first N guardians)
 */
export function getSignerIndices(count: number): number[] {
  return Array.from({ length: count }, (_, i) => i);
}
