use crate::governance::action::{
    GovernanceAction, parse_governance_header, validate_governance_header,
};
use core::convert::TryFrom;
use soroban_sdk::{Bytes, BytesN, Env, contractevent};
use wormhole_soroban_client::{
    ACTION_CONTRACT_UPGRADE, BytesReader, CONTRACT_UPGRADE_PAYLOAD_MIN_LENGTH, WormholeError, VAA
};

/// Event published when a contract upgrade is executed.
///
/// Topics: ["wormhole_core", "upgrade"]
/// - "wormhole_core": Namespace for all core contract governance/lifecycle events
/// - "upgrade": Event type for contract upgrades
#[contractevent(topics = ["wormhole_core", "upgrade"], data_format = "single-value")]
struct ContractUpgradeEvent {
    new_contract_hash: BytesN<32>,
}

#[derive(Debug, PartialEq)]
pub struct ContractUpgradePayload {
    pub module: BytesN<32>,
    pub action: u8,
    pub chain: u16,
    pub new_contract_hash: BytesN<32>,
}

impl<'a> TryFrom<(&'a Env, &'a Bytes)> for ContractUpgradePayload {
    type Error = WormholeError;

    fn try_from(value: (&'a Env, &'a Bytes)) -> Result<Self, Self::Error> {
        let (env, payload) = value;

        if payload.len() < CONTRACT_UPGRADE_PAYLOAD_MIN_LENGTH {
            return Err(WormholeError::InvalidPayload);
        }

        let mut reader = BytesReader::new(payload);
        let (module, action, chain) = parse_governance_header(env, &mut reader)?;
        let new_contract_hash = reader.read_bytes_n::<32>()?;

        Ok(ContractUpgradePayload {
            module,
            action,
            chain,
            new_contract_hash,
        })
    }
}

impl ContractUpgradePayload {
    fn validate(&self) -> Result<(), WormholeError> {
        validate_governance_header(
            &self.module,
            self.action,
            self.chain,
            ACTION_CONTRACT_UPGRADE,
        )
    }
}

pub struct ContractUpgradeAction;

impl GovernanceAction for ContractUpgradeAction {
    type Payload = ContractUpgradePayload;

    fn validate_payload(_env: &Env, payload: &Self::Payload) -> Result<(), WormholeError> {
        payload.validate()
    }

    fn execute(
        env: &Env,
        _vaa: &VAA,
        payload: &Self::Payload,
    ) -> Result<(), WormholeError> {
        env.deployer()
            .update_current_contract_wasm(payload.new_contract_hash.clone());

        ContractUpgradeEvent {
            new_contract_hash: payload.new_contract_hash.clone(),
        }
        .publish(env);

        Ok(())
    }
}


#[cfg(test)]
mod tests {
    use soroban_sdk::testutils::Events;
    use soroban_sdk::{IntoVal, Symbol, Vec};
    use super::*;
    #[test]
    fn test_contract_upgrade_execute_event() {
        let env = Env::default();
        let contract_id = env.register(crate::Wormhole, ());
        let client = crate::WormholeClient::new(&env, &contract_id);

        let g = BytesN::<20>::from_array(&env, &[0u8; 20]);
        let mut guardians = Vec::new(&env);
        guardians.push_back(g);

        let governance_emitter = BytesN::<32>::from_array(&env, &[1u8; 32]);
        client.initialize(&guardians, &governance_emitter);

        let wasm = Bytes::from_slice(&env, &[0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00]);
        let new_contract_hash = env.deployer().upload_contract_wasm(wasm);

        let payload = ContractUpgradePayload {
            module: BytesN::<32>::from_array(&env, &wormhole_soroban_client::MODULE_CORE),
            action: wormhole_soroban_client::ACTION_CONTRACT_UPGRADE,
            chain: 0u16,
            new_contract_hash: new_contract_hash.clone(),
        };

        let vaa = VAA {
            version: 1,
            guardian_set_index: 0,
            signatures: Vec::new(&env),
            timestamp: 0,
            nonce: 0,
            emitter_chain: u32::from(wormhole_soroban_client::GOVERNANCE_CHAIN_ID),
            emitter_address: BytesN::<32>::from_array(&env, &wormhole_soroban_client::GOVERNANCE_EMITTER),
            sequence: 0,
            consistency_level: wormhole_soroban_client::ConsistencyLevel::Confirmed,
            payload: Bytes::new(&env),
        };

        env.as_contract(&contract_id, || {
            let r = <ContractUpgradeAction as GovernanceAction>::execute(&env, &vaa, &payload);
            assert!(r.is_ok());
        });

        let events = env.events().all();
        assert_eq!(events.len(), 1);

        let e = events.last().unwrap();
        let topics = e.1.clone();

        let t0: Symbol = topics.get(0).unwrap().into_val(&env);
        let t1: Symbol = topics.get(1).unwrap().into_val(&env);

        assert_eq!(t0, Symbol::new(&env, "wormhole_core"));
        assert_eq!(t1, Symbol::new(&env, "upgrade"));

        let data: BytesN<32> = e.2.clone().into_val(&env);
        assert_eq!(data, new_contract_hash);
    }
}