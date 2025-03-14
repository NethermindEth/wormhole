mod messages {
    use dep::std;
    use dep::std::hash::{keccak256, sha256};
    use dep::aztec::crypto::{verify_signature, ecdsa_recover, SchnorrSignature};
    
    use crate::structs::{VM, GuardianSet, Signature, compute_vm_hash};

    use crate::getters{
        get_guardian_set,
        get_guardian_set_index
    };
    
    // Parse and verify a VM
    fn parse_and_verify_vm(encoded_vm: [u8]) -> (VM, bool, str) {
        let vm = parse_vm(encoded_vm);
        
        // Setting check_hash to false as we can trust the hash field in this case
        // given that parseVM computes and then sets the hash field
        let (valid, reason) = verify_vm_internal(vm, false);
        
        (vm, valid, reason)
    }
    
    // Verify a VM against a valid Guardian set
    fn verify_vm(vm: VM) -> (bool, str) {
        verify_vm_internal(vm, true)
    }
    
    // Internal verification function
    fn verify_vm_internal(vm: VM, check_hash: bool) -> (bool, str) {
        // Get the guardian set for the provided index
        let guardian_set = get_guardian_set(vm.guardian_set_index);
        
        // Verify hash if required
        if check_hash {
            let computed_hash = compute_vm_hash(
                vm.timestamp,
                vm.nonce,
                vm.emitter_chain_id,
                vm.emitter_address,
                vm.sequence,
                vm.consistency_level,
                vm.payload
            );
            
            if computed_hash != vm.hash {
                return (false, "vm.hash doesn't match body");
            }
        }
        
        // Check for valid guardian set
        if guardian_set.keys.len() == 0 {
            return (false, "invalid guardian set");
        }
        
        // Check if VM guardian set index matches the current index (unless expired)
        let current_time = dep::std::block::timestamp();
        if vm.guardian_set_index != get_guardian_set_index() && 
           guardian_set.expiration_time < current_time {
            return (false, "guardian set has expired");
        }
        
        // Check for quorum
        if vm.signatures.len() < quorum(guardian_set.keys.len()) {
            return (false, "no quorum");
        }
        
        // Verify signatures against guardian set
        let (signatures_valid, invalid_reason) = verify_signatures(
            vm.hash, 
            vm.signatures, 
            guardian_set
        );
        
        if !signatures_valid {
            return (false, invalid_reason);
        }
        
        // Valid VM
        (true, "")
    }
    
    // Verify signatures against a guardian set
    fn verify_signatures(
        hash: [u8; 32], 
        signatures: [Signature], 
        guardian_set: GuardianSet
    ) -> (bool, str) {
        let mut last_index: u8 = 0;
        let guardian_count = guardian_set.keys.len();
        
        for i in 0..signatures.len() {
            let sig = signatures[i];
            
            // Recover the public key from the signature
            // In Noir, we use ecdsa_recover
            let signatory_opt = ecdsa_recover(
                hash, 
                sig.v, 
                sig.r, 
                sig.s
            );
            
            // Make sure signature recovery succeeded
            match signatory_opt {
                None => return (false, "signature recovery failed"),
                Some(signatory) => {
                    // Ensure signature indices are ascending
                    if i > 0 && sig.guardian_index <= last_index {
                        return (false, "signature indices must be ascending");
                    }
                    last_index = sig.guardian_index;
                    
                    // Ensure index is within bounds
                    if sig.guardian_index as usize >= guardian_count {
                        return (false, "guardian index out of bounds");
                    }
                    
                    // Verify that the signer matches the guardian
                    if signatory != guardian_set.keys[sig.guardian_index as usize] {
                        return (false, "VM signature invalid");
                    }
                }
            }
        }
        
        // All signatures are valid
        (true, "")
    }
    
    // Parse VM from encoded bytes
    fn parse_vm(encoded_vm: [u8]) -> VM {
        let mut index = 0;
        
        // Parse version
        let version = encoded_vm[index];
        index += 1;
        
        // Verify version
        assert(version == 1, "VM version incompatible");
        
        // Parse guardian set index
        let guardian_set_index = bytes_to_field(encoded_vm, index, 4);
        index += 4;
        
        // Parse signatures
        let signers_len = encoded_vm[index] as usize;
        index += 1;
        
        let mut signatures = Vec::with_capacity(signers_len);
        
        for _ in 0..signers_len {
            let guardian_index = encoded_vm[index];
            index += 1;
            
            let r = array_slice_32(encoded_vm, index);
            index += 32;
            
            let s = array_slice_32(encoded_vm, index);
            index += 32;
            
            let v = encoded_vm[index] + 27;
            index += 1;
            
            signatures.push(Signature {
                r,
                s,
                v,
                guardian_index
            });
        }
        
        // Extract the body for hashing
        let body_start = index;
        let body_length = encoded_vm.len() - body_start;
        let body = array_slice(encoded_vm, body_start, body_length);
        
        // Hash the body (double hash as in the original)
        let inner_hash = keccak256(body);
        let hash = keccak256([inner_hash]);
        
        // Parse timestamp
        let timestamp = bytes_to_field(encoded_vm, index, 4);
        index += 4;
        
        // Parse nonce
        let nonce = bytes_to_field(encoded_vm, index, 4);
        index += 4;
        
        // Parse emitter chain ID
        let emitter_chain_id = bytes_to_field(encoded_vm, index, 2);
        index += 2;
        
        // Parse emitter address
        let emitter_address = array_slice_32(encoded_vm, index);
        index += 32;
        
        // Parse sequence
        let sequence = bytes_to_field(encoded_vm, index, 8);
        index += 8;
        
        // Parse consistency level
        let consistency_level = encoded_vm[index];
        index += 1;
        
        // Parse payload (remainder of encoded_vm)
        let payload_length = encoded_vm.len() - index;
        let payload = array_slice(encoded_vm, index, payload_length);
        
        // Construct and return the VM
        VM {
            version,
            guardian_set_index,
            signatures,
            timestamp,
            nonce,
            emitter_chain_id,
            emitter_address,
            sequence,
            consistency_level,
            payload,
            hash
        }
    }
    
    // Calculate quorum threshold
    fn quorum(num_guardians: usize) -> usize {
        // Ensure max guardians is 255
        assert(num_guardians < 256, "too many guardians");
        
        // Calculate (2/3 * num_guardians) + 1
        ((num_guardians * 2) / 3) + 1
    }
    
    // Helper utility functions
    
    // Convert bytes to Field, reading specified number of bytes
    fn bytes_to_field(bytes: [u8], start: usize, length: usize) -> Field {
        let mut result: Field = 0;
        
        for i in 0..length {
            if start + i < bytes.len() {
                // Shift and add each byte
                result = result * 256 + (bytes[start + i] as Field);
            }
        }
        
        result
    }
    
    // Extract a 32-byte array from a larger byte array
    fn array_slice_32(bytes: [u8], start: usize) -> [u8; 32] {
        let mut result = [0; 32];
        
        for i in 0..32 {
            if start + i < bytes.len() {
                result[i] = bytes[start + i];
            }
        }
        
        result
    }
    
    // Extract a slice of bytes from a larger byte array
    fn array_slice(bytes: [u8], start: usize, length: usize) -> [u8] {
        let mut result = Vec::with_capacity(length);
        
        for i in 0..length {
            if start + i < bytes.len() {
                result.push(bytes[start + i]);
            }
        }
        
        result
    }

    // Note: In Aztec, we would use PublicKey for keys rather than addresses
    // This assumes PublicKey is defined or imported elsewhere in the codebase
    
    // Utility method to compute VM hash from components
    fn compute_vm_hash(
        timestamp: Field,
        nonce: Field,
        emitter_chain_id: Field,
        emitter_address: [u8; 32],
        sequence: Field,
        consistency_level: u8,
        payload: [u8]
    ) -> [u8; 32] {
        // Assemble the body 
        let mut body = Vec::new();
        
        // Add timestamp (4 bytes)
        body.extend(timestamp.to_be_bytes(4));
        
        // Add nonce (4 bytes)
        body.extend(nonce.to_be_bytes(4));
        
        // Add emitter chain ID (2 bytes)
        body.extend(emitter_chain_id.to_be_bytes(2));
        
        // Add emitter address (32 bytes)
        body.extend(emitter_address);
        
        // Add sequence (8 bytes)
        body.extend(sequence.to_be_bytes(8));
        
        // Add consistency level (1 byte)
        body.push(consistency_level);
        
        // Add payload (variable length)
        body.extend(payload);
        
        // Hash the body (double hash as in the original)
        let inner_hash = dep::std::hash::keccak256(body);
        dep::std::hash::keccak256([inner_hash])
    }
}