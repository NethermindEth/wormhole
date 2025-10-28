#![no_std]

use soroban_sdk::{
    contract, contractevent, contractimpl, contracttype, panic_with_error, Address, Bytes, BytesN,
    Env, Symbol, TryFromVal,
};
use wormhole_interface::{Wormhole as WormholeInterface, WormholeError};

#[derive(Clone)]
#[contracttype]
enum DataKey {
    Owner,
    Seq(BytesN<32>),
}

#[derive(Clone, Debug, Eq, PartialEq)]
#[contracttype]
pub struct MessageData {
    pub nonce: u32,
    pub payload: Bytes,
    pub consistency: u32,
}

#[contractevent]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MessagePublished {
    #[topic]
    pub emitter: BytesN<32>,
    #[topic]
    pub sequence: u64,
    pub nonce: u32,
    pub payload: Bytes,
    pub consistency: u32,
}

#[contract]
pub struct Wormhole;

#[contractimpl]
impl WormholeInterface for Wormhole {
    fn initialize(env: Env, owner: Address) {
        if env.storage().persistent().has(&DataKey::Owner) {
            panic_with_error!(&env, WormholeError::AlreadyInitialized);
        }
        env.storage().persistent().set(&DataKey::Owner, &owner);
        // TODO Store guardians here
    }

    fn publish_message(
        env: Env,
        emitter: BytesN<32>,
        nonce: u32,
        payload: Bytes,
        consistency: u32,
    ) -> u64 {
        // TODO: Authorization checks

        let seq = bump_sequence(&env, &emitter);

        MessagePublished {
            emitter: emitter.clone(),
            sequence: seq,
            nonce,
            payload,
            consistency,
        }
            .publish(&env);

        seq
    }

    fn sequence_of(env: Env, emitter: BytesN<32>) -> u64 {
        get_sequence(&env, &emitter)
    }
}

fn get_sequence(env: &Env, emitter: &BytesN<32>) -> u64 {
    env.storage()
        .persistent()
        .get(&DataKey::Seq(emitter.clone()))
        .unwrap_or(0)
}

fn bump_sequence(env: &Env, emitter: &BytesN<32>) -> u64 {
    let next = get_sequence(env, emitter).saturating_add(1);
    env.storage()
        .persistent()
        .set(&DataKey::Seq(emitter.clone()), &next);
    next
}

#[cfg(all(test, not(target_arch = "wasm32")))]
mod tests {
    use super::*;
    use soroban_sdk::{
        testutils::{Address as _, Events},
        Address, Bytes, BytesN, Env,
    };
    use wormhole_interface::WormholeClient;

    fn setup() -> (Env, Address, WormholeClient<'static>) {
        let env = Env::default();
        env.mock_all_auths();
        let id = env.register(Wormhole, ());
        let client = WormholeClient::new(&env, &id);
        let owner = Address::generate(&env);
        client.initialize(&owner);
        (env, owner, client)
    }

    #[derive(Debug, PartialEq)]
    struct DecodedEvent {
        emitter: BytesN<32>,
        sequence: u64,
        data: MessageData,
    }

    fn get_last_message_published(env: &Env) -> DecodedEvent {
        let events = env.events().all();
        assert!(!events.is_empty(), "no events recorded");
        let (_contract_id, topics, data) = events.last().unwrap();

        // topics: [ MessagePublished, emitter, sequence ]
        assert_eq!(topics.len(), 3, "incorrect number of topics");

        let t0 = Symbol::try_from_val(env, &topics.get_unchecked(0)).expect("topic 0 decode failed");
        assert_eq!(
            t0,
            Symbol::new(env, "message_published"),
            "incorrect event name"
        );

        let emitter = BytesN::<32>::try_from_val(env, &topics.get_unchecked(1))
            .expect("topic 1 decode failed (emitter)");
        let sequence =
            u64::try_from_val(env, &topics.get_unchecked(2)).expect("topic 2 decode failed (seq)");

        let message_data = MessageData::try_from_val(env, &data).expect("data decode failed");

        DecodedEvent {
            emitter,
            sequence,
            data: message_data,
        }
    }

    #[test]
    fn initial_sequence_is_zero() {
        let (env, _, client) = setup();
        let emitter = BytesN::<32>::from_array(&env, &[0xAA; 32]);
        assert_eq!(client.sequence_of(&emitter), 0);
    }

    #[test]
    fn publish_increments_sequence_and_emits_event() {
        let (env, _owner, client) = setup();

        let emitter = BytesN::<32>::from_array(&env, &[7u8; 32]);
        let payload = Bytes::from_array(&env, &[1, 2, 3, 4]);
        let consistency = 1u32;

        let before_len: u32 = env.events().all().len();

        let s1 = client.publish_message(&emitter, &7u32, &payload, &consistency);

        // Check events immediately (no intervening call that would reset the buffer).
        let mid_len: u32 = env.events().all().len();
        assert!(mid_len > before_len, "Event count did not increase");
        
        assert_eq!(s1, 1);
        assert_eq!(client.sequence_of(&emitter), 1);

        let nonce2 = 8u32;
        let s2 = client.publish_message(&emitter, &nonce2, &payload, &consistency);
        assert_eq!(s2, 2);

        let event = get_last_message_published(&env);
        assert_eq!(event.sequence, s2);
        assert_eq!(event.emitter, emitter);
        assert_eq!(event.data.nonce, nonce2);
        assert_eq!(event.data.payload, payload);
        assert_eq!(event.data.consistency, consistency);
    }

    #[test]
    fn sequences_are_per_emitter() {
        let (env, _, client) = setup();

        let a = BytesN::<32>::from_array(&env, &[1u8; 32]);
        let b = BytesN::<32>::from_array(&env, &[2u8; 32]);
        let payload = Bytes::from_array(&env, &[]);
        let consistency = 1u32;

        client.publish_message(&a, &1u32, &payload, &consistency);
        client.publish_message(&a, &2u32, &payload, &consistency);
        client.publish_message(&b, &3u32, &payload, &consistency);

        assert_eq!(client.sequence_of(&a), 2);
        assert_eq!(client.sequence_of(&b), 1);
    }

    #[test]
    fn last_event_topics_and_data_roundtrip() {
        let (env, _, client) = setup();

        let emitter = BytesN::<32>::from_array(&env, &[0x11; 32]);
        let payload = Bytes::from_array(&env, &[9, 9, 9]);
        let nonce = 42u32;
        let consistency = 5u32;

        let seq = client.publish_message(&emitter, &nonce, &payload, &consistency);
        assert_eq!(seq, 1);

        let event = get_last_message_published(&env);

        assert_eq!(event.emitter, emitter);
        assert_eq!(event.sequence, seq);
        assert_eq!(event.data.payload, payload);
        assert_eq!(event.data.nonce, nonce);
        assert_eq!(event.data.consistency, consistency);
    }

    #[test]
    fn many_publishes_monotonic_sequence() {
        let (env, _, client) = setup();

        let emitter = BytesN::<32>::from_array(&env, &[0x55; 32]);
        let payload = Bytes::from_array(&env, &[0xAB]);
        let consistency = 1u32;

        let mut last = 0u64;
        for i in 0..10u32 {
            let s = client.publish_message(&emitter, &i, &payload, &consistency);
            assert_eq!(s, (i as u64) + 1);
            assert!(s > last);
            last = s;
        }
        assert_eq!(client.sequence_of(&emitter), 10);
    }
}