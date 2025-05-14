use dep::aztec::macros::aztec;
mod structs;

#[aztec]
pub contract Wormhole {
    use crate::structs::{Provider, GuardianSet, Guardian, WormholeStorage};

    use dep::aztec::{
        macros::{
            functions::{initializer, public},
            storage::storage,
        },
        prelude::{Map, PublicMutable, AztecAddress},
        protocol_types::traits:: {Serialize, ToField}
    };

    // use dep::token::Token; 
    // token = { git = "https://github.com/Cheetah0x/aztec-standards-residency", tag = "dev", directory = "src/token_contract"}
    // token = { git = "https://github.com/AztecProtocol/aztec-packages/", tag = "v0.84.0" , directory="noir-projects/noir-contracts/contracts/token_contract" }

    #[storage]
    struct Storage<Context> {
        state: PublicMutable<WormholeStorage, Context>,
        // guardian_sets: Map<u64, PublicMutable<GuardianSet, Context>, Context>,
        sequences: Map<AztecAddress, PublicMutable<u64, Context>, Context>,
        wormhole_address: PublicMutable<AztecAddress, Context>,
        token_address: PublicMutable<AztecAddress, Context>,
        guardian_1: Map<u32, PublicMutable<Guardian, Context>, Context>, // FIXED 13 guardians
        guardian_2: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_3: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_4: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_5: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_6: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_7: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_8: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_9: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_10: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_11: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_12: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_13: Map<u32, PublicMutable<Guardian, Context>, Context>,
        current_guardian_set_index: PublicMutable<u32, Context>,
    }

    #[public]
    #[initializer]
    fn init(
        chain_id: u16,
        // governance_chain_id: u16,
        // governance_contract: Field,
        evm_chain_id: u16,
        // initial_guardians: [[u8;20]; 19],
        wormhole_address: AztecAddress, // QUESTION FOR OSSIE: WHO IS THIS? WHERE DO WE GET THE DEPOSIT ADDRESS? WHAT TOKEN ARE WE SENDING?
        token_address: AztecAddress, // TODO: DEPLOY THIS BEFORE DEPLOYING WORMHOLE (MUST EXIST ALREADY)
    ) {
        let provider: Provider = Provider {
            chain_id,
            // governance_chain_id,
            // governance_contract,
            evm_chain_id,
        };
        // Initialize the wormhole state storage
        storage.state.write(WormholeStorage::init(provider));

        // Define the addresses for the wormhole account and token contract
        storage.wormhole_address.write(wormhole_address);
        storage.token_address.write(token_address);
        storage.current_guardian_set_index.write(0 as u32);
    }

    #[public]
    fn set_guardian1(
        guardian: Guardian,
        index: u32,
    ){ 
        storage.guardian_1.at(index).write(guardian);
    }

    #[public]
    fn set_guardian2(
        guardian: Guardian,
        index: u32,
    ){ 
        storage.guardian_2.at(index).write(guardian);
    }

    #[public]
    fn set_guardian3(
        guardian: Guardian,
        index: u32,
    ){ 
        storage.guardian_3.at(index).write(guardian);
    }

    #[public]
    fn set_guardian4(
        guardian: Guardian,
        index: u32,
    ){ 
        storage.guardian_4.at(index).write(guardian);
    }

    #[public]
    fn set_guardian5(
        guardian: Guardian,
        index: u32,
    ){ 
        storage.guardian_5.at(index).write(guardian);
    }

    #[public]
    fn set_guardian6(
        guardian: Guardian,
        index: u32,
    ){ 
        storage.guardian_6.at(index).write(guardian);
    }

    #[public]
    fn set_guardian7(
        guardian: Guardian,
        index: u32,
    ){ 
        storage.guardian_7.at(index).write(guardian);
    }

    #[public]
    fn set_guardian8(
        guardian: Guardian,
        index: u32,
    ){ 
        storage.guardian_8.at(index).write(guardian);
    }

    #[public]
    fn set_guardian9(
        guardian: Guardian,
        index: u32,
    ){ 
        storage.guardian_9.at(index).write(guardian);
    }

    #[public]
    fn set_guardian10(
        guardian: Guardian,
        index: u32,
    ){ 
        storage.guardian_10.at(index).write(guardian);
    }

    #[public]
    fn set_guardian11(
        guardian: Guardian,
        index: u32,
    ){ 
        storage.guardian_11.at(index).write(guardian);
    }

    #[public]
    fn set_guardian12(
        guardian: Guardian,
        index: u32,
    ){ 
        storage.guardian_12.at(index).write(guardian);
    }

    #[public]
    fn set_guardian13(
        guardian: Guardian,
        index: u32,
    ){ 
        storage.guardian_13.at(index).write(guardian);
    }

    // #[public]
    // fn set_guardians(
    //     initial_guardians: [[u8;20]; 13],
    // ){ 
    //     // Initialize the guardian sets
    //     let mut new_guardians: [Guardian; 13] = [Guardian::default(); 13];
        
    //     for i in 0..13 { 
    //         new_guardians[i] = Guardian::new(initial_guardians[i]);
    //     }

    //     storage.current_guardian_set_index.write(0);

    //     storage.guardian_1.at(0).write(new_guardians[0]);
    //     storage.guardian_2.at(0).write(new_guardians[1]);
    //     storage.guardian_3.at(0).write(new_guardians[2]);
    //     storage.guardian_4.at(0).write(new_guardians[3]);
    //     storage.guardian_5.at(0).write(new_guardians[4]);
    //     storage.guardian_6.at(0).write(new_guardians[5]);
    //     storage.guardian_7.at(0).write(new_guardians[6]);
    //     storage.guardian_8.at(0).write(new_guardians[7]);
    //     storage.guardian_9.at(0).write(new_guardians[8]);
    //     storage.guardian_10.at(0).write(new_guardians[9]);
    //     storage.guardian_11.at(0).write(new_guardians[10]);
    //     storage.guardian_12.at(0).write(new_guardians[11]);
    //     storage.guardian_13.at(0).write(new_guardians[12]);
    // }

    #[public]
    fn publish_message(
        nonce: u64,
        payloads: [[u8; 24]; 8], // size of payload needs investigation, looks like max size is Field's size unless we send multiple logs.
        message_fee: u128,
    ) -> u64 {
        // check fee
        assert(storage.state.read().message_fee <= message_fee, "insufficient fee");

        // increase current sequence
        let sequence = storage.sequences.at(context.msg_sender()).read();
        storage.sequences.at(context.msg_sender()).write(sequence + 1);

        // prepare message
        let msg = [
            context.msg_sender().to_field(),
            sequence as Field,
            nonce as Field,
            2 as Field, 
            context.timestamp() as Field,
            Field::from_be_bytes(payloads[0]),
            Field::from_be_bytes(payloads[1]),
            Field::from_be_bytes(payloads[2]),
            Field::from_be_bytes(payloads[3]),
            Field::from_be_bytes(payloads[4]),
            Field::from_be_bytes(payloads[5]),
            Field::from_be_bytes(payloads[6]),
            Field::from_be_bytes(payloads[7]),
        ];

        // let receiver_address = storage.wormhole_address.read();
        // let token_address = storage.token_address.read();

        // deposit the message fee to wormhole token contract
        // let _ = Token::at(token_address).transfer_public_to_public(
        //     context.msg_sender(),
        //     receiver_address, 
        //     message_fee,
        //     sequence as Field,
        // ).call(&mut context);

        context.emit_public_log(msg);

        sequence
    }

    // #[public]
    // fn set_provider(provider: Provider) {
    //     WormholeStorage::set_provider(storage.state.read(),provider);
    // }

    // #[public]
    // fn get_provider() -> Provider {
    //     WormholeStorage::get_provider(storage.state.read())
    // }

    // #[public]
    // fn add_guardian_set(gs: GuardianSet) -> u64 {
    //     let to = gs.index;
    //     storage.guardian_sets.at(to).write(gs);
    //     // returns storage slot for key
    //     to
    // }

    // #[public]
    // fn get_guardian_set(index: u64) -> GuardianSet {
    //     storage.guardian_sets.at(index).read()
    // }

    // #[public]
    // fn expire_guardian_set(index: u64) {       
    //     // Update the expiration time
    //     let expiration_time = context.timestamp() + 86400;
        
    //     // Write the updated guardian set back to storage
    //     GuardianSet::expire_guardian_set(&mut storage.guardian_sets.at(index).read(), context.timestamp(), expiration_time);
    // }

    // #[public]
    // fn guardian_set_expired(index: u64) -> bool {
    //     let current_idx = storage.state.read().guardian_set_index;
    //     GuardianSet::is_active(storage.guardian_sets.at(index).read(), context.timestamp(), current_idx)
    // }

    // #[public]
    // fn set_message_fee(message_fee: u64) {
    //     storage.state.write(WormholeStorage::set_message_fee(storage.state.read(), message_fee));
    // }

    // #[public]
    // fn get_message_fee() -> u64 {
    //     storage.state.read().message_fee
    // }

    // #[public]
    // fn get_guardian_set_index() -> u64 {
    //     storage.state.read().guardian_set_index
    // }

    // #[public]
    // fn get_chain_id() -> u16 {
    //     storage.state.read().get_chain_id()
    // }

    // TODO: finish getters & setters for state variables

    // #[public]
    // fn add_sequence(to: AztecAddress, sequence: Field) -> Field {
    //     storage.sequences.at(to).write(sequence);
    //     // returns storage slot for key
    //     derive_storage_slot_in_map(storage.sequences.storage_slot, to)
    // }

    // #[public]
    // fn get_sequences(address: AztecAddress) -> Field {
    //     storage.sequences.at(address).read()
    // }

    // #[public]
    // fn set_initialized_implementation(to: Field, ii: bool) -> Field {
    //     storage.initialized_implementations.at(to).write(ii);
    //     // returns storage slot for key
    //     derive_storage_slot_in_map(storage.initialized_implementations.storage_slot, to)
    // }

    // #[public]
    // fn get_initialized_implementations(address: Field) -> bool {
    //     storage.initialized_implementations.at(address).read()
    // }

    // // Publish a message to be attested by the Wormhole network
    // #[public]
    // fn publishMessage(
    //     nonce: u32,
    //     payload: [u8; 24], // size of payload needs investigation, looks like max size is Field's size unless we send multiple logs.
    //     consistencyLevel: u8
    //     ) {
    //     // check fee
    //     assert(context.transaction_fee() == get_message_fee(), "invalid fee");

    //     let sequence = useSequence(context.msg_sender());
        
    //     // Create an array of Field elements
    //     let message = [
    //         context.msg_sender().to_field(),
    //         sequence,  // Already a Field
    //         Field::from(nonce),
    //         Field::from(consistencyLevel),
    //         Field::from_be_bytes(payload),
    //         Field::from_be_bytes(payload),
    //         Field::from_be_bytes(payload),
    //         Field::from_be_bytes(payload),
    //         Field::from_be_bytes(payload),
    //         Field::from_be_bytes(payload),
    //         Field::from_be_bytes(payload),
    //         Field::from_be_bytes(payload),
    //         Field::from_be_bytes(payload)
    //     ];
        
    //     // emit log
    //     context.emit_public_log(message);
    // }

    // #[public]
    // fn useSequence(emitter: AztecAddress) -> Field {
    //     let sequence = get_sequences(emitter);
    //     add_sequence(emitter, sequence + 1);
    //     sequence
    // }

}
