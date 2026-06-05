/**
 * Guardian set generator for multiple test scenarios
 */

import { ethers } from "ethers";
import { MockGuardians } from "@certusone/wormhole-sdk/lib/cjs/mock";
import { GuardianKey, GuardianSet } from "../types";

/**
 * Generate deterministic private keys for testing
 * Uses a seed-based approach to create reproducible keys
 */
function generatePrivateKeys(startIndex: number, count: number): string[] {
  const keys: string[] = [];
  for (let i = 0; i < count; i++) {
    // Create deterministic key from index (TEST ONLY - NOT SECURE)
    const seed = `test_guardian_${startIndex + i}_stellar_wormhole`;
    const hash = ethers.utils.id(seed); // keccak256 hash
    keys.push(hash.substring(2)); // Remove 0x prefix
  }
  return keys;
}

/**
 * Derive Ethereum address from private key
 */
function deriveEthereumAddress(privateKey: string): string {
  const wallet = new ethers.Wallet(privateKey);
  return wallet.address;
}

/**
 * Create a guardian set with specified number of guardians
 */
export function createGuardianSetWithSize(
  setIndex: number,
  numGuardians: number,
  startKeyIndex: number = 0
): GuardianSet {
  const privateKeys = generatePrivateKeys(startKeyIndex, numGuardians);

  const guardians: GuardianKey[] = privateKeys.map((pk, index) => ({
    index,
    privateKey: pk,
    ethereumAddress: deriveEthereumAddress(pk),
  }));

  return {
    index: setIndex,
    guardians,
  };
}

/**
 * Create MockGuardians instance for signing VAAs
 */
export function createMockGuardiansWithKeys(
  guardianSetIndex: number,
  privateKeys: string[]
): MockGuardians {
  return new MockGuardians(guardianSetIndex, privateKeys);
}

/**
 * Calculate quorum for a guardian set
 * Formula: (num_guardians * 2 / 3) + 1
 */
export function calculateQuorum(numGuardians: number): number {
  return Math.floor((numGuardians * 2) / 3) + 1;
}

/**
 * Generate all three guardian sets for testing
 */
export interface TestGuardianSets {
  gs0: GuardianSet;  // 3 guardians
  gs1: GuardianSet;  // 7 guardians
  gs2: GuardianSet;  // 19 guardians
}

export function generateAllGuardianSets(): TestGuardianSets {
  return {
    gs0: createGuardianSetWithSize(0, 3, 0),   // Indices 0-2
    gs1: createGuardianSetWithSize(1, 7, 3),   // Indices 3-9
    gs2: createGuardianSetWithSize(2, 19, 10), // Indices 10-28
  };
}

/**
 * Get MockGuardians for a specific guardian set
 */
export function getMockGuardiansForSet(guardianSet: GuardianSet): MockGuardians {
  const privateKeys = guardianSet.guardians.map(g => g.privateKey);
  return createMockGuardiansWithKeys(guardianSet.index, privateKeys);
}

/**
 * Get signer indices for achieving quorum
 */
export function getQuorumSignerIndices(numGuardians: number): number[] {
  const quorum = calculateQuorum(numGuardians);
  // Return first 'quorum' indices for deterministic testing
  return Array.from({ length: quorum }, (_, i) => i);
}

/**
 * Guardian set info for display
 */
export function getGuardianSetInfo(guardianSet: GuardianSet): {
  index: number;
  numGuardians: number;
  quorum: number;
  addresses: string[];
} {
  return {
    index: guardianSet.index,
    numGuardians: guardianSet.guardians.length,
    quorum: calculateQuorum(guardianSet.guardians.length),
    addresses: guardianSet.guardians.map(g => g.ethereumAddress),
  };
}