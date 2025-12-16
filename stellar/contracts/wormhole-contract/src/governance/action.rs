use crate::{initialize, storage::StorageKey, utils::keccak256_hash, vaa::verify_vaa};
use core::convert::TryFrom;
use soroban_sdk::{Bytes, BytesN, Env};
use wormhole_soroban_client::{
    BytesReader, CHAIN_ID_STELLAR, GOVERNANCE_CHAIN_ID, GOVERNANCE_EMITTER, MODULE_CORE,
    STORAGE_TTL_EXTENSION, STORAGE_TTL_THRESHOLD, VAA, WormholeError,
};

pub trait GovernanceAction {
    type Payload: for<'a> TryFrom<(&'a Env, &'a Bytes), Error = WormholeError>;

    fn validate_payload(env: &Env, payload: &Self::Payload) -> Result<(), WormholeError>;
    fn execute(env: &Env, vaa: &VAA, payload: &Self::Payload) -> Result<(), WormholeError>;

    fn submit(env: &Env, vaa_bytes: Bytes) -> Result<(), WormholeError> {
        initialize::require_initialized(env)?;

        let (vaa, vaa_hash) = verify_and_hash_governance_vaa(env, &vaa_bytes)?;
        require_vaa_not_consumed(env, &vaa_hash)?;

        let payload = Self::Payload::try_from((env, &vaa.payload))?;
        Self::validate_payload(env, &payload)?;

        consume_vaa(env, &vaa_hash);

        Self::execute(env, &vaa, &payload)?;

        Ok(())
    }
}

fn verify_governance_vaa(vaa: &VAA) -> Result<(), WormholeError> {
    if vaa.emitter_chain != GOVERNANCE_CHAIN_ID {
        return Err(WormholeError::InvalidGovernanceChain);
    }

    if vaa.emitter_address.to_array() != GOVERNANCE_EMITTER {
        return Err(WormholeError::InvalidGovernanceEmitter);
    }

    Ok(())
}

fn verify_and_hash_governance_vaa(
    env: &Env,
    vaa_bytes: &Bytes,
) -> Result<(VAA, BytesN<32>), WormholeError> {
    let vaa = VAA::try_from((env, vaa_bytes))?;
    verify_governance_vaa(&vaa)?;
    verify_vaa(env, vaa_bytes)?;

    let body_bytes = vaa.serialize_body(env);
    let vaa_hash = keccak256_hash(env, &body_bytes);

    Ok((vaa, vaa_hash))
}

fn is_vaa_consumed(env: &Env, hash: &BytesN<32>) -> bool {
    env.storage()
        .persistent()
        .has(&StorageKey::ConsumedGovernanceVAA(hash.clone()))
}

fn require_vaa_not_consumed(env: &Env, hash: &BytesN<32>) -> Result<(), WormholeError> {
    if is_vaa_consumed(env, hash) {
        return Err(WormholeError::GovernanceVAAAlreadyConsumed);
    }
    Ok(())
}

fn consume_vaa(env: &Env, hash: &BytesN<32>) {
    env.storage()
        .persistent()
        .set(&StorageKey::ConsumedGovernanceVAA(hash.clone()), &true);

    env.storage().persistent().extend_ttl(
        &StorageKey::ConsumedGovernanceVAA(hash.clone()),
        STORAGE_TTL_THRESHOLD,
        STORAGE_TTL_EXTENSION,
    );
}

pub fn is_governance_vaa_consumed_from_bytes(
    env: &Env,
    vaa_bytes: &Bytes,
) -> Result<bool, WormholeError> {
    let body_bytes = VAA::get_body_bytes(vaa_bytes)?;
    let vaa_hash = keccak256_hash(env, &body_bytes);
    Ok(is_vaa_consumed(env, &vaa_hash))
}

pub fn parse_governance_header(
    env: &Env,
    reader: &mut BytesReader,
) -> Result<(BytesN<32>, u8, u16), WormholeError> {
    let module = reader.read_bytes_n::<32>()?;
    let action = reader.read_u8()?;
    let chain = reader.read_u16_be()?;

    Ok((BytesN::from_array(env, &module.to_array()), action, chain))
}

pub fn validate_governance_header(
    module: &BytesN<32>,
    action: u8,
    chain: u16,
    expected_action: u8,
) -> Result<(), WormholeError> {
    if module.to_array() != MODULE_CORE {
        return Err(WormholeError::InvalidGovernanceModule);
    }

    if action != expected_action {
        return Err(WormholeError::InvalidGovernanceAction);
    }

    if chain != 0 && chain != CHAIN_ID_STELLAR {
        return Err(WormholeError::InvalidGovernanceChain);
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use soroban_sdk::{Bytes, BytesN, Env};
    use wormhole_soroban_client::{CHAIN_ID_STELLAR, MODULE_CORE, WormholeError};
    use crate::governance::{GovernanceAction, TransferFeesAction};

    #[test]
    fn test_governance_header_validation() {
        let env = Env::default();

        fn mk_payload(env: &Env, module: BytesN<32>, action: u8, chain: u16) -> Bytes {
            let mut b = Bytes::new(env);
            b.append(&module.into());
            b.append(&Bytes::from_slice(env, &[action]));
            b.append(&Bytes::from_slice(env, &chain.to_be_bytes()));
            b.append(&Bytes::from_slice(env, &[0u8]));
            b
        }

        let expected_action: u8 = 4;

        {
            let mut not_core_arr = [0u8; 32];
            not_core_arr[0..7].copy_from_slice(b"NotCore");
            let not_core = BytesN::from_array(&env, &not_core_arr);

            let payload = mk_payload(&env, not_core.clone(), expected_action, CHAIN_ID_STELLAR);
            let parsed = <TransferFeesAction as GovernanceAction>::Payload::try_from((&env, &payload));

            if let Ok(p) = parsed {
                let r = <TransferFeesAction as GovernanceAction>::validate_payload(&env, &p);
                assert_eq!(r, Err(WormholeError::InvalidGovernanceModule));
            } else {
                let r = validate_governance_header(
                    &not_core,
                    expected_action,
                    CHAIN_ID_STELLAR,
                    expected_action,
                );
                assert_eq!(r, Err(WormholeError::InvalidGovernanceModule));
            }
        }

        {
            let wrong_action: u8 = 0xFF;
            let payload = mk_payload(
                &env,
                BytesN::from_array(&env, &MODULE_CORE),
                wrong_action,
                CHAIN_ID_STELLAR,
            );
            let parsed = <TransferFeesAction as GovernanceAction>::Payload::try_from((&env, &payload));

            if let Ok(p) = parsed {
                let r = <TransferFeesAction as GovernanceAction>::validate_payload(&env, &p);
                assert_eq!(r, Err(WormholeError::InvalidGovernanceAction));
            } else {
                let r = validate_governance_header(
                    &BytesN::from_array(&env, &MODULE_CORE),
                    wrong_action,
                    CHAIN_ID_STELLAR,
                    expected_action,
                );
                assert_eq!(r, Err(WormholeError::InvalidGovernanceAction));
            }
        }

        {
            let chain_wrong: u16 = 1;
            let payload = mk_payload(
                &env,
                BytesN::from_array(&env, &MODULE_CORE),
                expected_action,
                chain_wrong,
            );
            let parsed = <TransferFeesAction as GovernanceAction>::Payload::try_from((&env, &payload));

            if let Ok(p) = parsed {
                let r = <TransferFeesAction as GovernanceAction>::validate_payload(&env, &p);
                assert_eq!(r, Err(WormholeError::InvalidGovernanceChain));
            } else {
                let r = validate_governance_header(
                    &BytesN::from_array(&env, &MODULE_CORE),
                    expected_action,
                    chain_wrong,
                    expected_action,
                );
                assert_eq!(r, Err(WormholeError::InvalidGovernanceChain));
            }
        }

        {
            let r0 = validate_governance_header(
                &BytesN::from_array(&env, &MODULE_CORE),
                expected_action,
                0,
                expected_action,
            );
            assert!(r0.is_ok());

            let r1 = validate_governance_header(
                &BytesN::from_array(&env, &MODULE_CORE),
                expected_action,
                CHAIN_ID_STELLAR,
                expected_action,
            );
            assert!(r1.is_ok());
        }
    }
}