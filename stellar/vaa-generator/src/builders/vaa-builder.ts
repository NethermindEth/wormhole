/**
 * Core VAA building functionality
 */

import { MockGuardians, MockEmitter } from "@certusone/wormhole-sdk/lib/cjs/mock";
import { DEFAULT_VAA_PARAMS } from "../config";

export interface VAAParams {
  timestamp?: number;
  nonce?: number;
  emitterChain?: number;
  emitterAddress?: string;
  sequence?: number;
  consistencyLevel?: number;
  payload: Buffer;
}

/**
 * Build and sign a VAA with specified parameters
 */
export function buildSignedVAA(
  guardians: MockGuardians,
  signerIndices: number[],
  params: VAAParams
): Buffer {
  const {
    timestamp = DEFAULT_VAA_PARAMS.timestamp,
    nonce = DEFAULT_VAA_PARAMS.nonce,
    emitterChain = DEFAULT_VAA_PARAMS.emitterChain,
    emitterAddress = DEFAULT_VAA_PARAMS.emitterAddress,
    sequence = DEFAULT_VAA_PARAMS.sequence,
    consistencyLevel = DEFAULT_VAA_PARAMS.consistencyLevel,
    payload,
  } = params;

  // Create emitter with specified chain
  const emitter = new MockEmitter(
    emitterAddress,
    emitterChain,
    sequence
  );

  // Publish message (creates unsigned VAA body)
  const published = emitter.publishMessage(
    nonce,
    payload,
    consistencyLevel,
    timestamp
  );

  // Sign with guardians
  const signedVAA = guardians.addSignatures(published, signerIndices);

  return signedVAA;
}

/**
 * Convert Buffer to base64 string
 */
export function toBase64(buffer: Buffer): string {
  return buffer.toString("base64");
}

/**
 * Convert Buffer to hex string
 */
export function toHex(buffer: Buffer): string {
  return buffer.toString("hex");
}
