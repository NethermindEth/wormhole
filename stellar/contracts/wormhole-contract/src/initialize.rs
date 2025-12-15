use crate::{
    governance::guardian_set::{set_current_index, store},
    storage::StorageKey,
};
use soroban_sdk::{BytesN, Env, Vec, contractevent};
use wormhole_soroban_client::*;

/// Event published when the contract is initialized.
///
/// Topics: ["wormhole_core", "init"]
/// - "wormhole_core": Namespace for all core contract governance/lifecycle events
/// - "init": Event type for initialization
#[contractevent(topics = ["wormhole_core", "init"])]
struct InitializeEvent {
    chain_id: u32,
    guardian_count: u32,
    governance_chain_id: u32,
    governance_emitter: BytesN<32>,
}

pub fn is_initialized(env: &Env) -> bool {
    env.storage()
        .persistent()
        .get(&StorageKey::Initialized)
        .unwrap_or(false)
}

/// Ensures the contract is initialized, returning an Error if not.
pub fn require_initialized(env: &Env) -> Result<(), WormholeError> {
    if !is_initialized(env) {
        return Err(WormholeError::NotInitialized);
    }
    Ok(())
}

fn set_initialized(env: &Env) {
    env.storage()
        .persistent()
        .set(&StorageKey::Initialized, &true);
}

pub fn initialize(
    env: &Env,
    initial_guardians: Vec<BytesN<20>>,
    governance_emitter: BytesN<32>,
) -> Result<(), WormholeError> {
    if is_initialized(env) {
        return Err(WormholeError::AlreadyInitialized);
    }

    // Validate initial guardians
    if initial_guardians.is_empty() {
        return Err(WormholeError::EmptyGuardianSet);
    }

    // Set contract as its own admin for upgrades
    let contract_address = env.current_contract_address();
    env.storage()
        .instance()
        .set(&StorageKey::Admin, &contract_address);

    // Store governance emitter address
    env.storage()
        .persistent()
        .set(&StorageKey::GovernanceEmitter, &governance_emitter);

    // Extend TTL for governance emitter (it's permanent config)
    env.storage().persistent().extend_ttl(
        &StorageKey::GovernanceEmitter,
        STORAGE_TTL_THRESHOLD,
        STORAGE_TTL_EXTENSION,
    );

    // Create the initial guardian set (always index 0)
    let guardian_set = GuardianSetInfo {
        keys: initial_guardians.clone(),
        creation_time: env.ledger().timestamp(),
    };

    store(env, 0, guardian_set)?;

    set_current_index(env, 0);

    set_initialized(env);

    // Emit initialization event
    InitializeEvent {
        chain_id: u32::from(CHAIN_ID_STELLAR),
        guardian_count: initial_guardians.len(),
        governance_chain_id: GOVERNANCE_CHAIN_ID,
        governance_emitter: governance_emitter.clone(),
    }
    .publish(env);

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{Wormhole, WormholeClient};
    use soroban_sdk::{IntoVal, Symbol, testutils::Events, vec};

    #[test]
    fn test_initialize_success() {
        let env = Env::default();
        let contract_id = env.register(Wormhole, ());
        let client = WormholeClient::new(&env, &contract_id);

        let guardian = BytesN::from_array(&env, &[0u8; 20]);
        let initial_guardians = vec![&env, guardian.clone()];
        let governance_emitter = BytesN::from_array(&env, &[1u8; 32]);

        client.initialize(&initial_guardians, &governance_emitter);

        let events = env.events().all();
        assert_eq!(events.len(), 1);

        let event = events.last().unwrap();
        let topics = event.1.clone();
        let t0: Symbol = topics.get(0).unwrap().into_val(&env);
        let t1: Symbol = topics.get(1).unwrap().into_val(&env);
        assert_eq!(t0, Symbol::new(&env, "wormhole_core"));
        assert_eq!(t1, Symbol::new(&env, "init"));

        assert_eq!(event.0, contract_id);

        assert!(client.is_initialized());
        assert_eq!(client.get_current_guardian_set_index(), 0);
        let set = client.get_guardian_set(&0);
        assert_eq!(set.keys.len(), 1);
        assert_eq!(set.keys.get(0).unwrap(), guardian);
    }

    #[test]
    fn test_initialize_empty_guardians() {
        let env = Env::default();
        let contract_id = env.register(Wormhole, ());
        let client = WormholeClient::new(&env, &contract_id);

        let initial_guardians: Vec<BytesN<20>> = Vec::new(&env);
        let governance_emitter = BytesN::from_array(&env, &[1u8; 32]);

        let res = client.try_initialize(&initial_guardians, &governance_emitter);
        assert_eq!(res, Err(Ok(WormholeError::EmptyGuardianSet)));

        let events = env.events().all();
        assert_eq!(events.len(), 0);

        assert!(!client.is_initialized());
    }

    #[test]
    fn test_initialize_already_initialized() {
        let env = Env::default();
        let contract_id = env.register(Wormhole, ());
        let client = WormholeClient::new(&env, &contract_id);

        let guardian = BytesN::from_array(&env, &[0u8; 20]);
        let initial_guardians = vec![&env, guardian];
        let governance_emitter = BytesN::from_array(&env, &[1u8; 32]);

        client.initialize(&initial_guardians, &governance_emitter);

        let events = env.events().all();
        assert_eq!(events.len(), 1);

        let res2 = client.try_initialize(&initial_guardians, &governance_emitter);
        assert_eq!(res2, Err(Ok(WormholeError::AlreadyInitialized)));

        let events_after_fail = env.events().all();
        assert_eq!(events_after_fail.len(), 0);

        assert!(client.is_initialized());
    }

    #[test]
    fn test_is_initialized_flag() {
        let env = Env::default();
        let contract_id = env.register(Wormhole, ());
        let client = WormholeClient::new(&env, &contract_id);

        assert!(!client.is_initialized());

        let guardian = BytesN::from_array(&env, &[0u8; 20]);
        let initial_guardians = vec![&env, guardian];
        let governance_emitter = BytesN::from_array(&env, &[1u8; 32]);

        client.initialize(&initial_guardians, &governance_emitter);

        assert!(client.is_initialized());
    }
}
