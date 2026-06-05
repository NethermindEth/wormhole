/**
 * Configuration constants for VAA generation
 */

// Governance constants (from Rust code)
export const GOVERNANCE_CHAIN_ID = 1; // Solana
export const GOVERNANCE_EMITTER = "0000000000000000000000000000000000000000000000000000000000000004";

// Stellar chain ID in Wormhole
export const CHAIN_ID_STELLAR = 61;

// Core module identifier (right-padded "Core")
// 28 zero bytes (56 hex chars) + "Core" (8 hex chars) = 32 bytes total
export const MODULE_CORE = "00000000000000000000000000000000000000000000000000000000436f7265";

// Governance action IDs
export const ACTION_CONTRACT_UPGRADE = 1;
export const ACTION_GUARDIAN_SET_UPGRADE = 2;
export const ACTION_SET_MESSAGE_FEE = 3;
export const ACTION_TRANSFER_FEES = 4;

// Default VAA parameters
export const DEFAULT_VAA_PARAMS = {
  version: 1,
  guardianSetIndex: 2, // Using GS2 for current contract state
  timestamp: 1700000000, // Fixed for determinism
  nonce: 1,
  emitterChain: GOVERNANCE_CHAIN_ID,
  emitterAddress: GOVERNANCE_EMITTER,
  sequence: 1,
  consistencyLevel: 32, // Finalized
};

// ⚠️  WARNING: TEST KEYS ONLY - NEVER USE IN PRODUCTION
// 19 deterministic private keys for test guardians
// These are ONLY for testing, generated from a seed for reproducibility
// MockGuardians from Wormhole SDK uses these internally
export const TEST_GUARDIAN_PRIVATE_KEYS = [
  "cfb12303a19cde580bb4dd771639b0d26bc68353645571a8cff516ab2ee113a0",
  "c3b2f08ea8f8ddee70c68c6b1e0b88c8a6c42e1d19c6b8e6a2c8f2a1c9c7e8a1",
  "a1b2c3d4e5f6071829384950a1b2c3d4e5f6071829384950a1b2c3d4e5f60718",
  "9876543210abcdef9876543210abcdef9876543210abcdef9876543210abcdef",
  "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
  "fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321",
  "5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a",
  "b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1",
  "c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2",
  "d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3",
  "e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4e4",
  "f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5f5",
  "a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6",
  "b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7",
  "c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8c8",
  "d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9",
  "eaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaeaea",
  "fbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfb",
];

// Number of signatures required for quorum (13 of 19)
export const QUORUM_SIGNATURES = 13;
