# Wormhole Core Contract for Stellar/Soroban

A production-ready implementation of the [Wormhole](https://wormhole.com) Core Contract for [Stellar's Soroban](https://soroban.stellar.org) smart contract platform. This enables Stellar to participate in Wormhole's cross-chain messaging protocol as **Chain ID 61**.

## What is Wormhole?

Wormhole is a generic message-passing protocol connecting 30+ blockchains:

- **Guardian Network**: 19 validators observe and attest to cross-chain messages
- **VAAs (Verifiable Action Approvals)**: Signed attestations requiring 13-of-19 guardian signatures
- **Core Contracts**: On-chain contracts that verify VAAs and post messages for guardian observation

This contract implements the Stellar side of that infrastructure.

## Repository Structure

```
contracts/
├── wormhole-soroban-client/     # Public API crate (for external integrations)
│   └── src/
│       ├── lib.rs               # Re-exports and WormholeCoreInterface trait
│       ├── types.rs             # VAA, Signature, GuardianSetInfo, etc.
│       ├── bytes_reader.rs      # Binary parsing utilities
│       ├── error.rs             # 46 error variants
│       └── constants.rs         # Protocol constants
│
└── wormhole-contract/           # Implementation crate
    └── src/
        ├── lib.rs               # Contract entry point
        ├── initialize.rs        # One-time setup
        ├── storage.rs           # StorageKey enum
        ├── vaa.rs               # Signature verification
        ├── message.rs           # Cross-chain message posting
        ├── utils/mod.rs         # Crypto utilities
        └── governance/          # Governance actions
            ├── mod.rs
            ├── action.rs        # GovernanceAction trait
            ├── contract_upgrade.rs   # Action 1
            ├── guardian_set.rs       # Action 2
            ├── set_message_fee.rs    # Action 3
            └── transfer_fees.rs      # Action 4
```

### Why Two Crates?

- **wormhole-soroban-client**: Lightweight public API. External contracts depend only on this, resulting in smaller WASM binaries.
- **wormhole-contract**: Full implementation with storage access and business logic.

## Quick Start

### Prerequisites

```bash
# Install Rust
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

# Add Soroban WASM target
rustup target add wasm32-unknown-unknown

# Install Stellar CLI (optional, for deployment)
cargo install stellar-cli
```

### Build

```bash
# Build the contract
cd contracts/wormhole-contract
stellar contract build

# Output: target/wasm32-unknown-unknown/release/wormhole_contract.wasm

# Optimize for deployment (optional, reduces size)
stellar contract optimize --wasm target/wasm32-unknown-unknown/release/wormhole_contract.wasm
```

### Test

```bash
# Run all tests
cargo test --lib

# Run with output
cargo test --lib -- --nocapture

# Lint
cargo clippy --all-targets -- -D warnings
```

## Contract Interface

The contract implements `WormholeCoreInterface` with the following public functions:

### Initialization

```rust
/// Initialize the contract with the initial guardian set.
/// Can only be called once.
fn initialize(
    env: Env,
    initial_guardians: Vec<BytesN<20>>,  // Ethereum addresses
    governance_emitter: BytesN<32>,       // Authorized governance source
) -> Result<(), Error>;

/// Check if the contract has been initialized.
fn is_initialized(env: Env) -> bool;
```

### VAA Operations

```rust
/// Verify VAA signatures against stored guardian set.
/// Returns true if valid, error otherwise.
fn verify_vaa(env: Env, vaa_bytes: Bytes) -> Result<bool, Error>;

/// Parse VAA bytes into structured data (no signature verification).
fn parse_vaa(env: Env, vaa_bytes: Bytes) -> Result<VAA, Error>;
```

### Governance Actions

All governance requires a signed VAA from the Wormhole governance source (Solana chain 1).

```rust
/// Action 1: Upgrade contract WASM to new hash.
fn submit_contract_upgrade(env: Env, vaa_bytes: Bytes) -> Result<(), Error>;

/// Action 2: Install new guardian set (index must be current + 1).
fn submit_guardian_set_upgrade(env: Env, vaa_bytes: Bytes) -> Result<(), Error>;

/// Action 3: Update message posting fee.
fn submit_set_message_fee(env: Env, vaa_bytes: Bytes) -> Result<(), Error>;

/// Action 4: Transfer accumulated fees to recipient.
fn submit_transfer_fees(env: Env, vaa_bytes: Bytes) -> Result<(), Error>;
```

### Message Posting

```rust
/// Post a cross-chain message for guardian attestation.
/// Returns the assigned sequence number.
fn post_message(
    env: Env,
    emitter: Address,              // Must authorize the call
    nonce: u32,                    // Caller-provided deduplication
    payload: Bytes,                // Application-specific data
    consistency_level: ConsistencyLevel,
) -> Result<u64, Error>;
```

### State Queries

```rust
fn get_current_guardian_set_index(env: Env) -> u32;
fn get_guardian_set(env: Env, index: u32) -> Result<GuardianSetInfo, Error>;
fn get_message_fee(env: Env) -> u64;
fn get_emitter_sequence(env: Env, emitter: Address) -> u64;
fn get_posted_message_hash(env: Env, emitter: Address, sequence: u64) -> Option<BytesN<32>>;
fn get_chain_id(env: Env) -> u16;
fn get_governance_chain_id(env: Env) -> u16;
fn get_governance_emitter(env: Env) -> BytesN<32>;
```

## Core Concepts

### VAA Structure

```
┌─────────────────────────────────────────────────────────────┐
│ Header (6 bytes)                                            │
│   version (1) │ guardian_set_index (4) │ signature_count (1)│
├─────────────────────────────────────────────────────────────┤
│ Signatures (66 bytes each)                                  │
│   guardian_index (1) │ r (32) │ s (32) │ v (1)              │
├─────────────────────────────────────────────────────────────┤
│ Body (variable)                                             │
│   timestamp (4) │ nonce (4) │ emitter_chain (2) │           │
│   emitter_address (32) │ sequence (8) │                     │
│   consistency_level (1) │ payload (variable)                │
└─────────────────────────────────────────────────────────────┘
```

### Signature Verification

1. Serialize VAA body to bytes
2. Double hash: `keccak256(keccak256(body))`
3. For each signature, recover secp256k1 public key
4. Derive Ethereum address from public key
5. Compare against guardian set keys
6. Require 13-of-19 signatures (quorum)

### Governance Flow

All governance actions follow the same pattern:

1. Parse and verify VAA signatures
2. Verify governance source (chain 1, emitter 0x...04)
3. Check VAA not already consumed (replay protection)
4. Parse action-specific payload
5. Validate payload (e.g., sequential guardian set index)
6. **Consume VAA before execution** (critical for contract upgrades)
7. Execute the action

## Protocol Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `CHAIN_ID_STELLAR` | 61 | Stellar's Wormhole chain ID |
| `GOVERNANCE_CHAIN_ID` | 1 | Solana (governance source) |
| `GOVERNANCE_EMITTER` | `0x...04` | Authorized governance address |
| `GUARDIAN_SET_EXPIRATION_TIME` | 86,400s | 24 hours grace period |
| `MINIMUM_CONTRACT_BALANCE` | 10^7 stroops | 1 XLM minimum |
| `STORAGE_TTL_THRESHOLD` | 100,000 | ~5.8 days |
| `STORAGE_TTL_EXTENSION` | 1,000,000 | ~58 days |

## Security Features

### Replay Protection
- Governance VAA hashes are tracked and cannot be reprocessed
- VAAs are marked consumed **before** execution (critical for contract upgrades)

### Guardian Set Management
- Sets can only upgrade sequentially (n → n+1)
- Old sets expire after 24 hours (grace period for in-flight VAAs)
- Cannot overwrite existing sets

### Contract Self-Authorization
- Contract is its own admin (no external admin key to compromise)
- Guardian signatures provide the true authorization

### Balance Protection
- Contract must maintain ≥1 XLM to prevent Stellar account deallocation
- Fee transfers validate remaining balance

## Integration Example

For contracts that want to verify Wormhole messages:

```rust
use soroban_sdk::{contract, contractimpl, Env, Address, Bytes};
use wormhole_soroban_client::{WormholeCoreInterface, VAA};

#[contract]
pub struct MyBridge;

#[contractimpl]
impl MyBridge {
    pub fn receive_message(env: Env, wormhole: Address, vaa_bytes: Bytes) {
        // Create client for Wormhole contract
        let wormhole_client = wormhole_soroban_client::WormholeCoreClient::new(&env, &wormhole);

        // Verify the VAA (checks signatures against guardian set)
        wormhole_client.verify_vaa(&vaa_bytes).unwrap();

        // Parse to get the payload
        let vaa = wormhole_client.parse_vaa(&vaa_bytes).unwrap();

        // Process the cross-chain message
        // vaa.emitter_chain - source blockchain
        // vaa.emitter_address - source contract
        // vaa.sequence - message sequence number
        // vaa.payload - your application data
    }
}
```

## Error Handling

Errors are categorized by range:

| Range | Category | Examples |
|-------|----------|----------|
| 1-19 | VAA Errors | `InvalidVAAFormat`, `InvalidSignature`, `InsufficientSignatures` |
| 20-29 | Initialization | `NotInitialized`, `AlreadyInitialized` |
| 30-39 | Governance | `InvalidGovernanceChain`, `GovernanceVAAAlreadyConsumed` |
| 40-49 | Storage | `GuardianSetNotFound`, `EmptyGuardianSet` |
| 50-59 | Fees | `InsufficientFeePaid`, `InsufficientFunds` |
| 60-69 | Parsing | `InvalidPayload`, `UnexpectedEndOfInput` |

## Development

### Adding Dependencies

In your `Cargo.toml`:

```toml
[dependencies]
wormhole-soroban-client = { path = "../wormhole-soroban-client" }
```

## License

