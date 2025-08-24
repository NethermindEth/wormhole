# Wormhole on Aztec

This folder contains the reference implementation of the Wormhole cross-chain messaging protocol smart contracts on the [Aztec Network](https://aztec.network/), implemented in [Noir](https://noir-lang.org/), Aztec's domain-specific language for zero-knowledge circuits.

## Project Overview

This implementation demonstrates VAA (Verifiable Action Approval) verification on Aztec's testnet, establishing the foundation for cross-chain messaging between Aztec and other chains in the Wormhole ecosystem.

**Key Components:**
- **Noir Smart Contract**: Core VAA parsing and signature verification
- **Verification Service**: Node.js testing service with REST API

⚠️ **This implementation is under active development and currently configured for testnet only.**

# Project Structure

The project is laid out as follows:

- [`contracts/`](./contracts/) - Noir smart contracts for VAA verification
- [`vaa-verification-service.mjs`](./vaa-verification-service.mjs) - Node.js testing service

# Implementation Details

## Smart Contract ([`contracts/src/main.nr`](./contracts/src/main.nr))

The core Wormhole contract implements:

- **VAA Parsing**: Extracts guardian signatures and message body from VAA bytes
- **Hash Computation**: Double Keccak256 hashing per Wormhole specification
- **ECDSA Verification**: secp256k1 signature validation against guardian public keys
- **Guardian Management**: Storage for up to 19 guardians with set expiration

**Current Implementation:**
- Fixed-size VAA handling (2000 bytes with actual length parameter)
- Single guardian verification (testnet configuration)
- Support for up to 13 concurrent signatures (prepared for mainnet)

## Verification Service

Node.js service providing:
- REST API for VAA verification testing
- Integration with Aztec's Private Execution Environment (PXE)
- Testnet deployment and interaction capabilities

# Building & Testing

## Prerequisites
- Node.js v20+ (tested with v24, officially supports v22.15.x LTS)
- [Docker](https://docs.docker.com/get-docker/) - required for Aztec sandbox
- [Aztec CLI tools](https://docs.aztec.network/developers/getting_started) - install with:
  ```bash
  bash -i <(curl -s https://install.aztec.network)
  ```

## Setup
```bash
npm install
```

## Running the Verification Service
```bash
npm run start-verification
```

# Testing

The verification service provides endpoints for testing VAA verification:

- `GET /health` - Service status
- `POST /verify` - Custom VAA verification  
- `POST /test` - Test with real Arbitrum Sepolia VAA

## Example
```bash
curl -X POST http://localhost:3000/test
```

This uses a real VAA containing "Hello Wormhole!" to demonstrate end-to-end verification.

# Current Status

## Limitations
- **Testnet Only**: Configured for Aztec testnet
- **Single Guardian**: Verifies one guardian signature (testnet configuration)  
- **ECDSA Loop**: Full multi-guardian verification pending Noir compiler improvements

## Future Development
- Multi-guardian signature verification (13/19 consensus for mainnet)
- Cross-chain asset transfers (token bridge equivalent)
- Performance optimizations and gas efficiency improvements

# Implementation Notes

This demonstrates successful VAA verification in Noir on Aztec Network, establishing the foundation for full Wormhole protocol support. The implementation leverages Aztec's privacy-focused execution environment while maintaining compatibility with Wormhole's cross-chain messaging specifications.
