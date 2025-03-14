
mod governance {
    use dep::std;
    use dep::aztec::{context, state, upgrade, contract, token, crypto};
    
    use crate::structs::{VM};
    use crate::messages::{parse_vm, verify_vm};
    use crate::governance_structs::{
        ContractUpgrade,
        GuardianSetUpgrade,
        SetMessageFee,
        TransferFees,
        RecoverChainId,
        parse_contract_upgrade,
        parse_set_message_fee,
        parse_guardian_set_upgrade,
        parse_transfer_fees,
        parse_recover_chain_id
    };
    use crate::getters{
        is_governance_action_consumed,
        get_guardian_set_index,
        get_chain_id,
        get_governance_chain_id,
        get_governance_contract,
        get_aztec_chain_id
    };
    use crate::setters{
        set_governance_action_consumed,
        set_chain_id,
        set_aztec_chain_id,
        set_message_fee,
        expire_guardian_set,
        store_guardian_set,
        set_guardian_set_index
    }
    
    // Event definitions
    struct ContractUpgradedEvent {
        old_contract: PublicKey,
        new_contract: PublicKey
    }
    
    struct GuardianSetAddedEvent {
        index: Field
    }
    
    // "Core" module (left padded)
    // 0x00000000000000000000000000000000000000000000000000000000436f7265
    const MODULE: [u8; 32] = [
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 67, 111, 114, 101
    ]; // "Core" in hex
    
    // Check if this is a fork
    fn is_fork() -> bool {
        get_aztec_chain_id() != context::get_current_block().chain_id
    }
    
    // Submit a contract upgrade via Governance VM
    fn submit_contract_upgrade(encoded_vm: [u8]) {
        // Ensure we're not on a fork
        assert(!is_fork(), "invalid fork");
        
        // Parse the VM
        let vm = parse_vm(encoded_vm);
        
        // Verify the VM is valid before processing it
        let (is_valid, reason) = verify_governance_vm(vm);
        assert(is_valid, reason);
        
        // Parse the contract upgrade payload
        let upgrade = parse_contract_upgrade(vm.payload);
        
        // Verify the VM is for this module
        assert(upgrade.module == MODULE, "Invalid Module");
        
        // Verify the VM is for this chain
        assert(upgrade.chain == get_chain_id(), "Invalid Chain");
        
        // Record the governance action as consumed
        set_governance_action_consumed(vm.hash);
        
        // Upgrade the implementation to the new contract
        let old_contract = upgrade_implementation(upgrade.new_contract);
        
        // Emit event for the upgrade
        emit_contract_upgraded(old_contract, upgrade.new_contract);
    }
    
    // Set message fee via Governance VM
    fn submit_set_message_fee(encoded_vm: [u8]) {
        // Parse the VM
        let vm = parse_vm(encoded_vm);
        
        // Verify the VM is valid before processing it
        let (is_valid, reason) = verify_governance_vm(vm);
        assert(is_valid, reason);
        
        // Parse the set message fee payload
        let fee_update = parse_set_message_fee(vm.payload);
        
        // Verify the VM is for this module
        assert(fee_update.module == MODULE, "Invalid Module");
        
        // Verify the VM is for this chain
        assert(fee_update.chain == get_chain_id() && !is_fork(), "Invalid Chain");
        
        // Record the governance action as consumed to prevent reentry
        set_governance_action_consumed(vm.hash);
        
        // Update the message fee
        set_message_fee(fee_update.message_fee);
    }
    
    // Deploy a new guardian set via Governance VM
    fn submit_new_guardian_set(encoded_vm: [u8]) {
        // Parse the VM
        let vm = parse_vm(encoded_vm);
        
        // Verify the VM is valid before processing it
        let (is_valid, reason) = verify_governance_vm(vm);
        assert(is_valid, reason);
        
        // Parse the guardian set upgrade payload
        let upgrade = parse_guardian_set_upgrade(vm.payload);
        
        // Verify the VM is for this module
        assert(upgrade.module == MODULE, "invalid Module");
        
        // Verify the VM is for this chain
        assert((upgrade.chain == get_chain_id() && !is_fork()) || upgrade.chain == 0, "invalid Chain");
        
        // Verify the Guardian Set keys are not empty
        assert(upgrade.new_guardian_set.keys.len() > 0, "new guardian set is empty");
        
        // Verify that the index is incrementing via a predictable +1 pattern
        assert(upgrade.new_guardian_set_index == get_guardian_set_index() + 1, 
              "index must increase in steps of 1");
        
        // Record the governance action as consumed to prevent reentry
        set_governance_action_consumed(vm.hash);
        
        // Trigger a time-based expiry of current guardian set
        expire_guardian_set(get_guardian_set_index());
        
        // Add the new guardian set to guardian sets
        store_guardian_set(upgrade.new_guardian_set, upgrade.new_guardian_set_index);
        
        // Make the new guardian set effective
        set_guardian_set_index(upgrade.new_guardian_set_index);
        
        // Emit event for guardian set added
        emit_guardian_set_added(upgrade.new_guardian_set_index);
    }
    
    // Submit transfer fees via Governance VM
    fn submit_transfer_fees(encoded_vm: [u8]) {
        // Parse the VM
        let vm = parse_vm(encoded_vm);
        
        // Verify the VM is valid before processing it
        let (is_valid, reason) = verify_governance_vm(vm);
        assert(is_valid, reason);
        
        // Obtain the transfer from the VM payload
        let transfer = parse_transfer_fees(vm.payload);
        
        // Verify the VM is for this module
        assert(transfer.module == MODULE, "invalid Module");
        
        // Verify the VM is for this chain
        assert((transfer.chain == get_chain_id() && !is_fork()) || transfer.chain == 0, "invalid Chain");
        
        // Record the governance action as consumed to prevent reentry
        set_governance_action_consumed(vm.hash);
        
        // Obtain the recipient address 
        let recipient = bytes32_to_public_key(transfer.recipient);
        
        // Transfer fees to the recipient
        transfer_to_recipient(recipient, transfer.amount);
    }
    
    // Update chainId and evmChainId on a forked chain via Governance VM
    fn submit_recover_chain_id(encoded_vm: [u8]) {
        // Ensure we are on a fork
        assert(is_fork(), "not a fork");
        
        // Parse the VM
        let vm = parse_vm(encoded_vm);
        
        // Verify the VM is valid before processing it
        let (is_valid, reason) = verify_governance_vm(vm);
        assert(is_valid, reason);
        
        // Parse the recover chain ID payload
        let rci = parse_recover_chain_id(vm.payload);
        
        // Verify the VM is for this module
        assert(rci.module == MODULE, "invalid Module");
        
        // Verify the VM is for this chain
        assert(rci.aztec_chain_id == context::get_current_block().chain_id, "invalid EVM Chain");
        
        // Record the governance action as consumed to prevent reentry
        set_governance_action_consumed(vm.hash);
        
        // Update the chain IDs
        set_aztec_chain_id(rci.aztec_chain_id);
        set_chain_id(rci.new_chain_id);
    }
    
    // Verify a Governance VM is valid
    fn verify_governance_vm(vm: VM) -> (bool, str) {
        // Verify the VM is valid
        let (is_valid, reason) = verify_vm(vm);
        if !is_valid {
            return (false, reason);
        }
        
        // Only current guardian set can sign governance packets
        if vm.guardian_set_index != get_guardian_set_index() {
            return (false, "not signed by current guardian set");
        }
        
        // Verify the VM is from the governance chain
        if vm.emitter_chain_id != get_governance_chain_id() {
            return (false, "wrong governance chain");
        }
        
        // Verify the emitter contract is the governance contract
        if vm.emitter_address != get_governance_contract() {
            return (false, "wrong governance contract");
        }
        
        // Verify this governance action hasn't already been consumed
        if is_governance_action_consumed(vm.hash) {
            return (false, "governance action already consumed");
        }
        
        // Governance VM is valid
        (true, "")
    }
    
    // Upgrade implementation
    fn upgrade_implementation(new_implementation: PublicKey) -> PublicKey {
        // Get current implementation
        let current_implementation = upgrade::get_implementation();
        
        // Upgrade to the new implementation
        upgrade::upgrade_to(new_implementation);
        
        // Initialize the new implementation
        let success = initialize_implementation(new_implementation);
        assert(success, "initialization failed");
        
        // Return the old implementation
        current_implementation
    }
    
    // Helper functions
    
    // Initialize a new implementation
    fn initialize_implementation(implementation: PublicKey) -> bool {
        // Create function selector for "initialize"
        let initialize_selector = ContractFunction::new("initialize");

        // Perform delegate call to the implementation
        let result = proxy::delegate_call(
            implementation,
            initialize_selector,
            [] // Empty arguments array
        );

        // Check if the call was successful
        match result {
            Ok(_) => true,
            Err(_) => false
        }
    }
    
    // Transfer funds to a recipient
    fn transfer_to_recipient(recipient: PublicKey, amount: Field) {
        // Get the token address for the native token or specific token
        let token_address = context::get_native_token();
        
        // Create a public transfer note
        let transfer_note = transfer::create_public_transfer_note(
            token_address,
            context::contract_address(), // from (current contract)
            recipient,                   // to
            amount                       // amount
        );
        
        // Execute the transfer
        transfer::public_transfer(transfer_note);
    }
    
    // Convert bytes32 to PublicKey
    fn bytes32_to_public_key(bytes: [u8; 32]) -> PublicKey {
        // Check if the bytes represent a valid point on the curve
        // The first byte is often used as a format indicator
        
        // For a compressed public key representation (common in Aztec)
        if bytes[0] == 0x02 || bytes[0] == 0x03 {
            // The bytes are already in compressed format
            return PublicKey::from_compressed_bytes(bytes);
        } 
        
        // For a bytes32 that represents an Ethereum address or other format
        // We may need to derive a point on the curve
        
        // 1. Check if it's an Ethereum address format (last 20 bytes)
        let eth_address = bytes_conversion::extract_eth_address(bytes);
        
        // 2. Derive a public key from the address (this is a one-way process, 
        // so we're creating a deterministic key from the address)
        let public_key = PublicKey::derive_from_eth_address(eth_address);
        
        // Return the derived public key
        public_key
    }
    
    // Emit contract upgraded event
    fn emit_contract_upgraded(old_contract: PublicKey, new_contract: PublicKey) {
        let event = ContractUpgradedEvent {
            old_contract,
            new_contract
        };
        context::emit_event(event);
    }
    
    // Emit guardian set added event
    fn emit_guardian_set_added(index: Field) {
        let event = GuardianSetAddedEvent {
            index
        };
        context::emit_event(event);
    }
}