#![no_std]

use soroban_sdk::{
    Bytes, BytesN, Env, IntoVal, Symbol, Val, contract, contractevent, contractimpl, contracttype,
};
use wormhole_interface::{EVENT_TOPIC_LMP, Wormhole as WormholeInterface};

#[derive(Clone)]
#[contracttype]
enum DataKey {
    Seq(BytesN<32>),
}

/// Event payload published under topic (EVENT_TOPIC_LMP, emitter, sequence).
#[contractevent]
pub struct LmpData {
    pub payload: Bytes,
    pub nonce: u32,
    pub consistency: u32,
}

impl IntoVal<Env, Val> for LmpData {
    fn into_val(&self, env: &Env) -> Val {
        (&self.payload, self.nonce, self.consistency).into_val(env)
    }
}

#[contract]
pub struct Wormhole;

#[contractimpl]
impl WormholeInterface for Wormhole {
    fn publish_message(
        env: Env,
        emitter: BytesN<32>,
        nonce: u32,
        payload: Bytes,
        consistency: u32,
    ) -> u64 {
        let seq = bump_sequence(&env, &emitter);
        env.events().publish(
            (EVENT_TOPIC_LMP, emitter.clone(), seq),
            LmpData {
                payload,
                nonce,
                consistency,
            },
        );
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
        Bytes, BytesN, Env, Symbol, TryFromVal, Vec, symbol_short, testutils::Events,
    };

    fn setup() -> (Env, soroban_sdk::Address) {
        let env = Env::default();
        let id = env.register(Wormhole, ());
        (env, id)
    }

    fn last_event(
        env: &Env,
    ) -> (
        soroban_sdk::Address,
        Vec<soroban_sdk::Val>,
        soroban_sdk::Val,
    ) {
        let all: Vec<(
            soroban_sdk::Address,
            Vec<soroban_sdk::Val>,
            soroban_sdk::Val,
        )> = env.events().all();
        assert!(all.len() > 0, "no events recorded");
        all.get_unchecked(all.len() - 1)
    }

    #[test]
    fn event_topic_constant_is_lmp() {
        assert_eq!(EVENT_TOPIC_LMP, symbol_short!("LMP"));
    }

    #[test]
    fn initial_sequence_is_zero() {
        let (env, id) = setup();
        let client = WormholeClient::new(&env, &id);
        let emitter = BytesN::<32>::from_array(&env, &[0xAA; 32]);
        assert_eq!(client.sequence_of(&emitter), 0);
    }

    #[test]
    fn publish_increments_sequence_and_emits_event() {
        let (env, id) = setup();
        let client = WormholeClient::new(&env, &id);

        let emitter = BytesN::<32>::from_array(&env, &[7u8; 32]);
        let payload = Bytes::from_array(&env, &[1, 2, 3, 4]);

        let before_len: u32 = env.events().all().len();

        let s1 = client.publish_message(&emitter, &7u32, &payload, &1u32);
        assert_eq!(s1, 1);
        assert_eq!(client.sequence_of(&emitter), 1);
        let mid_len: u32 = env.events().all().len();
        assert_eq!(mid_len, before_len + 1);

        let s2 = client.publish_message(&emitter, &8u32, &payload, &1u32);
        assert_eq!(s2, 2);
        assert_eq!(client.sequence_of(&emitter), 2);
        let after_len: u32 = env.events().all().len();
        assert_eq!(after_len, mid_len + 1);

        let (_addr, topics, data) = last_event(&env);
        assert_eq!(topics.len(), 3);

        let t0: Symbol = Symbol::try_from_val(&env, &topics.get_unchecked(0)).unwrap();
        let t1: BytesN<32> = BytesN::<32>::try_from_val(&env, &topics.get_unchecked(1)).unwrap();
        let t2: i128 = i128::try_from_val(&env, &topics.get_unchecked(2)).unwrap();

        assert_eq!(t0, EVENT_TOPIC_LMP);
        assert_eq!(t1, emitter);
        assert_eq!(t2, s2 as i128);

        let decoded: (Bytes, u32, u32) = <(Bytes, u32, u32)>::try_from_val(&env, &data).unwrap();
        assert_eq!(decoded.0, payload);
        assert_eq!(decoded.1, 8);
        assert_eq!(decoded.2, 1);
    }

    #[test]
    fn sequences_are_per_emitter() {
        let (env, id) = setup();
        let client = WormholeClient::new(&env, &id);

        let a = BytesN::<32>::from_array(&env, &[1u8; 32]);
        let b = BytesN::<32>::from_array(&env, &[2u8; 32]);
        let payload = Bytes::from_array(&env, &[]);

        assert_eq!(client.sequence_of(&a), 0);
        assert_eq!(client.sequence_of(&b), 0);

        assert_eq!(client.publish_message(&a, &1u32, &payload, &1u32), 1);
        assert_eq!(client.publish_message(&a, &2u32, &payload, &1u32), 2);
        assert_eq!(client.publish_message(&b, &3u32, &payload, &1u32), 1);

        assert_eq!(client.sequence_of(&a), 2);
        assert_eq!(client.sequence_of(&b), 1);
    }

    #[test]
    fn last_event_topics_and_data_roundtrip() {
        let (env, id) = setup();
        let client = WormholeClient::new(&env, &id);

        let emitter = BytesN::<32>::from_array(&env, &[0x11; 32]);
        let payload = Bytes::from_array(&env, &[9, 9, 9]);

        let seq = client.publish_message(&emitter, &42u32, &payload, &5u32);
        assert_eq!(seq, 1);

        let (_addr, topics, data) = last_event(&env);

        let t0: Symbol = Symbol::try_from_val(&env, &topics.get_unchecked(0)).unwrap();
        let t1: BytesN<32> = BytesN::<32>::try_from_val(&env, &topics.get_unchecked(1)).unwrap();
        let t2: i128 = i128::try_from_val(&env, &topics.get_unchecked(2)).unwrap();

        assert_eq!(t0, EVENT_TOPIC_LMP);
        assert_eq!(t1, emitter);
        assert_eq!(t2, 1);

        let decoded: (Bytes, u32, u32) = <(Bytes, u32, u32)>::try_from_val(&env, &data).unwrap();
        assert_eq!(decoded.0, payload);
        assert_eq!(decoded.1, 42);
        assert_eq!(decoded.2, 5);
    }

    #[test]
    fn many_publishes_monotonic_sequence() {
        let (env, id) = setup();
        let client = WormholeClient::new(&env, &id);

        let emitter = BytesN::<32>::from_array(&env, &[0x55; 32]);
        let payload = Bytes::from_array(&env, &[0xAB]);

        let mut last = 0u64;
        for i in 0..10u32 {
            let s = client.publish_message(&emitter, &i, &payload, &1u32);
            assert_eq!(s, (i as u64) + 1);
            assert!(s > last);
            last = s;
        }
        assert_eq!(client.sequence_of(&emitter), 10);
    }
}
