mod main {
    use dep::std;
    use dep::aztec::{context, upgrade, proxy};
    use dep::aztec::contract::proxy;
    
    use crate::setup::setup;
    
    // Constructor - sets up the Wormhole contract
    fn init(
        setup_contract: PublicKey,
        impl_contract: PublicKey,
        initial_guardians: [PublicKey],
        chain_id: Field,
        governance_chain_id: Field,
        governance_contract: [u8; 32],
        aztec_chain_id: Field
    ) {
        // Create a proxy to the setup contract
        let proxy = proxy::create(setup_contract);
        
        // Call setup function with initialization data
        proxy.call_method(
            "setup",
            [
                impl_contract, 
                initial_guardians, 
                chain_id, 
                governance_chain_id, 
                governance_contract, 
                aztec_chain_id
            ]
        );
    }
    
    // Main dispatch function to delegate calls to the implementation contract
    #[aztec(dispatch)]
    fn dispatch() {
        // Get the current implementation address
        let implementation = proxy::get_implementation();
        
        // Ensure we have a valid implementation set
        assert(implementation != PublicKey::zero(), "No implementation contract set");
        
        // Get the call data to forward
        let calldata = context::get_calldata();
        
        // Get the value being sent with the call
        let value = context::msg_value();
        
        // Delegate the call to the implementation contract with the same calldata and value
        let result = proxy::delegate_call(
            implementation,
            calldata,
            value
        );
        
        // If the delegated call was successful, forward the return data
        match result {
            Ok(return_data) => {
                // Return the data from the implementation
                context::return_data(return_data);
            },
            Err(error) => {
                // Forward the error from the implementation
                context::revert_with_error(error);
            }
        }
    }
}