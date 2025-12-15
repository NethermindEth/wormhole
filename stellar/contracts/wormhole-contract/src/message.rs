use soroban_sdk::{Address, Bytes, BytesN, Env, contractevent, token};
use wormhole_soroban_client::{
    CHAIN_ID_STELLAR, ConsistencyLevel, PostedMessageData, STORAGE_TTL_EXTENSION,
    STORAGE_TTL_THRESHOLD, WormholeError,
};

use crate::{
    governance, initialize,
    storage::StorageKey,
    utils::{address_to_bytes32, get_native_token_address, keccak256_hash},
};

/// Event published when a cross-chain message is posted.
///
/// Topics: ["wormhole", "message_published"]
/// - "wormhole": Namespace for cross-chain message events that guardians observe and attest to
/// - "message_published": Event type for new messages (guardians filter on this to find messages to sign)
#[contractevent(topics = ["wormhole", "message_published"])]
struct MessagePublishedEvent {
    nonce: u32,
    sequence: u64,
    emitter_address: BytesN<32>,
    payload: Bytes,
    consistency_level: ConsistencyLevel,
}

/// Serialize PostedMessageData for hashing
fn serialize_posted_message(
    message: &PostedMessageData,
    env: &Env,
) -> Result<Bytes, WormholeError> {
    // Build fixed-size header as a single array (51 bytes)
    let mut header = [0u8; 51];
    let mut offset = 0;

    // timestamp (4 bytes, big-endian)
    header[offset..offset + 4].copy_from_slice(&message.timestamp.to_be_bytes());
    offset += 4;

    // nonce (4 bytes, big-endian)
    header[offset..offset + 4].copy_from_slice(&message.nonce.to_be_bytes());
    offset += 4;

    // emitter_chain (2 bytes, big-endian u16)
    header[offset..offset + 2].copy_from_slice(&(message.emitter_chain as u16).to_be_bytes());
    offset += 2;

    // emitter_address (32 bytes)
    header[offset..offset + 32].copy_from_slice(&message.emitter_address.to_array());
    offset += 32;

    // sequence (8 bytes, big-endian)
    header[offset..offset + 8].copy_from_slice(&message.sequence.to_be_bytes());
    offset += 8;

    // consistency_level (1 byte)
    header[offset] = message.consistency_level as u8;

    // Create Bytes from header and append payload in a single operation
    let mut bytes = Bytes::from_array(env, &header);
    bytes.append(&message.payload);

    Ok(bytes)
}

pub fn get_emitter_sequence(env: &Env, emitter: &Address) -> u64 {
    env.storage()
        .persistent()
        .get(&StorageKey::EmitterSequence(emitter.clone()))
        .unwrap_or(0)
}

/// Get the hash of a posted message by emitter and sequence number.
/// Returns None if the message was not found.
pub fn get_posted_message_hash(env: &Env, emitter: &Address, sequence: u64) -> Option<BytesN<32>> {
    env.storage()
        .persistent()
        .get(&StorageKey::PostedMessage(emitter.clone(), sequence))
}

fn next_emitter_sequence(env: &Env, emitter: &Address) -> u64 {
    let current = get_emitter_sequence(env, emitter);
    let next = current.saturating_add(1);
    env.storage()
        .persistent()
        .set(&StorageKey::EmitterSequence(emitter.clone()), &next);

    env.storage().persistent().extend_ttl(
        &StorageKey::EmitterSequence(emitter.clone()),
        STORAGE_TTL_THRESHOLD,
        STORAGE_TTL_EXTENSION,
    );

    current
}

fn store_posted_message(env: &Env, emitter: &Address, sequence: u64, message_hash: &BytesN<32>) {
    env.storage().persistent().set(
        &StorageKey::PostedMessage(emitter.clone(), sequence),
        message_hash,
    );

    env.storage().persistent().extend_ttl(
        &StorageKey::PostedMessage(emitter.clone(), sequence),
        STORAGE_TTL_THRESHOLD,
        STORAGE_TTL_EXTENSION,
    );
}

pub fn post_message_with_fee(
    env: &Env,
    emitter: &Address,
    nonce: u32,
    payload: Bytes,
    consistency_level: ConsistencyLevel,
) -> Result<u64, WormholeError> {
    initialize::require_initialized(env)?;

    let required_fee = governance::get_message_fee(env);

    if required_fee > 0 {
        let native_token = get_native_token_address(env);
        let token_client = token::TokenClient::new(env, &native_token);
        let contract = env.current_contract_address();

        match token_client.try_transfer_from(
            &contract,
            emitter,
            &contract,
            &i128::from(required_fee),
        ) {
            Ok(Ok(())) => {}
            _ => {
                return Err(WormholeError::InsufficientFeePaid);
            }
        }
    }

    let sequence = next_emitter_sequence(env, emitter);

    let emitter_bytes = address_to_bytes32(env, emitter);

    let message_data = PostedMessageData {
        timestamp: u32::try_from(env.ledger().timestamp()).unwrap_or(0),
        nonce,
        emitter_chain: u32::from(CHAIN_ID_STELLAR),
        emitter_address: emitter_bytes.clone(),
        sequence,
        consistency_level,
        payload: payload.clone(),
    };

    let message_bytes = serialize_posted_message(&message_data, env)?;
    let hash_bytes = keccak256_hash(env, &message_bytes);

    store_posted_message(env, emitter, sequence, &hash_bytes);

    MessagePublishedEvent {
        nonce,
        sequence,
        emitter_address: emitter_bytes,
        payload,
        consistency_level,
    }
    .publish(env);

    Ok(sequence)
}

#[cfg(test)]
mod tests {
    use soroban_sdk::{
        Address, IntoVal, Symbol, contracttype,
        testutils::{Address as TestAddress, Events},
        vec,
    };

    use super::*;
    use crate::{Wormhole, WormholeClient};

    #[test]
    fn test_require_initialized_enforced() {
        let env = Env::default();
        let contract_id = env.register(Wormhole, ());
        let client = WormholeClient::new(&env, &contract_id);

        env.mock_all_auths();

        let emitter = <Address as TestAddress>::generate(&env);
        let nonce = 123u32;
        let payload = Bytes::from_array(&env, &[0xAA, 0xBB]);
        let res_post =
            client.try_post_message(&emitter, &nonce, &payload, &ConsistencyLevel::Confirmed);
        assert_eq!(res_post, Err(Ok(WormholeError::NotInitialized)));

        let empty_vaa = Bytes::new(&env);

        let res_fee = client.try_submit_set_message_fee(&empty_vaa);
        assert_eq!(res_fee, Err(Ok(WormholeError::NotInitialized)));

        let res_transfer = client.try_submit_transfer_fees(&empty_vaa);
        assert_eq!(res_transfer, Err(Ok(WormholeError::NotInitialized)));

        let res_gs = client.try_submit_guardian_set_upgrade(&empty_vaa);
        assert_eq!(res_gs, Err(Ok(WormholeError::NotInitialized)));

        let res_upgrade = client.try_submit_contract_upgrade(&empty_vaa);
        assert_eq!(res_upgrade, Err(Ok(WormholeError::NotInitialized)));
    }

    #[test]
    fn test_post_message_no_fee() {
        #[contracttype]
        #[derive(Clone, Debug, PartialEq)]
        struct ExpectedMessagePublishedEvent {
            nonce: u32,
            sequence: u64,
            emitter_address: BytesN<32>,
            payload: Bytes,
            consistency_level: ConsistencyLevel,
        }

        let env = Env::default();
        let contract_id = env.register(Wormhole, ());
        let client = WormholeClient::new(&env, &contract_id);

        // init
        let guardian = BytesN::from_array(&env, &[0u8; 20]);
        let initial_guardians = soroban_sdk::vec![&env, guardian];
        let governance_emitter = BytesN::from_array(&env, &[1u8; 32]);
        client.initialize(&initial_guardians, &governance_emitter);

        assert_eq!(client.get_message_fee(), 0);

        // auth
        env.mock_all_auths();

        let emitter = Address::generate(&env);
        let nonce = 7u32;
        let payload = Bytes::from_array(&env, &[0xAA, 0xBB, 0xCC]);
        let cl = ConsistencyLevel::Confirmed;

        // precompute values used by hashing/event
        let ts_u32 = u32::try_from(env.ledger().timestamp()).unwrap_or(0);
        let emitter_bytes32 = address_to_bytes32(&env, &emitter);

        // call #1
        let seq0 = client.post_message(&emitter, &nonce, &payload, &cl);
        assert_eq!(seq0, 0);

        // event
        let events = env.events().all();
        assert_eq!(events.len(), 1);
        let event = events.last().unwrap();

        let topics = event.1.clone();
        let t0: Symbol = topics.get(0).unwrap().into_val(&env);
        let t1: Symbol = topics.get(1).unwrap().into_val(&env);
        assert_eq!(t0, Symbol::new(&env, "wormhole"));
        assert_eq!(t1, Symbol::new(&env, "message_published"));

        let ev_data: ExpectedMessagePublishedEvent = event.2.clone().into_val(&env);
        assert_eq!(ev_data.nonce, nonce);
        assert_eq!(ev_data.sequence, 0);
        assert_eq!(ev_data.emitter_address, emitter_bytes32.clone());
        assert_eq!(ev_data.payload, payload.clone());
        assert_eq!(ev_data.consistency_level, cl);

        assert_eq!(client.get_emitter_sequence(&emitter), 1);

        let stored_hash0 = client
            .get_posted_message_hash(&emitter, &0)
            .expect("missing hash");

        let mut header = [0u8; 51];
        let mut off = 0usize;
        header[off..off + 4].copy_from_slice(&ts_u32.to_be_bytes());
        off += 4;
        header[off..off + 4].copy_from_slice(&nonce.to_be_bytes());
        off += 4;
        header[off..off + 2].copy_from_slice(&(CHAIN_ID_STELLAR as u16).to_be_bytes());
        off += 2;
        header[off..off + 32].copy_from_slice(&emitter_bytes32.to_array());
        off += 32;
        header[off..off + 8].copy_from_slice(&0u64.to_be_bytes());
        off += 8;
        header[off] = cl as u8;

        let mut msg_bytes = Bytes::from_array(&env, &header);
        msg_bytes.append(&payload);

        let expected_hash0 = keccak256_hash(&env, &msg_bytes);
        assert_eq!(stored_hash0, expected_hash0);

        let seq1 = client.post_message(&emitter, &nonce, &payload, &cl);
        assert_eq!(seq1, 1);
        assert_eq!(client.get_emitter_sequence(&emitter), 2);
    }

    #[test]
    fn test_post_message_fee_not_paid() {
        let env = Env::default();
        let contract_id = env.register(Wormhole, ());
        let client = WormholeClient::new(&env, &contract_id);

        // Initialize
        let guardian = BytesN::from_array(&env, &[0u8; 20]);
        let initial_guardians = vec![&env, guardian];
        let governance_emitter = BytesN::from_array(&env, &[1u8; 32]);
        client.initialize(&initial_guardians, &governance_emitter);

        let fee: u64 = 10_000;
        env.as_contract(&contract_id, || {
            env.storage()
                .persistent()
                .set(&StorageKey::MessageFee, &fee);
        });
        assert_eq!(client.get_message_fee(), fee);

        env.mock_all_auths();

        let emitter = <Address as TestAddress>::generate(&env);
        let nonce = 1u32;
        let payload = Bytes::from_array(&env, &[0x01, 0x02, 0x03]);
        let cl = ConsistencyLevel::Confirmed;

        let res = client.try_post_message(&emitter, &nonce, &payload, &cl);
        assert_eq!(res, Err(Ok(WormholeError::InsufficientFeePaid)));

        let events = env.events().all();
        assert_eq!(events.len(), 0);
    }

    #[test]
    fn test_emitter_sequence_increment() {
        let env = soroban_sdk::Env::default();
        let contract_id = env.register(Wormhole, ());
        let client = WormholeClient::new(&env, &contract_id);

        // initialize (fee defaults to 0)
        let guardian = BytesN::from_array(&env, &[0u8; 20]);
        let initial_guardians = soroban_sdk::vec![&env, guardian];
        let governance_emitter = BytesN::from_array(&env, &[1u8; 32]);
        client.initialize(&initial_guardians, &governance_emitter);

        env.mock_all_auths();

        let emitter = soroban_sdk::Address::generate(&env);
        let nonce = 42u32;
        let payload = Bytes::from_array(&env, &[0xDE, 0xAD, 0xBE, 0xEF]);
        let cl = ConsistencyLevel::Confirmed;

        let s0 = client.post_message(&emitter, &nonce, &payload, &cl);
        assert_eq!(s0, 0);
        assert_eq!(client.get_emitter_sequence(&emitter), 1);

        let s1 = client.post_message(&emitter, &nonce, &payload, &cl);
        assert_eq!(s1, 1);
        assert_eq!(client.get_emitter_sequence(&emitter), 2);

        let s2 = client.post_message(&emitter, &nonce, &payload, &cl);
        assert_eq!(s2, 2);
        assert_eq!(client.get_emitter_sequence(&emitter), 3);
    }

    #[test]
    fn test_posted_message_hash_stored() {
        let env = soroban_sdk::Env::default();
        let contract_id = env.register(Wormhole, ());
        let client = WormholeClient::new(&env, &contract_id);

        let guardian = BytesN::from_array(&env, &[0u8; 20]);
        let initial_guardians = vec![&env, guardian];
        let governance_emitter = BytesN::from_array(&env, &[1u8; 32]);
        client.initialize(&initial_guardians, &governance_emitter);

        env.mock_all_auths();

        let emitter = soroban_sdk::Address::generate(&env);
        let nonce = 7u32;
        let payload = Bytes::from_array(&env, &[0xAA, 0xBB, 0xCC]);
        let cl = ConsistencyLevel::Confirmed;

        let ts_u32 = u32::try_from(env.ledger().timestamp()).unwrap_or(0);

        let seq = client.post_message(&emitter, &nonce, &payload, &cl);
        assert_eq!(seq, 0);

        let stored = client
            .get_posted_message_hash(&emitter, &seq)
            .expect("missing stored hash");

        let emitter_bytes32 = address_to_bytes32(&env, &emitter);

        let mut header = [0u8; 51];
        let mut off = 0usize;

        header[off..off + 4].copy_from_slice(&ts_u32.to_be_bytes());
        off += 4;

        header[off..off + 4].copy_from_slice(&nonce.to_be_bytes());
        off += 4;

        header[off..off + 2].copy_from_slice(&CHAIN_ID_STELLAR.to_be_bytes());
        off += 2;

        header[off..off + 32].copy_from_slice(&emitter_bytes32.to_array());
        off += 32;

        header[off..off + 8].copy_from_slice(&seq.to_be_bytes());
        off += 8;

        header[off] = cl as u8;

        let mut msg_bytes = Bytes::from_array(&env, &header);
        msg_bytes.append(&payload);

        let expected = keccak256_hash(&env, &msg_bytes);

        assert_eq!(stored, expected);
    }
}
