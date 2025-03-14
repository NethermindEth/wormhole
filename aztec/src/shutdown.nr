mod shutdown {
    use dep::std;
    use dep::aztec::{context, upgrade};
    use dep::aztec::contract::call_handler;
    use dep::aztec::contract::payment_handler;
    
    use crate::governance;

    use crate::setters::{
        set_initialized
    };
    
    /**
     * @title  Shutdown
     * @notice This module implements a stripped-down version of the Wormhole core
     *         messaging protocol that is a drop-in replacement for Wormhole's
     *         implementation, effectively disabling all non-governance
     *         functionality.
     *         In particular, outgoing messages are disabled, but the contract
     *         remains upgradeable through governance.
     */
    
    // Initialize function called after an upgrade
    fn initialize() {
        // Get current implementation
        let implementation = upgrade::get_implementation();
        
        // Mark as initialized
        set_initialized(implementation);
        
        // This function intentionally does not have an 'initializer' modifier,
        // to allow this contract to be upgraded to multiple times.
        // NOTE: leave this function empty!
    }
    
    // All other functions from the implementation are intentionally omitted
    // This effectively disables all non-governance functionality
    
    // Fallback function to reject all unexpected calls in shutdown state
    #[aztec(fallback)]
    fn fallback() {
        // Get information about the attempted call
        let caller = context::msg_sender();
        let calldata = context::get_calldata();
        let selector = calldata.len() >= 4 ? calldata[0..4] : [];
        
        // Log the blocked call attempt
        call_handler::log_shutdown_call(caller, selector);
        
        // Revert with a clear message indicating the contract is in shutdown state
        context::revert("Shutdown: This contract has been disabled and only supports governance operations");
    }
    
    // Receive function to reject all direct value transfers in shutdown state
    #[aztec(receive)]
    fn receive() {
        // Get information about the attempted transfer
        let sender = context::msg_sender();
        let amount = context::msg_value();
        let token = context::msg_token();
        
        // Log the rejected transfer with shutdown context
        payment_handler::log_shutdown_payment(sender, token, amount);
        
        // Revert with a clear message explaining both the shutdown state and no-asset policy
        context::revert("Shutdown: The Wormhole contract is disabled and does not accept direct asset transfers");
    }
}