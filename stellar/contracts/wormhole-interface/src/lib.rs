#![no_std]

use soroban_sdk::{Bytes, BytesN, Env, Symbol, symbol_short};

/// Short event topic (<= 9 chars for `symbol_short!`).
pub const EVENT_TOPIC_LMP: Symbol = symbol_short!("LMP");

/// Minimal cross-crate interface the contract will implement.
pub trait Wormhole {
    /// Publishes a message and returns the incremented per-emitter sequence.
    fn publish_message(
        env: Env,
        emitter: BytesN<32>,
        nonce: u32,
        payload: Bytes,
        consistency: u32,
    ) -> u64;

    /// Returns current sequence for an emitter (0 if none).
    fn sequence_of(env: Env, emitter: BytesN<32>) -> u64;
}
