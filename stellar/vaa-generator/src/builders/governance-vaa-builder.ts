/**
 * Helper for building governance VAAs
 */

import { MockGuardians, MockEmitter } from "@certusone/wormhole-sdk/lib/cjs/mock";
import {
  GOVERNANCE_CHAIN_ID,
  GOVERNANCE_EMITTER,
  DEFAULT_VAA_PARAMS
} from "../config";

/**
 * Create a governance VAA with proper emitter settings
 */
export function createGovernanceVAA(
  mockGuardians: MockGuardians,
  signerIndices: number[],
  payload: Buffer,
  sequence: number = DEFAULT_VAA_PARAMS.sequence,
  timestamp: number = DEFAULT_VAA_PARAMS.timestamp,
  nonce: number = DEFAULT_VAA_PARAMS.nonce,
  consistencyLevel: number = DEFAULT_VAA_PARAMS.consistencyLevel
): Buffer {
  // Create governance emitter
  const emitter = new MockEmitter(
    GOVERNANCE_EMITTER,
    GOVERNANCE_CHAIN_ID,
    sequence
  );

  // Publish message
  const published = emitter.publishMessage(
    nonce,
    payload,
    consistencyLevel,
    timestamp
  );

  // Sign with guardians
  return mockGuardians.addSignatures(published, signerIndices);
}

/**
 * Create a non-governance VAA (for edge cases with wrong chain/emitter)
 */
export function createCustomVAA(
  mockGuardians: MockGuardians,
  signerIndices: number[],
  payload: Buffer,
  emitterChain: number,
  emitterAddress: string,
  sequence: number = DEFAULT_VAA_PARAMS.sequence,
  timestamp: number = DEFAULT_VAA_PARAMS.timestamp,
  nonce: number = DEFAULT_VAA_PARAMS.nonce,
  consistencyLevel: number = DEFAULT_VAA_PARAMS.consistencyLevel
): Buffer {
  // Create custom emitter
  const emitter = new MockEmitter(
    emitterAddress,
    emitterChain,
    sequence
  );

  // Publish message
  const published = emitter.publishMessage(
    nonce,
    payload,
    consistencyLevel,
    timestamp
  );

  // Sign with guardians
  return mockGuardians.addSignatures(published, signerIndices);
}