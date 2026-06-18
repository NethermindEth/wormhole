//! Wormhole Core Contract Interface for Stellar/Soroban.
//!
//! This crate provides the public API for interacting with the Wormhole Core
//! contract. External contracts should depend only on this interface crate for
//! smaller WASM binaries, while the implementation lives in
//! `wormhole-contract`.
//!
//! # Key Types
//!
//! - [`VAA`] - Verifiable Action Approval, the core cross-chain message format
//! - [`GuardianSetInfo`] - Guardian set metadata stored on-chain
//! - [`ConsistencyLevel`] - Finality requirements for message attestation
//! - [`WormholeError`] - All possible error conditions
//!
//! # Example
//!
//! ```ignore
//! use wormhole_soroban_client::{WormholeCoreInterface, VAA, WormholeError};
//!
//! // Parse and verify a VAA
//! let vaa = VAA::try_from((&env, &vaa_bytes))?;
//! client.verify_vaa(&vaa_bytes)?;
//!
//! // Post a cross-chain message (emitter is always treated as a contract)
//! let sequence = client.post_message(&emitter, nonce, &payload, ConsistencyLevel::Finalized)?;
//! ```

#![no_std]

pub mod bytes_reader;
pub mod constants;
pub mod error;
pub mod types;

pub use bytes_reader::BytesReader;
pub use constants::*;
pub use error::WormholeError;
pub use types::*;

use soroban_sdk::{Address, Bytes, BytesN, Env, contractclient};

/// Computes the canonical 32-byte Wormhole identity of a Soroban address.
///
/// This is the canonical mapping from a Stellar `Address` (account `G…` or
/// contract `C…`) onto Wormhole's fixed 32-byte wire format, shared by the core
/// contract, NTT, and the off-chain SDK. Any reimplementation must produce
/// identical bytes; the test vectors in this crate pin the exact mapping.
///
/// Definition: `keccak256` over the **StrKey text** — `address.to_string()`
/// returns the canonical `G…`/`C…` string, and `.to_bytes()` is its raw ASCII
/// with no length prefix and no trailing NUL. Hashing the StrKey (which carries
/// the kind prefix + checksum) keeps accounts and contracts collision-free even
/// when their underlying 32 bytes coincide.
pub fn hash_address(env: &Env, address: &Address) -> BytesN<32> {
    env.crypto()
        .keccak256(&address.to_string().to_bytes())
        .to_bytes()
}

/// Complete public interface for the Wormhole Core contract.
///
/// Defines all contract entry points for VAA verification, governance actions,
/// cross-chain message posting, and state queries. The `wormhole-contract`
/// crate implements this trait with the `#[contractimpl]` macro.
///
/// # Security Model
///
/// - Governance actions require VAAs signed by a quorum (13/19) of guardians
/// - VAAs are consumed after use to prevent replay attacks
/// - The contract is its own admin—upgrades require guardian consensus
///
/// # Initialization
///
/// The contract is initialized via `__constructor` at deployment time.
/// Constructor arguments (initial guardians and governance emitter) are passed
/// during the `stellar contract deploy` command after `--`.
#[contractclient(name = "WormholeClient")]
pub trait WormholeCoreInterface {
    // ========== VAA Verification ==========

    /// Verify a complete VAA (Verifiable Action Approval).
    /// Parses the VAA and verifies all guardian signatures.
    ///
    /// # Arguments
    /// * `vaa_bytes` - Serialized VAA bytes
    ///
    /// # Returns
    /// `true` if VAA is valid and properly signed
    ///
    /// # Errors
    /// * `Error::InvalidVAAFormat` - Malformed VAA bytes
    /// * `Error::GuardianSetNotFound` - Guardian set not found
    /// * `Error::GuardianSetExpired` - Guardian set has expired
    /// * `Error::InsufficientSignatures` - Not enough signatures for quorum
    /// * `Error::InvalidSignature` - Invalid guardian signature
    fn verify_vaa(env: Env, vaa_bytes: Bytes) -> Result<(), WormholeError>;

    /// Parse a VAA structure without signature verification.
    ///
    /// # Arguments
    /// * `vaa_bytes` - Serialized VAA bytes
    ///
    /// # Returns
    /// Parsed VAA structure
    ///
    /// # Errors
    /// * `Error::InvalidVAAFormat` - Malformed VAA bytes
    fn parse_vaa(env: Env, vaa_bytes: Bytes) -> Result<VAA, WormholeError>;

    /// Parse and verify a VAA in a single call.
    ///
    /// Equivalent to calling `verify_vaa` then `parse_vaa`, but parses the
    /// VAA only once. This is the recommended entry point for integrators
    /// who need both the parsed structure and signature verification.
    ///
    /// # Arguments
    /// * `vaa_bytes` - Serialized VAA bytes
    ///
    /// # Returns
    /// Parsed and verified VAA structure
    ///
    /// # Errors
    /// * `Error::InvalidVAAFormat` - Malformed VAA bytes
    /// * `Error::GuardianSetNotFound` - Guardian set not found
    /// * `Error::GuardianSetExpired` - Guardian set has expired
    /// * `Error::InsufficientSignatures` - Not enough signatures for quorum
    /// * `Error::InvalidSignature` - Invalid guardian signature
    fn parse_and_verify_vaa(env: Env, vaa_bytes: Bytes) -> Result<VAA, WormholeError>;

    // ========== Governance Actions ==========

    /// Submit a contract upgrade governance VAA.
    /// Requires valid VAA signed by current guardian set.
    ///
    /// # Arguments
    /// * `vaa_bytes` - Serialized governance VAA containing upgrade payload
    ///
    /// # Errors
    /// * VAA verification errors
    /// * `Error::InvalidGovernanceModule` - Wrong module in payload
    /// * `Error::InvalidGovernanceAction` - Wrong action ID
    /// * `Error::InvalidGovernanceChain` - Wrong chain ID
    /// * `Error::GovernanceVAAAlreadyConsumed` - VAA already processed
    fn submit_contract_upgrade(env: Env, vaa_bytes: Bytes) -> Result<(), WormholeError>;

    /// Submit a guardian set upgrade governance VAA.
    /// Requires valid VAA signed by current guardian set.
    ///
    /// # Arguments
    /// * `vaa_bytes` - Serialized governance VAA containing guardian set
    ///   upgrade payload
    ///
    /// # Errors
    /// * VAA verification errors
    /// * Governance validation errors
    /// * `Error::InvalidGuardianSetSequence` - New index not sequential
    /// * `Error::EmptyGuardianSet` - No guardians in new set
    fn submit_guardian_set_upgrade(env: Env, vaa_bytes: Bytes) -> Result<(), WormholeError>;

    /// Submit a set message fee governance VAA.
    /// Requires valid VAA signed by current guardian set.
    ///
    /// # Arguments
    /// * `vaa_bytes` - Serialized governance VAA containing fee update payload
    ///
    /// # Errors
    /// * VAA verification errors
    /// * Governance validation errors
    fn submit_set_message_fee(env: Env, vaa_bytes: Bytes) -> Result<(), WormholeError>;

    /// Submit a transfer fees governance VAA.
    /// Requires valid VAA signed by current guardian set.
    ///
    /// # Arguments
    /// * `vaa_bytes` - Serialized governance VAA containing fee transfer
    ///   payload
    ///
    /// # Errors
    /// * VAA verification errors
    /// * Governance validation errors
    /// * `Error::InsufficientFees` - Not enough fees to transfer
    /// * `Error::TransferFailed` - Token transfer failed
    fn submit_transfer_fees(env: Env, vaa_bytes: Bytes) -> Result<(), WormholeError>;

    // ========== Message Posting ==========

    /// Post a cross-chain message to be attested by Guardians.
    /// The emitter is always treated as a contract address. Collects message
    /// fee if configured.
    ///
    /// # Arguments
    /// * `emitter` - Contract address acting as the message emitter (must
    ///   authorize)
    /// * `nonce` - Unique nonce for the message
    /// * `payload` - Message payload bytes
    /// * `consistency_level` - Finality requirement (Confirmed or Finalized)
    ///
    /// # Returns
    /// Sequence number assigned to the message
    ///
    /// # Errors
    /// * `Error::InsufficientFeePaid` - Fee not paid (requires prior token
    ///   approval)
    fn post_message(
        env: Env,
        emitter: Address,
        nonce: u32,
        payload: Bytes,
        consistency_level: ConsistencyLevel,
    ) -> Result<u64, WormholeError>;

    // ========== State Queries ==========

    /// Get the current active guardian set index.
    ///
    /// # Returns
    /// Index of the current guardian set
    fn get_current_guardian_set_index(env: Env) -> u32;

    /// Get a guardian set by index.
    ///
    /// # Arguments
    /// * `index` - Guardian set index
    ///
    /// # Returns
    /// Guardian set information
    ///
    /// # Errors
    /// * `Error::GuardianSetNotFound` - Guardian set does not exist
    fn get_guardian_set(env: Env, index: u32) -> Result<GuardianSetInfo, WormholeError>;

    /// Get the expiry timestamp for a guardian set.
    ///
    /// # Arguments
    /// * `index` - Guardian set index
    ///
    /// # Returns
    /// Expiry timestamp, or None if not expired
    fn get_guardian_set_expiry(env: Env, index: u32) -> Option<u64>;

    /// Get the current sequence number for an emitter.
    ///
    /// # Arguments
    /// * `emitter` - Emitter address
    ///
    /// # Returns
    /// Next sequence number for the emitter
    fn get_emitter_sequence(env: Env, emitter: Address) -> u64;

    /// Get the current message fee in stroops (10^-7 XLM).
    ///
    /// # Returns
    /// Message fee in stroops
    fn get_message_fee(env: Env) -> u64;

    /// Check if a governance VAA has been consumed (replay protection).
    ///
    /// # Arguments
    /// * `vaa_bytes` - Serialized VAA bytes
    ///
    /// # Returns
    /// `true` if VAA has been consumed
    ///
    /// # Errors
    /// * `Error::InvalidVAAFormat` - Malformed VAA bytes
    fn is_governance_vaa_consumed(env: Env, vaa_bytes: Bytes) -> Result<bool, WormholeError>;

    // ========== Protocol Constants ==========

    /// Get the Wormhole chain ID for Stellar (61).
    ///
    /// # Returns
    /// Chain ID as u32
    fn get_chain_id() -> u32;

    /// Get the governance chain ID (Solana = 1).
    ///
    /// # Returns
    /// Governance chain ID
    fn get_governance_chain_id() -> u32;

    /// Get the governance emitter address.
    ///
    /// # Returns
    /// 32-byte governance emitter address
    fn get_governance_emitter(env: Env) -> BytesN<32>;
}

#[cfg(test)]
mod tests {
    use super::*;
    use soroban_sdk::{String, testutils::Address as _};

    #[test]
    fn test_hash_address_uses_strkey_string_bytes() {
        let env = Env::default();
        let address = Address::generate(&env);
        let expected = env
            .crypto()
            .keccak256(&address.to_string().to_bytes())
            .to_bytes();

        assert_eq!(hash_address(&env, &address), expected);
    }

    fn assert_hash(strkey: &str, expected: [u8; 32]) {
        let env = Env::default();
        let address = Address::from_string(&String::from_str(&env, strkey));

        // The hash input is the raw StrKey ASCII: no length prefix, no trailing NUL.
        assert_eq!(
            address.to_string().to_bytes(),
            Bytes::from_slice(&env, strkey.as_bytes())
        );
        assert_eq!(
            hash_address(&env, &address),
            BytesN::from_array(&env, &expected)
        );
    }

    // Fixed vectors pinning hash_address for an account and a contract StrKey.
    // Expected values are keccak256 over the StrKey ASCII, computed independently;
    // NTT and the off-chain SDK must produce identical bytes.
    #[test]
    fn test_hash_address_account_vector() {
        assert_hash(
            "GAIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCF6M",
            [
                0x27, 0x99, 0x72, 0xbd, 0x86, 0xb0, 0xe9, 0xcf, 0xf5, 0x3e, 0xd0, 0x68, 0xcc,
                0x08, 0x9b, 0x96, 0x59, 0x64, 0x75, 0x48, 0x6a, 0x38, 0x2e, 0xe5, 0x62, 0x95,
                0x76, 0xb3, 0x64, 0x12, 0x5d, 0x27,
            ],
        );
    }

    #[test]
    fn test_hash_address_contract_vector() {
        assert_hash(
            "CARCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEVQO",
            [
                0x79, 0xa0, 0x39, 0x9c, 0x82, 0x8a, 0xeb, 0x24, 0xb7, 0xf1, 0x70, 0x05, 0xd1,
                0x29, 0xf4, 0xa2, 0x95, 0xb3, 0xa6, 0x68, 0x68, 0x74, 0xcb, 0x1b, 0xea, 0xe5,
                0xc9, 0xc9, 0x5b, 0x95, 0xa9, 0x44,
            ],
        );
    }
}
