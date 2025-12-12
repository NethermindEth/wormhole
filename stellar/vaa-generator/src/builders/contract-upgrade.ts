/**
 * Contract Upgrade payload builder
 */

import { MODULE_CORE, ACTION_CONTRACT_UPGRADE, CHAIN_ID_STELLAR } from "../config";

export interface ContractUpgradeParams {
  module?: string;
  action?: number;
  chain?: number;
  newContractHash: string;
}

/**
 * Build Contract Upgrade governance payload
 *
 * Format: Module(32) + Action(1) + Chain(2) + NewContractHash(32) = 67 bytes
 */
export function buildContractUpgradePayload(params: ContractUpgradeParams): Buffer {
  const {
    module = MODULE_CORE,
    action = ACTION_CONTRACT_UPGRADE,
    chain = CHAIN_ID_STELLAR,
    newContractHash,
  } = params;

  const buffer = Buffer.alloc(67);
  let offset = 0;

  // Module (32 bytes)
  const moduleBuffer = Buffer.from(module, "hex");
  moduleBuffer.copy(buffer, offset);
  offset += 32;

  // Action (1 byte)
  buffer.writeUInt8(action, offset);
  offset += 1;

  // Chain (2 bytes, big-endian)
  buffer.writeUInt16BE(chain, offset);
  offset += 2;

  // New contract hash (32 bytes)
  const hashBuffer = Buffer.from(newContractHash, "hex");
  hashBuffer.copy(buffer, offset);

  return buffer;
}

/**
 * Helper to create a test contract hash
 */
export function createTestHash(pattern: number = 0x42): string {
  return Buffer.alloc(32, pattern).toString("hex");
}

/**
 * Helper to create zero hash
 */
export function createZeroHash(): string {
  return Buffer.alloc(32, 0).toString("hex");
}

/**
 * Helper to create max hash
 */
export function createMaxHash(): string {
  return Buffer.alloc(32, 0xff).toString("hex");
}
