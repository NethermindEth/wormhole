#![no_std]

use soroban_sdk::{contractclient, contracterror, Address, Bytes, BytesN, Env};

#[contractclient(name = "WormholeClient")]
/// Minimal cross-crate interface the contract will implement.
pub trait Wormhole {
    /// Initialize the contract (e.g., set owner, chain ID, initial guardians).
    fn initialize(env: Env, owner: Address);

    /// Publishes a message and returns the incremented per-emitter sequence.
    fn publish_message(
        env: Env,
        emitter: BytesN<32>,
        nonce: u32,
        payload: Bytes,
        // Using u32 for Soroban compatibility
        consistency: u32,
    ) -> u64;

    /// Returns current sequence for an emitter (0 if none).
    fn sequence_of(env: Env, emitter: BytesN<32>) -> u64;
}

#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq, PartialOrd, Ord)]
#[repr(u32)]
pub enum WormholeError {
    AlreadyInitialized = 1,
    NotInitialized = 2,
    Unauthorized = 3,
}