mod setup {
    use dep::std;
    use dep::aztec::{context, upgrade};
    
    use crate::structs::{GuardianSet, UpgradeEvent};

    use crate::setters::{
        initialize_state,
        store_guardian_set,
        set_chain_id,
        set_governance_chain_id,
        store_governance_contract,
        set_aztec_chain_id,
        set_initialized
    };

    // Setup the Wormhole contract
    fn setup(
        implementation: PublicKey,
        initial_guardians: [PublicKey],
        chain_id: Field,
        governance_chain_id: Field,
        governance_contract: [u8; 32],
        aztec_chain_id: Field
    ) {
        // Require at least one guardian
        assert(initial_guardians.len() > 0, "no guardians specified");
        
        // Create initial guardian set
        let initial_guardian_set = GuardianSet {
            keys: initial_guardians,
            expiration_time: 0 // Never expires
        };
        
        // Store initial guardian set at index 0
        store_guardian_set(initial_guardian_set, 0);
        // Initial guardian set index is 0 (default)
        
        // Set chain ID
        set_chain_id(chain_id);
        
        // Set governance chain ID and contract
        set_governance_chain_id(governance_chain_id);
        store_governance_contract(governance_contract);
        
        // Set Aztec chain ID
        set_aztec_chain_id(aztec_chain_id);
        
        // Upgrade to implementation
        upgrade_to(implementation);
        
        // Mark implementation as initialized
        // See https://github.com/wormhole-foundation/wormhole/issues/1930
        set_initialized(implementation);
    }
    
    // Helper function to upgrade to a new implementation
    fn upgrade_to(implementation: PublicKey) {
        // Get the current proxy address
        let proxy_address = context::get_contract_address();
        
        // Ensure the caller has upgrade permissions
        let caller = context::msg_sender();
        assert(proxy_admin::is_admin(proxy_address, caller), "Caller is not the admin");
        
        // Perform the upgrade
        proxy_admin::upgrade_to(proxy_address, implementation);
        
        // Emit an upgrade event
        let event = UpgradeEvent {
            proxy: proxy_address,
            implementation: implementation
        };
        context::emit_event(event);
    }
}