# Wormhole Integration for Aztec Network

This implementation demonstrates that **Aztec Network can be integrated into the Wormhole ecosystem**, enabling cross-chain messaging and access to Wormhole's multi-chain infrastructure.

## What This Demonstrates

This MVP showcases:
- **VAA Verification on Aztec**: Complete Wormhole VAA parsing and signature verification in Noir
- **Cross-Chain Message Reception**: Aztec can receive and verify messages from any Wormhole-supported chain

**Future Potential**: While this MVP focuses on message reception, it establishes the groundwork for full bidirectional integration, including token bridges, NFT transfers, and arbitrary cross-chain data sharing.

## Architecture

- **Smart Contract** ([`contracts/src/main.nr`](./contracts/src/main.nr)): Noir implementation of VAA verification
- **Verification Service** ([`vaa-verification-service.mjs`](./vaa-verification-service.mjs)): REST API server for testing
- **Deployment Script** ([`contracts/deploy.sh`](./contracts/deploy.sh)): Automated testnet deployment

## Key Technical Features

- **VAA Parsing**: Extracts guardian signatures and message payload from VAA bytes
- **ECDSA Verification**: secp256k1 signature validation using Aztec's cryptographic primitives  
- **Guardian Management**: Configurable guardian set with signature verification
- **Wormhole Compatibility**: Full compliance with Wormhole message format and verification standards

# Quick Start Guide

## Prerequisites
- Node.js v20+
- Docker
- [Aztec CLI tools](https://docs.aztec.network/developers/getting_started):
  ```bash
  bash -i <(curl -s https://install.aztec.network)
  ```

## 1. Deploy Contracts to Testnet
```bash
cd contracts
./deploy.sh
```
The deployment script will:
- Set up wallets and accounts on Aztec testnet
- Deploy a test token contract
- Deploy the Wormhole VAA verification contract
- Configure everything for testing

## 2. Start the Verification Service
```bash
npm install
npm run start-verification
```

## 3. Test VAA Verification
Test with a real Wormhole VAA from Arbitrum Sepolia:
```bash
curl -X POST http://localhost:3000/test
```

Or verify a custom VAA:
```bash
curl -X POST http://localhost:3000/verify \
  -H "Content-Type: application/json" \
  -d '{"vaaBytes": "YOUR_VAA_HEX_HERE"}'
```

## 4. Monitor Results
- Check service health: `GET http://localhost:3000/health`
- View transactions: [Aztec Explorer](http://aztecscan.xyz/)

# Current Implementation

This MVP demonstrates **single guardian VAA verification** on Aztec testnet, proving the technical feasibility of Wormhole integration. The implementation handles complete VAA parsing, signature verification, and message extraction in Aztec's zero-knowledge environment.

**Expandability**: This foundation supports scaling to full multi-guardian verification (13/19 consensus) and bidirectional cross-chain functionality as Aztec moves toward mainnet.
