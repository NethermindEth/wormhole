/**
 * Core type definitions for VAA generation
 */

export interface TestVAA {
  name: string;
  description: string;
  category: 'happy_path' | 'validation_error' | 'parsing_error' | 'edge_case' | 'common_error';
  expectedResult: 'success' | 'error';
  expectedError: string | null;
  vaa: {
    base64: string;
    hex: string;
    length: number;
  };
  payload: any;
  metadata: {
    guardianSetIndex: number;
    numSignatures: number;
    timestamp: number;
    emitterChain: number;
    sequence: number;
  };
}

export interface GuardianKey {
  index: number;
  privateKey: string;
  ethereumAddress: string;
}

export interface GuardianSet {
  index: number;
  guardians: GuardianKey[];
}

export interface ContractUpgradePayload {
  module: string;
  action: number;
  chain: number;
  newContractHash: string;
}
