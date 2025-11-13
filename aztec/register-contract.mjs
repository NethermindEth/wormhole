// register-contract.mjs - Register your deployed testnet contract with PXE
import { createPXEClient, waitForPXE, loadContractArtifact, AztecAddress, Fr, Point } from '@aztec/aztec.js';
import WormholeJson from "./contracts/target/wormhole_contracts-Wormhole.json" with { type: "json" };

const PXE_URL = process.env.PXE_URL || 'http://localhost:8080';
const CONTRACT_ADDRESS = process.env.CONTRACT_ADDRESS || '0x2b13cff4daef709134419f1506ccae28956e02102a5ef5f2d0077e4991a9f493';

async function registerDeployedContract() {
  console.log('🔗 Connecting to PXE...');
  const pxe = createPXEClient(PXE_URL);
  await waitForPXE(pxe);
  
  console.log('📦 Loading contract artifact...');
  const contractArtifact = loadContractArtifact(WormholeJson);
  const contractAddress = AztecAddress.fromString(CONTRACT_ADDRESS);
  
  console.log('📡 Registering contract class...');
  await pxe.registerContractClass(contractArtifact);
  
  console.log('🔍 Adding deployed contract instance...');
  // For testnet contracts, we can add them with minimal instance data
  await pxe.addContracts([
    {
      artifact: contractArtifact,
      completeAddress: {
        address: contractAddress,
        publicKeysHash: Fr.ZERO,
        partialAddress: Fr.ZERO
      }
    }
  ]);
  
  console.log(`✅ Successfully registered deployed contract: ${CONTRACT_ADDRESS}`);
  
  // Verify it's registered
  console.log('🔍 Verifying registration...');
  const contracts = await pxe.getContracts();
  const found = contracts.find(c => c.address.equals(contractAddress));
  
  if (found) {
    console.log('✅ Contract found in PXE!');
    console.log(`📍 Address: ${found.address}`);
  } else {
    console.log('❌ Contract not found in PXE after registration');
  }
}

registerDeployedContract().catch(console.error);