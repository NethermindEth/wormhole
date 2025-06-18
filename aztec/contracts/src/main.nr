use dep::aztec::macros::aztec;
mod structs;

/**
To deploy on testnet, use the following commands (following the tutorial):
... (your deployment instructions)
*/

#[aztec]
pub contract Wormhole {
    use crate::structs::{Provider, Guardian, WormholeStorage, VAA, Signature, Body};
    use dep::keccak256::keccak256;

    use dep::aztec::{
        macros::{
            functions::{initializer, public, private, internal},
            storage::storage,
        },
        prelude::{Map, PublicMutable, AztecAddress},
        protocol_types::traits::ToField
    };

    use dep::token::Token; 

    #[storage]
    struct Storage<Context> {
        state: PublicMutable<WormholeStorage, Context>,
        sequences: Map<AztecAddress, PublicMutable<u64, Context>, Context>,
        wormhole_address: PublicMutable<AztecAddress, Context>,
        token_address: PublicMutable<AztecAddress, Context>,
        guardian_1: Map<u32, PublicMutable<Guardian, Context>, Context>,
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
        guardian_14: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_15: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_16: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_17: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_18: Map<u32, PublicMutable<Guardian, Context>, Context>,
        guardian_19: Map<u32, PublicMutable<Guardian, Context>, Context>,
        current_guardian_set_index: Map<u32, PublicMutable<u64, Context>, Context>,
    }

    #[public]
    #[initializer]
    fn init(
        chain_id: u16,
        evm_chain_id: u16,
        wormhole_address: AztecAddress, 
        token_address: AztecAddress, 
    ) {
        let provider: Provider = Provider {
            chain_id,
            evm_chain_id,
        };
        storage.state.write(WormholeStorage::init(provider));
        storage.wormhole_address.write(wormhole_address);
        storage.token_address.write(token_address);
        storage.current_guardian_set_index.at(0).write(18_446_744_073_709_551_615);
    }

    #[public]
    fn publish_message_in_public(
        nonce: u64,
        payloads: [[u8; 31]; 8], 
        message_fee: u128,
        consistency: u8,
        from: AztecAddress,
        token_nonce: Field,
    ) -> u64 {
        assert(storage.state.read().message_fee <= message_fee, "insufficient fee");

        let receiver_address = storage.wormhole_address.read();
        let token_address = storage.token_address.read();

        let _ = Token::at(token_address).transfer_in_public(
            from,
            receiver_address, 
            message_fee,
            token_nonce,  
        ).call(&mut context);

        let sequence = storage.sequences.at(context.msg_sender()).read();
        storage.sequences.at(context.msg_sender()).write(sequence + 1);

        let msg: [Field; 13] = [
            context.msg_sender().to_field(), 
            sequence as Field,  
            nonce as Field,
            consistency as Field, 
            context.timestamp() as Field,
            Field::from_le_bytes(payloads[0]),
            Field::from_le_bytes(payloads[1]),
            Field::from_le_bytes(payloads[2]),
            Field::from_le_bytes(payloads[3]),
            Field::from_le_bytes(payloads[4]),
            Field::from_le_bytes(payloads[5]),
            Field::from_le_bytes(payloads[6]),
            Field::from_le_bytes(payloads[7]),
        ];

        context.emit_public_log(msg);
        sequence
    }

    #[private]
    fn publish_message_in_private(
        nonce: u64,
        payloads: [[u8; 31]; 8], 
        message_fee: u128,
        consistency: u8,
        from: AztecAddress, 
        token_nonce: Field,
    ) {
        assert(1 <= message_fee, "insufficient fee");

        let receiver_address = AztecAddress {
            inner: 0x25c6603099990f1085903124acaf00217a6e29e50baf163036c566a83c9bb6e3
        };
        let token_address = AztecAddress {
            inner: 0x0dc025163fe73041b970e9a26905fb41358ad14ef8de84e38746679f210d300e
        };

        let _ = Token::at(token_address).transfer_in_private(
            from,
            receiver_address, 
            message_fee,
            token_nonce,  
        ).call(&mut context);

        Wormhole::at(context.this_address())._publish_message(
            nonce, 
            payloads, 
            consistency
        ).enqueue(&mut context);
    }

    #[public]
    #[internal]
    fn _publish_message(
        nonce: u64,
        payloads: [[u8; 31]; 8], 
        consistency: u8,
    ) -> u64 {
        let sequence = storage.sequences.at(context.msg_sender()).read();
        storage.sequences.at(context.msg_sender()).write(sequence + 1);

        let msg: [Field; 13] = [
            context.msg_sender().to_field(), 
            sequence as Field,  
            nonce as Field,
            consistency as Field, 
            context.timestamp() as Field,
            Field::from_le_bytes(payloads[0]),
            Field::from_le_bytes(payloads[1]),
            Field::from_le_bytes(payloads[2]),
            Field::from_le_bytes(payloads[3]),
            Field::from_le_bytes(payloads[4]),
            Field::from_le_bytes(payloads[5]),
            Field::from_le_bytes(payloads[6]),
            Field::from_le_bytes(payloads[7]),
        ];

        context.emit_public_log(msg);
        sequence
    }

    // Guardian setters (your existing code)
    #[public]
    fn set_guardian1(guardian: Guardian, index: u32) { 
        storage.guardian_1.at(index).write(guardian);
    }

    #[public]
    fn set_guardian2(guardian: Guardian, index: u32) { 
        storage.guardian_2.at(index).write(guardian);
    }

    #[public]
    fn set_guardian3(guardian: Guardian, index: u32) { 
        storage.guardian_3.at(index).write(guardian);
    }

    #[public]
    fn set_guardian4(guardian: Guardian, index: u32) { 
        storage.guardian_4.at(index).write(guardian);
    }

    #[public]
    fn set_guardian5(guardian: Guardian, index: u32) { 
        storage.guardian_5.at(index).write(guardian);
    }

    #[public]
    fn set_guardian6(guardian: Guardian, index: u32) { 
        storage.guardian_6.at(index).write(guardian);
    }

    #[public]
    fn set_guardian7(guardian: Guardian, index: u32) { 
        storage.guardian_7.at(index).write(guardian);
    }

    #[public]
    fn set_guardian8(guardian: Guardian, index: u32) { 
        storage.guardian_8.at(index).write(guardian);
    }

    #[public]
    fn set_guardian9(guardian: Guardian, index: u32) { 
        storage.guardian_9.at(index).write(guardian);
    }

    #[public]
    fn set_guardian10(guardian: Guardian, index: u32) { 
        storage.guardian_10.at(index).write(guardian);
    }

    #[public]
    fn set_guardian11(guardian: Guardian, index: u32) { 
        storage.guardian_11.at(index).write(guardian);
    }

    #[public]
    fn set_guardian12(guardian: Guardian, index: u32) { 
        storage.guardian_12.at(index).write(guardian);
    }

    #[public]
    fn set_guardian13(guardian: Guardian, index: u32) { 
        storage.guardian_13.at(index).write(guardian);
    }

    #[public]
    fn set_guardian14(guardian: Guardian, index: u32) { 
        storage.guardian_14.at(index).write(guardian);
    }

    #[public]
    fn set_guardian15(guardian: Guardian, index: u32) { 
        storage.guardian_15.at(index).write(guardian);
    }

    #[public]
    fn set_guardian16(guardian: Guardian, index: u32) { 
        storage.guardian_16.at(index).write(guardian);
    }

    #[public]
    fn set_guardian17(guardian: Guardian, index: u32) { 
        storage.guardian_17.at(index).write(guardian);
    }

    #[public]
    fn set_guardian18(guardian: Guardian, index: u32) { 
        storage.guardian_18.at(index).write(guardian);
    }

    #[public]
    fn set_guardian19(guardian: Guardian, index: u32) { 
        storage.guardian_19.at(index).write(guardian);
    }

    #[public]
    fn expire_guardian_set(index: u32) {
        storage.current_guardian_set_index.at(index).write(context.timestamp() + 86400);
    }

    #[public]
    fn guardian_set_expired(index: u32) -> bool {
        let timestamp = storage.current_guardian_set_index.at(index).read();
        context.timestamp() > timestamp
    }

    #[public]
    fn set_provider(provider: Provider) {
        let updated_storage = WormholeStorage::set_provider(storage.state.read(), provider);
        storage.state.write(updated_storage);
    }

    #[public]
    fn get_provider() -> Provider {
        WormholeStorage::get_provider(storage.state.read())
    }

    // Helper function to get guardian by index
    #[public]
    fn get_guardian_by_index(guardian_idx: u8, guardian_set_index: u32) -> Guardian {
        if guardian_idx == 0 { 
            storage.guardian_1.at(guardian_set_index).read()
        } else if guardian_idx == 1 { 
            storage.guardian_2.at(guardian_set_index).read()
        } else if guardian_idx == 2 { 
            storage.guardian_3.at(guardian_set_index).read()
        } else if guardian_idx == 3 { 
            storage.guardian_4.at(guardian_set_index).read()
        } else if guardian_idx == 4 { 
            storage.guardian_5.at(guardian_set_index).read()
        } else if guardian_idx == 5 { 
            storage.guardian_6.at(guardian_set_index).read()
        } else if guardian_idx == 6 { 
            storage.guardian_7.at(guardian_set_index).read()
        } else if guardian_idx == 7 { 
            storage.guardian_8.at(guardian_set_index).read()
        } else if guardian_idx == 8 { 
            storage.guardian_9.at(guardian_set_index).read()
        } else if guardian_idx == 9 { 
            storage.guardian_10.at(guardian_set_index).read()
        } else if guardian_idx == 10 { 
            storage.guardian_11.at(guardian_set_index).read()
        } else if guardian_idx == 11 { 
            storage.guardian_12.at(guardian_set_index).read()
        } else if guardian_idx == 12 { 
            storage.guardian_13.at(guardian_set_index).read()
        } else if guardian_idx == 13 { 
            storage.guardian_14.at(guardian_set_index).read()
        } else if guardian_idx == 14 { 
            storage.guardian_15.at(guardian_set_index).read()
        } else if guardian_idx == 15 { 
            storage.guardian_16.at(guardian_set_index).read()
        } else if guardian_idx == 16 { 
            storage.guardian_17.at(guardian_set_index).read()
        } else if guardian_idx == 17 { 
            storage.guardian_18.at(guardian_set_index).read()
        } else if guardian_idx == 18 { 
            storage.guardian_19.at(guardian_set_index).read()
        } else {
            Guardian::default()
        }
    }

    // VAA verification function
    #[public]
    fn verify_vaa(bytes: [u8; 1918]) -> bool {
        // Parse VAA components
        let version = bytes[0];
        assert(version == 1, "Unsupported version");
        
        let guardian_set_index: u32 = crate::structs::u32_from_u8s_le([bytes[1], bytes[2], bytes[3], bytes[4]]);
        let signatures_len = bytes[5];
        assert(signatures_len <= 13, "Too many signatures");

        let mut signatures: [Signature; 13] = [Signature::default(); 13];
        
        // Parse signatures
        for i in 0..signatures_len {
            if i < 13 {
                let sig_start: u32 = 6 + (i as u32 * 66);
                let mut sig_bytes = [0; 66];
                for j in 0..66 {
                    let byte_index: u32 = sig_start + j as u32;
                    if byte_index < 1918 {
                        sig_bytes[j] = bytes[byte_index];
                    }
                }
                signatures[i as u32] = Signature::from_bytes(sig_bytes);
            }
        }

        // Calculate body start and extract body
        let body_start: u32 = 6 + (signatures_len as u32 * 66);
        let mut body_bytes = [0; 1060];
        for i in 0..1060 {
            let byte_index: u32 = body_start + i as u32;
            if byte_index < 1918 {
                body_bytes[i] = bytes[byte_index];
            }
        }

        // Calculate hash: keccak256(keccak256(body))
        let hash1: [u8; 32] = keccak256(body_bytes, 1060);
        let hash: [u8; 32] = keccak256(hash1, 32);

        // Verify signatures
        let mut last_idx: u8 = 0;
        let mut verification_result = true;
        let mut valid_signatures = 0;

        for i in 0..13 {
            let sig = signatures[i];
            
            // Skip empty signatures
            let is_empty_sig = (i > 0) & (sig.guardian_index == 0);
            
            if !is_empty_sig {
                let guardian_idx = sig.guardian_index;

                // Check ascending order
                if guardian_idx <= last_idx {
                    verification_result = false;
                }
                last_idx = guardian_idx;

                // Check bounds
                if guardian_idx >= 19 {
                    verification_result = false;
                }

                if guardian_idx < 19 {
                    // Get guardian using contract storage - direct call instead of self
                    let guardian = if guardian_idx == 0 { 
                        storage.guardian_1.at(guardian_set_index).read()
                    } else if guardian_idx == 1 { 
                        storage.guardian_2.at(guardian_set_index).read()
                    } else if guardian_idx == 2 { 
                        storage.guardian_3.at(guardian_set_index).read()
                    } else if guardian_idx == 3 { 
                        storage.guardian_4.at(guardian_set_index).read()
                    } else if guardian_idx == 4 { 
                        storage.guardian_5.at(guardian_set_index).read()
                    } else if guardian_idx == 5 { 
                        storage.guardian_6.at(guardian_set_index).read()
                    } else if guardian_idx == 6 { 
                        storage.guardian_7.at(guardian_set_index).read()
                    } else if guardian_idx == 7 { 
                        storage.guardian_8.at(guardian_set_index).read()
                    } else if guardian_idx == 8 { 
                        storage.guardian_9.at(guardian_set_index).read()
                    } else if guardian_idx == 9 { 
                        storage.guardian_10.at(guardian_set_index).read()
                    } else if guardian_idx == 10 { 
                        storage.guardian_11.at(guardian_set_index).read()
                    } else if guardian_idx == 11 { 
                        storage.guardian_12.at(guardian_set_index).read()
                    } else if guardian_idx == 12 { 
                        storage.guardian_13.at(guardian_set_index).read()
                    } else if guardian_idx == 13 { 
                        storage.guardian_14.at(guardian_set_index).read()
                    } else if guardian_idx == 14 { 
                        storage.guardian_15.at(guardian_set_index).read()
                    } else if guardian_idx == 15 { 
                        storage.guardian_16.at(guardian_set_index).read()
                    } else if guardian_idx == 16 { 
                        storage.guardian_17.at(guardian_set_index).read()
                    } else if guardian_idx == 17 { 
                        storage.guardian_18.at(guardian_set_index).read()
                    } else if guardian_idx == 18 { 
                        storage.guardian_19.at(guardian_set_index).read()
                    } else {
                        Guardian::default()
                    };
                    
                    let pub_key_x = guardian.get_pub_key_x();
                    let pub_key_y = guardian.get_pub_key_y();

                    // Prepare signature bytes
                    let mut signature_bytes = [0; 64];
                    for j in 0..32 {
                        signature_bytes[j] = sig.r[j];
                        signature_bytes[32 + j] = sig.s[j];
                    }

                    // Verify signature
                    let is_valid = Signature::verify_signature(
                        pub_key_x, 
                        pub_key_y, 
                        signature_bytes, 
                        hash
                    );
                    
                    if is_valid {
                        valid_signatures += 1;
                    } else {
                        verification_result = false;
                    }
                }
            }
        }

        // Need at least 2/3 + 1 of guardians to sign (13 out of 19)
        let required_signatures = ((19 * 2) / 3) + 1;
        if valid_signatures < required_signatures {
            verification_result = false;
        }

        verification_result
    }

    // Parse and verify VAA in one function
    #[public]
    fn parse_and_verify_vaa(bytes: [u8; 1918]) -> VAA {
        // Parse the VAA first
        let version = bytes[0];
        assert(version == 1, "Unsupported version");
        
        let guardian_set_index: u32 = crate::structs::u32_from_u8s_le([bytes[1], bytes[2], bytes[3], bytes[4]]);
        let signatures_len = bytes[5];
        assert(signatures_len <= 13, "Too many signatures");

        let mut signatures: [Signature; 13] = [Signature::default(); 13];
        
        // Parse signatures
        for i in 0..signatures_len {
            if i < 13 {
                let sig_start: u32 = 6 + (i as u32 * 66);
                let mut sig_bytes = [0; 66];
                for j in 0..66 {
                    let byte_index: u32 = sig_start + j as u32;
                    if byte_index < 1918 {
                        sig_bytes[j] = bytes[byte_index];
                    }
                }
                signatures[i as u32] = Signature::from_bytes(sig_bytes);
            }
        }

        // Calculate body start and extract body
        let body_start: u32 = 6 + (signatures_len as u32 * 66);
        let mut body_bytes = [0; 1060];
        for i in 0..1060 {
            let byte_index: u32 = body_start + i as u32;
            if byte_index < 1918 {
                body_bytes[i] = bytes[byte_index];
            }
        }

        // Calculate hash: keccak256(keccak256(body))
        let hash1: [u8; 32] = keccak256(body_bytes, 1060);
        let hash: [u8; 32] = keccak256(hash1, 32);

        // Verify signatures
        let mut last_idx: u8 = 0;
        let mut verification_result = true;
        let mut valid_signatures = 0;

        for i in 0..13 {
            let sig = signatures[i];
            
            // Skip empty signatures
            let is_empty_sig = (i > 0) & (sig.guardian_index == 0);
            
            if !is_empty_sig {
                let guardian_idx = sig.guardian_index;

                // Check ascending order
                if guardian_idx <= last_idx {
                    verification_result = false;
                }
                last_idx = guardian_idx;

                // Check bounds
                if guardian_idx >= 19 {
                    verification_result = false;
                }

                if guardian_idx < 19 {
                    // Get guardian using contract storage - inline to avoid self calls
                    let guardian = if guardian_idx == 0 { 
                        storage.guardian_1.at(guardian_set_index).read()
                    } else if guardian_idx == 1 { 
                        storage.guardian_2.at(guardian_set_index).read()
                    } else if guardian_idx == 2 { 
                        storage.guardian_3.at(guardian_set_index).read()
                    } else if guardian_idx == 3 { 
                        storage.guardian_4.at(guardian_set_index).read()
                    } else if guardian_idx == 4 { 
                        storage.guardian_5.at(guardian_set_index).read()
                    } else if guardian_idx == 5 { 
                        storage.guardian_6.at(guardian_set_index).read()
                    } else if guardian_idx == 6 { 
                        storage.guardian_7.at(guardian_set_index).read()
                    } else if guardian_idx == 7 { 
                        storage.guardian_8.at(guardian_set_index).read()
                    } else if guardian_idx == 8 { 
                        storage.guardian_9.at(guardian_set_index).read()
                    } else if guardian_idx == 9 { 
                        storage.guardian_10.at(guardian_set_index).read()
                    } else if guardian_idx == 10 { 
                        storage.guardian_11.at(guardian_set_index).read()
                    } else if guardian_idx == 11 { 
                        storage.guardian_12.at(guardian_set_index).read()
                    } else if guardian_idx == 12 { 
                        storage.guardian_13.at(guardian_set_index).read()
                    } else if guardian_idx == 13 { 
                        storage.guardian_14.at(guardian_set_index).read()
                    } else if guardian_idx == 14 { 
                        storage.guardian_15.at(guardian_set_index).read()
                    } else if guardian_idx == 15 { 
                        storage.guardian_16.at(guardian_set_index).read()
                    } else if guardian_idx == 16 { 
                        storage.guardian_17.at(guardian_set_index).read()
                    } else if guardian_idx == 17 { 
                        storage.guardian_18.at(guardian_set_index).read()
                    } else if guardian_idx == 18 { 
                        storage.guardian_19.at(guardian_set_index).read()
                    } else {
                        Guardian::default()
                    };

                    let pub_key_x = guardian.get_pub_key_x();
                    let pub_key_y = guardian.get_pub_key_y();

                    // Prepare signature bytes
                    let mut signature_bytes = [0; 64];
                    for j in 0..32 {
                        signature_bytes[j] = sig.r[j];
                        signature_bytes[32 + j] = sig.s[j];
                    }

                    // Verify signature
                    let is_valid = Signature::verify_signature(
                        pub_key_x, 
                        pub_key_y, 
                        signature_bytes, 
                        hash
                    );
                    
                    if is_valid {
                        valid_signatures += 1;
                    } else {
                        verification_result = false;
                    }
                }
            }
        }

        // Need at least 2/3 + 1 of guardians to sign (13 out of 19)
        let required_signatures = ((19 * 2) / 3) + 1;
        if valid_signatures < required_signatures {
            verification_result = false;
        }

        // Assert verification passed
        assert(verification_result, "VAA verification failed");
        
        // Parse and return the VAA
        let body = Body::from_bytes(body_bytes);

        VAA::new(
            version,
            body.timestamp,
            body.nonce,
            body.emitter_chain_id,
            body.emitter_address,
            body.sequence,
            body.consistency_level,
            body.payload,
            guardian_set_index,
            signatures,
            hash
        )
    }
}