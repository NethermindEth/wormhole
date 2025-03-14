mod structs {
    // Provider contains chain and governance information
    struct Provider {
        chain_id: Field,              // Current chain ID in Wormhole network
        governance_chain_id: Field,   // Chain ID where governance messages originate
        governance_contract: [u8; 32], // Governance contract address (32 bytes)
    }

    // GuardianSet represents a set of validators
    struct GuardianSet {
        keys: [PublicKey],           // Array of guardian public keys
        expiration_time: Field,      // Timestamp when this set expires after being replaced
    }

    struct GuardianSetEvent {
        index: Field,
        num_guardians: Field,
        expiration_time: Field
    }

    struct UpgradeEvent {
        proxy: PublicKey,
        implementation: PublicKey
    }
        
    // Define the event for governance contract updates
    struct GovernanceContractUpdatedEvent {
        new_governance_contract: [u8; 32]
    }

    // Signature contains components of a guardian signature
    struct Signature {
        r: [u8; 32],                 // r component of signature
        s: [u8; 32],                 // s component of signature 
        v: u8,                       // v component of signature (recovery id + 27)
        guardian_index: u8,          // Index of the guardian in the set
    }

    // VM (Verified Message) represents a cross-chain message
    struct VM {
        // Header
        version: u8,                 // Message version

        // Verification fields
        guardian_set_index: Field,   // Index of the guardian set that signed
        signatures: [Signature],     // Array of signatures from guardians
        
        // Body that is hashed and signed
        timestamp: Field,            // Timestamp when message was created
        nonce: Field,                // Nonce for the message
        emitter_chain_id: Field,     // Chain ID of the emitter
        emitter_address: [u8; 32],   // Address of the emitter contract (32 bytes)
        sequence: Field,             // Sequence number from the emitter
        consistency_level: u8,       // Finality level (0 for instant)
        payload: [u8],               // Arbitrary message payload
        
        // Hash of the body, computed or verified
        hash: [u8; 32],              // Hash of the body
    }
    
}