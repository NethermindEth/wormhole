mod getters {
    use dep::std;
    use dep::aztec::{context};
    
    use crate::structs::{GuardianSet};
    use crate::state::{
        get_guardian_set as get_guardian_set_internal,
        get_guardian_set_index as get_guardian_set_index_internal,
        get_guardian_set_expiry as get_guardian_set_expiry_internal,
        is_governance_action_consumed as is_governance_action_consumed_internal,
        is_initialized as is_initialized_internal,
        get_chain_id as get_chain_id_internal,
        get_aztec_chain_id as get_aztec_chain_id_internal,
        get_governance_chain_id as get_governance_chain_id_internal,
        get_governance_contract as get_governance_contract_internal,
        get_message_fee as get_message_fee_internal,
        get_sequence as get_sequence_internal
    };
    
    // Get a guardian set by index
    fn get_guardian_set(index: Field) -> GuardianSet {
        get_guardian_set_internal(index)
    }
    
    // Get current guardian set index
    fn get_current_guardian_set_index() -> Field {
        get_guardian_set_index_internal()
    }
    
    // Get guardian set expiry period
    fn get_guardian_set_expiry() -> Field {
        get_guardian_set_expiry_internal()
    }
    
    // Check if a governance action has been consumed
    fn governance_action_is_consumed(hash: [u8; 32]) -> bool {
        is_governance_action_consumed_internal(hash)
    }
    
    // Check if an implementation is initialized
    fn is_initialized(impl_address: PublicKey) -> bool {
        is_initialized_internal(impl_address)
    }
    
    // Get the Wormhole chain ID
    fn chain_id() -> Field {
        get_chain_id_internal()
    }
    
    // Get the Aztec chain ID
    fn aztec_chain_id() -> Field {
        get_aztec_chain_id_internal()
    }
    
    // Check if this is running on a fork
    fn is_fork() -> bool {
        aztec_chain_id() != context::get_current_block().chain_id
    }
    
    // Get the governance chain ID
    fn governance_chain_id() -> Field {
        get_governance_chain_id_internal()
    }
    
    // Get the governance contract address
    fn governance_contract() -> [u8; 32] {
        get_governance_contract_internal()
    }
    
    // Get the message fee
    fn message_fee() -> Field {
        get_message_fee_internal()
    }
    
    // Get next sequence number for an emitter
    fn next_sequence(emitter: PublicKey) -> Field {
        get_sequence_internal(emitter)
    }
}