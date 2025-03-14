mod state {
    use dep::std;
    use dep::aztec::note::{note_header, Note, NoteHeader, BALANCE_NOTE_LEN, create_note, create_public_note, store_note, GUARDIAN_SET_NOTE_TYPE, fetch_public_note, GOVERNANCE_CONTRACT_NOTE_TYPE};
    use dep::aztec::note::storage::{StorageSlot, write_public_value};
    use dep::aztec::constants::GOVERNANCE_CONTRACT_STORAGE_SLOT;    
    use dep::aztec::hash::keccak256;
    use dep::aztec::storage::compute_slot;
    use dep::aztec::serialization::serialize;
    use dep::aztec::serialization::{
        Serializer,
        SerializationError,
        Deserializer
    };
    use dep::aztec::crypto::key_management::PublicKey;
    use dep::aztec::state_vars::{Map, set_value, get_value};
    use dep::aztec::hash::pedersen;
    use dep::aztec::storage::storage_slot;
    use dep::aztec::field_conversion::bytes_to_field;

    
    use crate::structs::{Provider, GuardianSet, VM, GovernanceContractUpdatedEvent, GuardianSetEvent};
    
    // Event definitions
    struct LogGuardianSetChanged {
        old_guardian_index: Field,
        new_guardian_index: Field,
    }
    
    struct LogMessagePublished {
        emitter_address: PublicKey,
        nonce: Field,
        payload: [u8],
    }
    
    // WormholeState encapsulates all state for the Wormhole protocol
    // In Aztec, we use a combination of private notes and public state
    struct WormholeState {
        // Provider info (chain ID, governance chain ID, governance contract)
        provider: Provider,
        
        // Guardian sets storage
        // In Aztec, we use a note-based approach for complex data structures
        guardian_sets: Map<Field, GuardianSet>,
        
        // Current active guardian set index
        guardian_set_index: Field,
        
        // Period for which a guardian set stays active after replacement
        guardian_set_expiry: Field,
        
        // Sequence numbers per emitter
        sequences: Map<PublicKey, Field>,
        
        // Mapping of consumed governance actions
        consumed_governance_actions: Map<[u8; 32], bool>,
        
        // Mapping of initialized implementations
        initialized_implementations: Map<PublicKey, bool>,
        
        // Message fee
        message_fee: Field,
        
        // Aztec chain ID (equivalent to EVM chain ID)
        aztec_chain_id: Field,
    }
    
    // Constants for state storage
    const PROVIDER_CHAIN_ID_SLOT: Field = 1;
    const PROVIDER_GOV_CHAIN_ID_SLOT: Field = 2;
    const PROVIDER_GOV_CONTRACT_SLOT: Field = 3;
    const GUARDIAN_SET_INDEX_SLOT: Field = 4;
    const GUARDIAN_SET_EXPIRY_SLOT: Field = 5;
    const MESSAGE_FEE_SLOT: Field = 6;
    const AZTEC_CHAIN_ID_SLOT: Field = 7;
    
    // Note prefixes for different types of data
    const GUARDIAN_SET_PREFIX: Field = 100;
    const SEQUENCE_PREFIX: Field = 200;
    const CONSUMED_GOV_ACTION_PREFIX: Field = 300;
    const INITIALIZED_IMPL_PREFIX: Field = 400;
    
    // Initialize the Wormhole state
    fn initialize_state(
        chain_id: Field,
        governance_chain_id: Field,
        governance_contract: [u8; 32],
        guardian_set: GuardianSet,
        aztec_chain_id: Field
    ) {
        // Set provider information
        set_value(PROVIDER_CHAIN_ID_SLOT, chain_id);
        set_value(PROVIDER_GOV_CHAIN_ID_SLOT, governance_chain_id);
        
        // For the governance contract (bytes32), we need to store it in chunks
        // or use a note-based approach for complex data
        store_governance_contract(governance_contract);
        
        // Set initial guardian set (index 0)
        store_guardian_set(guardian_set, 0);
        
        // Set guardian set index to 0
        set_value(GUARDIAN_SET_INDEX_SLOT, 0);
        
        // Set guardian set expiry (86400 seconds = 1 day)
        set_value(GUARDIAN_SET_EXPIRY_SLOT, 86400);
        
        // Initialize message fee to 0
        set_value(MESSAGE_FEE_SLOT, 0);
        
        // Set Aztec chain ID
        set_value(AZTEC_CHAIN_ID_SLOT, aztec_chain_id);
    }
    
    // Store a governance contract address
    fn store_governance_contract(governance_contract: [u8; 32]) {
        // Define the storage slot for the governance contract
        let slot = StorageSlot::with_position(GOVERNANCE_CONTRACT_STORAGE_SLOT);
        
        // Store the governance contract address as a 32-byte value
        // Aztec typically handles this with specialized storage for public values
        write_public_value(slot, governance_contract);
        
        // Emit an event to notify observers about the governance contract change
        let event = GovernanceContractUpdatedEvent {
            new_governance_contract: governance_contract
        };
        context::emit_event(event);
    }
    
    // Create a note to store governance contract
    fn create_governance_contract_note(governance_contract: [u8; 32]) -> Note {
        // Create a unique note identifier based on the storage purpose
        let note_type_id = 0x01; // Identifier for governance contract notes
        
        // Create a commitment to the governance contract
        // This includes the governance contract along with metadata
        let commitment_data = [
            note_type_id,             // Type identifier (1 byte)
            governance_contract,      // Governance contract (32 bytes)
            context::get_timestamp()  // Timestamp for versioning (4 bytes)
        ].concat();
        
        // Create a commitment using a hash function
        let commitment = keccak256(commitment_data);
        
        // Create the note header with the appropriate storage slot
        let header = NoteHeader::new(
            PROVIDER_GOV_CONTRACT_SLOT,
            commitment
        );
        
        // Create the actual note - since this is contract configuration,
        // we use a public note that can be read by anyone
        create_public_note(
            header,
            governance_contract
        )
    }
    
    // Store a guardian set
    fn store_guardian_set(guardian_set: GuardianSet, index: Field) {
        // Validate all guardian keys before storing
        for i in 0..guardian_set.keys.len() {
            // Ensure each key is valid
            assert(is_valid_key(guardian_set.keys[i]), "Invalid guardian key");
        }
        
        // Compute the storage slot for this guardian set
        // Using a namespace approach to avoid collisions
        let slot = compute_slot(GUARDIAN_SET_PREFIX, index);
        
        // Serialize the guardian set data
        // This converts the struct into a byte array for storage
        let serialized_data = serialize(guardian_set);
        
        // Create a note containing the guardian set data
        // Using a public note since guardian sets are public information
        let note = create_public_note(
            NoteType::GUARDIAN_SET,  // Note type identifier
            slot,                    // Storage slot
            serialized_data          // Note data
        );
        
        // Store the note in Aztec's note system
        store_note(note);
        
        // Emit an event for this guardian set update
        let event = GuardianSetEvent {
            index: index,
            num_guardians: guardian_set.keys.len(),
            expiration_time: guardian_set.expiration_time
        };
        context::emit_event(event);
    }
    
    // Create a note for a guardian set
    fn create_guardian_set_note(guardian_set: GuardianSet, index: Field) -> Note {
        // Compute the storage slot for this guardian set
        let slot = GUARDIAN_SET_PREFIX + index;
        
        // Serialize the guardian keys to a compact format
        let mut serialized_keys = Vec::with_capacity(guardian_set.keys.len() * 32);
        for key in guardian_set.keys {
            // Each key is a PublicKey which we serialize to bytes
            let key_bytes = key.to_bytes();
            serialized_keys.extend(key_bytes);
        }
        
        // Serialize the entire guardian set structure
        let serialized_data = serialize_struct(&[
            // Number of guardians (4 bytes)
            Hash::new().update_u32(guardian_set.keys.len() as u32).finalize(),
            // Serialized guardian keys (variable length)
            serialized_keys,
            // Expiration time (8 bytes)
            Hash::new().update_field(guardian_set.expiration_time).finalize()
        ]);
        
        // Create the note header with appropriate metadata
        let header = NoteHeader::new(
            slot,                        // Storage slot
            GUARDIAN_SET_NOTE_TYPE,      // Type identifier
            serialized_data.hash()       // Commitment to the data
        );
        
        // Create the actual note with the serialized data
        create_public_note(header, serialized_data)
    }
    
    // Serialize a guardian set to bytes
    fn serialize_guardian_set(guardian_set: GuardianSet) -> [u8] {
        // Create a new serializer with estimated capacity
        let estimated_size = 4 + (guardian_set.keys.len() * 32) + 8;
        let mut serializer = Serializer::with_capacity(estimated_size);
        
        // Serialize the number of guardian keys (u32)
        serializer.write_u32(guardian_set.keys.len() as u32);
        
        // Serialize each guardian key
        for key in guardian_set.keys {
            // Convert the public key to a standard format
            let key_bytes = key.to_bytes();
            
            // Write the key bytes to the serializer
            serializer.write_bytes(&key_bytes);
        }
        
        // Serialize the expiration time (Field)
        serializer.write_field(guardian_set.expiration_time);
        
        // Get the serialized data as a byte array
        serializer.finalize()
    }
    
    // Get a guardian set
    fn get_guardian_set(index: Field) -> GuardianSet {
        // Compute the storage slot for this guardian set
        let slot = GUARDIAN_SET_PREFIX + index;
        
        // Fetch the note containing the guardian set data
        let note_opt = fetch_public_note(slot, GUARDIAN_SET_NOTE_TYPE);
        
        // Check if the note exists
        assert(note_opt.is_some(), "Guardian set not found");
        let note = note_opt.unwrap();
        
        // Get the serialized content from the note
        let content = note.get_content();
        
        // Deserialize the guardian set
        deserialize_guardian_set(content)
    }
    
    // Deserialize a guardian set from bytes
    fn deserialize_guardian_set(data: [u8]) -> GuardianSet {
        // Create a deserializer for the data
        let mut deserializer = Deserializer::new(&data);
        
        // Read the number of guardian keys
        let num_keys = deserializer.read_u32();
        
        // Read each guardian key
        let mut keys = Vec::with_capacity(num_keys as usize);
        for _ in 0..num_keys {
            // Read the key bytes
            let key_bytes = deserializer.read_bytes(32);
            
            // Convert bytes to PublicKey
            let key = PublicKey::from_bytes(&key_bytes);
            
            // Add to the list of keys
            keys.push(key);
        }
        
        // Read the expiration time
        let expiration_time = deserializer.read_field();
        
        // Verify we've consumed all data
        assert(deserializer.is_finished(), "Corrupted guardian set data");
        
        // Return the reconstructed guardian set
        GuardianSet {
            keys,
            expiration_time
        }
    }
    
    // Set a sequence number for an emitter
    fn set_sequence(emitter: PublicKey, sequence: Field) {
        let slot = compute_sequence_slot(emitter);
        set_value(slot, sequence);
    }
    
    // Get a sequence number for an emitter
    fn get_sequence(emitter: PublicKey) -> Field {
        let slot = compute_sequence_slot(emitter);
        get_value(slot)
    }
    
    // Compute slot for sequence storage
    fn compute_sequence_slot(emitter: PublicKey) -> Field {
        // Convert the public key to bytes
        let emitter_bytes = emitter.to_bytes();
        
        // Create a domain separator for sequences to prevent collisions
        let domain = b"WORMHOLE_SEQUENCE";
        
        // Compute the storage slot using Aztec's storage slot utility
        // This uses Pedersen hash for efficiency in the circuit
        storage_slot::compute(
            SEQUENCE_PREFIX,  // Storage namespace/prefix
            domain,           // Domain separator
            emitter_bytes     // Unique identifier (emitter)
        )
    }
    
    // Mark a governance action as consumed
    fn set_governance_action_consumed(hash: [u8; 32]) {
        let slot = compute_governance_action_slot(hash);
        set_value(slot, 1); // 1 represents true
    }
    
    // Check if a governance action is consumed
    fn is_governance_action_consumed(hash: [u8; 32]) -> bool {
        let slot = compute_governance_action_slot(hash);
        get_value(slot) == 1
    }
    
    // Compute slot for governance action storage
    fn compute_governance_action_slot(hash: [u8; 32]) -> Field {
        // Create a domain separator for governance actions to prevent collisions
        let domain = b"WORMHOLE_GOV_ACTION";
        
        // Compute the storage slot using Aztec's storage slot utility
        // This combines the prefix, domain, and hash to create a unique slot
        storage_slot::compute(
            CONSUMED_GOV_ACTION_PREFIX,  // Storage namespace/prefix
            domain,                      // Domain separator
            hash                         // Action hash as unique identifier
        )
    }
    
    // Convert a 32-byte hash to a Field element
    fn hash_to_field(hash: [u8; 32]) -> Field {
        // For BN254 curve (which Aztec uses), we need to ensure the value fits within the field size
        // A Field element must be less than the BN254 curve order
        
        bytes_to_field(hash)
    }

    // Set implementation as initialized
    fn set_initialized(implementation: PublicKey) {
        let slot = compute_initialized_slot(implementation);
        set_value(slot, 1); // 1 represents true
    }
    
    // Check if an implementation is initialized
    fn is_initialized(implementation: PublicKey) -> bool {
        let slot = compute_initialized_slot(implementation);
        get_value(slot) == 1
    }
    
    // Compute slot for initialized implementation storage
    fn compute_initialized_slot(implementation: PublicKey) -> Field {
        // Convert the public key to bytes
        let implementation_bytes = implementation.to_bytes();
        
        // Create a domain separator for initialized implementations to prevent collisions
        let domain = b"WORMHOLE_INITIALIZED_IMPL";
        
        // Compute the storage slot using Aztec's storage slot utility
        storage_slot::compute(
            INITIALIZED_IMPL_PREFIX,  // Storage namespace/prefix
            domain,                   // Domain separator
            implementation_bytes      // Unique identifier (implementation address)
        )
    }
    
    // Getter functions
    fn get_chain_id() -> Field {
        get_value(PROVIDER_CHAIN_ID_SLOT)
    }
    
    fn get_governance_chain_id() -> Field {
        get_value(PROVIDER_GOV_CHAIN_ID_SLOT)
    }
    
    // Get the governance contract address
    fn get_governance_contract() -> [u8; 32] {
        // Try to fetch the governance contract note
        let note_opt = fetch_public_note(PROVIDER_GOV_CONTRACT_SLOT, GOVERNANCE_CONTRACT_NOTE_TYPE);
        
        // Check if the note exists
        assert(note_opt.is_some(), "Governance contract not set");
        let note = note_opt.unwrap();
        
        // Extract the content from the note (should be exactly 32 bytes)
        let content = note.get_content();
        
        // Ensure the content is the correct size
        assert(content.len() == 32, "Invalid governance contract data");
        
        // Convert the content to a fixed-size array
        let mut result = [0u8; 32];
        for i in 0..32 {
            result[i] = content[i];
        }
        
        // Return the governance contract address as a 32-byte array
        result
    }
    
    fn get_guardian_set_index() -> Field {
        get_value(GUARDIAN_SET_INDEX_SLOT)
    }
    
    fn get_guardian_set_expiry() -> Field {
        get_value(GUARDIAN_SET_EXPIRY_SLOT)
    }
    
    fn get_message_fee() -> Field {
        get_value(MESSAGE_FEE_SLOT)
    }
    
    fn get_aztec_chain_id() -> Field {
        get_value(AZTEC_CHAIN_ID_SLOT)
    }
    
    // Setter functions
    fn set_chain_id(chain_id: Field) {
        set_value(PROVIDER_CHAIN_ID_SLOT, chain_id);
    }
    
    fn set_governance_chain_id(governance_chain_id: Field) {
        set_value(PROVIDER_GOV_CHAIN_ID_SLOT, governance_chain_id);
    }
    
    fn set_guardian_set_index(index: Field) {
        set_value(GUARDIAN_SET_INDEX_SLOT, index);
    }
    
    fn set_message_fee(fee: Field) {
        set_value(MESSAGE_FEE_SLOT, fee);
    }
    
    fn set_aztec_chain_id(aztec_chain_id: Field) {
        set_value(AZTEC_CHAIN_ID_SLOT, aztec_chain_id);
    }
    
    // Update expiration time for a guardian set
    fn expire_guardian_set(index: Field) {
        let guardian_set = get_guardian_set(index);
        let current_time = dep::std::block::timestamp();
        let expiry = current_time + get_guardian_set_expiry();
        
        let updated_set = GuardianSet {
            keys: guardian_set.keys,
            expiration_time: expiry
        };
        
        store_guardian_set(updated_set, index);
    }
}