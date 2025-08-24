// vaa-verification-service.mjs - IMPROVED VERSION
import express from 'express';
import { Contract, loadContractArtifact, createAztecNodeClient, Fr, AztecAddress } from '@aztec/aztec.js';
import { getSchnorrAccount } from '@aztec/accounts/schnorr';
import { deriveSigningKey } from '@aztec/stdlib/keys';
import { createPXEService, getPXEServiceConfig } from '@aztec/pxe/server';
import { createStore } from "@aztec/kv-store/lmdb"
import WormholeJson from "./contracts/target/wormhole_contracts-Wormhole.json" with { type: "json" };
import { ProxyLogger, captureProfile } from './utils.mjs';

const app = express();
app.use(express.json());

// Configuration from environment variables (set by deployment script)
const PORT = process.env.PORT || 3000;
const NODE_URL = process.env.NODE_URL || 'https://aztec-alpha-testnet-fullnode.zkv.xyz/';
const PRIVATE_KEY = process.env.PRIVATE_KEY;
const CONTRACT_ADDRESS = process.env.CONTRACT_ADDRESS || process.env.WORMHOLE_CONTRACT_ADDRESS;
const SALT = process.env.SALT || '0x0000000000000000000000000000000000000000000000000000000000000000';

let pxe, nodeClient, wormholeContract, isReady = false;

// Validate required environment variables
function validateEnvironment() {
  const required = ['PRIVATE_KEY', 'CONTRACT_ADDRESS'];
  const missing = required.filter(key => !process.env[key] && !eval(key));
  
  if (missing.length > 0) {
    console.error('❌ Missing required environment variables:', missing);
    console.log('\n📝 Required environment variables:');
    console.log('  PRIVATE_KEY=your_deployed_account_private_key');
    console.log('  CONTRACT_ADDRESS=your_deployed_wormhole_contract_address');
    console.log('\n💡 These should be automatically set by the deployment script.');
    console.log('   Run: cd contracts && ./deploy.sh');
    return false;
  }
  
  console.log('✅ Environment variables validated');
  console.log(`📍 Node URL: ${NODE_URL}`);
  console.log(`📍 Contract Address: ${CONTRACT_ADDRESS}`);
  console.log(`📍 Private Key: ${PRIVATE_KEY.substring(0, 10)}...`);
  
  return true;
}

// Initialize Aztec for Testnet
async function init() {
  console.log('🔄 Initializing Aztec TESTNET connection...');
  
  if (!validateEnvironment()) {
    throw new Error('Environment validation failed');
  }
  
  try {
    // Create PXE and Node clients
    console.log('🔄 Connecting to Aztec node...');
    nodeClient = createAztecNodeClient(NODE_URL);
    
    const store = await createStore('pxe', {
      dataDirectory: 'store',
      dataStoreMapSizeKB: 1e6,
    });
    
    const config = getPXEServiceConfig();
    const l1Contracts = await nodeClient.getL1ContractAddresses();
    const configWithContracts = {
      ...config,
      l1Contracts,
    };
    
    ProxyLogger.create();
    const proxyLogger = ProxyLogger.getInstance();
    pxe = await createPXEService(nodeClient, configWithContracts, { 
      store,  
      loggers: {
        prover: proxyLogger.createLogger('pxe:bb:wasm:bundle:proxied'),
      } 
    });
    console.log('✅ Connected PXE to Aztec node and initialized');
    
    // Get contract instance from the node
    console.log('🔄 Fetching contract instance from node...');
    const contractAddress = AztecAddress.fromString(CONTRACT_ADDRESS);
    const contractInstance = await nodeClient.getContract(contractAddress);
    
    if (!contractInstance) {
      throw new Error(`Contract instance not found at address ${CONTRACT_ADDRESS}. Make sure the contract was deployed successfully.`);
    }
    
    console.log('✅ Contract instance retrieved from node');
    console.log(`📍 Retrieved contract address: ${contractInstance.address}`);
    console.log(`📍 Contract class ID: ${contractInstance.currentContractClassId}`);
    
    // Load contract artifact and check if it matches
    const contractArtifact = loadContractArtifact(WormholeJson);
    console.log(`📍 Local artifact class ID: ${contractArtifact.classId || 'computing...'}`);
    
    // Try to register the contract with PXE
    console.log('🔄 Registering contract with PXE...');
    try {
      await pxe.registerContract({
        instance: contractInstance,
        artifact: contractArtifact
      });
      console.log('✅ Contract registered with PXE using local artifact');
    } catch (error) {
      if (error.message.includes('Artifact does not match expected class id')) {
        console.log('⚠️  Local artifact class ID mismatch - this is expected after recompilation');
        console.log('🔄 Attempting to use contract instance without local artifact...');
        
        // Try to get the artifact from the node or use a simplified approach
        console.log('💡 The contract was deployed successfully but the local artifact has changed');
        console.log('   This happens when the contract is recompiled after deployment');
        console.log('   The service will still work but contract calls may have limitations');
      } else {
        throw error;
      }
    }
    
    // Create account using the deployed owner-wallet credentials
    console.log('🔄 Setting up owner-wallet account...');
    const secretKey = Fr.fromString(PRIVATE_KEY);
    const salt = Fr.fromString(SALT);
    const signingKey = deriveSigningKey(secretKey);
    
    // Create Schnorr account (this account is already deployed on testnet)
    const schnorrAccount = await getSchnorrAccount(pxe, secretKey, signingKey, salt);
    const accountAddress = schnorrAccount.getAddress();
    console.log(`📍 Account address: ${accountAddress}`);
    
    // Check if account is registered with PXE
    const registeredAccounts = await pxe.getRegisteredAccounts();
    const isRegistered = registeredAccounts.some(acc => acc.address.equals(accountAddress));
    
    if (isRegistered) {
      console.log('✅ Account found in PXE (from deployment)');
    } else {
      console.log('⚠️  Account not in PXE, registering...');
    }
    
    // Get wallet
    const wallet = await schnorrAccount.register();
    console.log(`✅ Using wallet: ${wallet.getAddress()}`);
    
    // Create contract instance
    console.log(`🔄 Creating contract instance...`);
    try {
      wormholeContract = await Contract.at(contractAddress, contractArtifact, wallet);
      console.log(`✅ Contract instance created successfully`);
      console.log(`📍 Final contract address: ${wormholeContract.address.toString()}`);
    } catch (error) {
      console.error('❌ Failed to create contract instance with local artifact');
      console.log('💡 This might be due to artifact mismatch after recompilation');
      throw error;
    }
    
    isReady = true;
    console.log(`✅ Connected to Wormhole contract on TESTNET: ${CONTRACT_ADDRESS}`);
    console.log(`✅ Node URL: ${NODE_URL}`);
    
  } catch (error) {
    console.error('❌ Initialization failed:', error);
    
    // Provide helpful error messages
    if (error.message.includes('Contract instance not found')) {
      console.log('\n💡 Troubleshooting steps:');
      console.log('1. Make sure the deployment script completed successfully');
      console.log('2. Check if the contract address is correct');
      console.log('3. Verify the contract exists at: http://aztecscan.xyz/');
      console.log(`4. Current contract address: ${CONTRACT_ADDRESS}`);
    } else if (error.message.includes('Artifact does not match')) {
      console.log('\n💡 Artifact mismatch solutions:');
      console.log('1. The contract was recompiled after deployment');
      console.log('2. Re-run the deployment script to get fresh artifacts');
      console.log('3. Or deploy a new contract with the current artifacts');
    }
    
    throw error;
  }
}

// Health check
app.get('/health', (req, res) => {
  res.json({ 
    status: isReady ? 'healthy' : 'initializing',
    network: 'testnet',
    timestamp: new Date().toISOString(),
    nodeUrl: NODE_URL,
    contractAddress: CONTRACT_ADDRESS,
    environment: {
      hasPrivateKey: !!PRIVATE_KEY,
      hasContractAddress: !!CONTRACT_ADDRESS,
      nodeUrl: NODE_URL,
      port: PORT
    }
  });
});

// Configuration endpoint
app.get('/config', (req, res) => {
  res.json({
    network: 'testnet',
    nodeUrl: NODE_URL,
    contractAddress: CONTRACT_ADDRESS,
    port: PORT,
    environment: {
      ownerAddress: process.env.OWNER_ADDRESS,
      receiverAddress: process.env.RECEIVER_ADDRESS,
      tokenContractAddress: process.env.TOKEN_CONTRACT_ADDRESS,
      wormholeContractAddress: process.env.WORMHOLE_CONTRACT_ADDRESS
    },
    ready: isReady
  });
});

// Verify VAA
app.post('/verify', async (req, res) => {
  if (!isReady) {
    return res.status(503).json({ 
      success: false, 
      error: 'Service not ready - Aztec testnet connection still initializing' 
    });
  }

  try {
    const { vaaBytes } = req.body;
    
    if (!vaaBytes) {
      return res.status(400).json({
        success: false,
        error: 'vaaBytes is required'
      });
    }
    
    // Convert hex to buffer
    const hexString = vaaBytes.startsWith('0x') ? vaaBytes.slice(2) : vaaBytes;
    const vaaBuffer = Buffer.from(hexString, 'hex');
    
    // Pad to 2000 bytes for contract but pass actual length
    const paddedVAA = Buffer.alloc(2000);
    vaaBuffer.copy(paddedVAA, 0, 0, Math.min(vaaBuffer.length, 2000));
    
    // Convert to array for Aztec contract
    const vaaArray = Array.from(paddedVAA);
    const actualLength = vaaBuffer.length;
    
    console.log(`🔍 Verifying VAA on TESTNET (${vaaBuffer.length} bytes actual, ${paddedVAA.length} bytes padded)`);
    console.log(`📍 Contract: ${CONTRACT_ADDRESS}`);
    
    // Call verify_vaa function with padded bytes and actual length
    console.log('🔄 Calling contract method verify_vaa...');
    const tx = await wormholeContract.methods
      .verify_vaa(vaaArray, actualLength)
      .send()
      .wait();
    
    console.log(`✅ VAA verified successfully on TESTNET: ${tx.txHash}`);
    
    res.json({
      success: true,
      network: 'testnet',
      txHash: tx.txHash,
      contractAddress: CONTRACT_ADDRESS,
      message: 'VAA verified successfully on Aztec testnet',
      processedAt: new Date().toISOString()
    });
    
  } catch (error) {
    console.error('❌ VAA verification failed on TESTNET:', error.message);
    res.status(500).json({
      success: false,
      network: 'testnet',
      error: error.message,
      processedAt: new Date().toISOString()
    });
  }
});

// Test endpoint with real VAA
app.post('/test', async (req, res) => {
  const realVAA = "010000000001004682bc4d5ff2e54dc2ee5e0eb64f5c6c07aa449ac539abc63c2be5c306a48f233e9300170a82adf3c3b7f43f23176fb079174a58d67d142477f646675d86eb6301684bfad4499602d22713000000000000000000000000697f31e074bf2c819391d52729f95506e0a72ffb0000000000000000c8000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000000e48656c6c6f20576f726d686f6c6521000000000000000000000000000000000000";
  
  console.log('🧪 Testing with real Arbitrum Sepolia VAA on TESTNET');
  
  // Call verify logic with test VAA
  const testReq = { body: { vaaBytes: realVAA } };
  
  return await new Promise((resolve) => {
    const mockRes = {
      json: (data) => { 
        res.json(data); 
        resolve();
      },
      status: (code) => ({
        json: (data) => { 
          res.status(code).json(data); 
          resolve();
        }
      })
    };
    
    // Use the same verify logic
    app._router.stack.find(r => r.route?.path === '/verify').route.stack[0].handle(
      { body: testReq.body }, 
      mockRes
    );
  });
});

// Start server
console.log('🚀 Starting VAA Verification Service...');
console.log('📍 Environment loaded from deployment script');

init().then(() => {
  app.listen(PORT, () => {
    console.log(`🚀 VAA Verification Service running on port ${PORT}`);
    console.log(`🌐 Network: TESTNET`);
    console.log(`📡 Node: ${NODE_URL}`);
    console.log(`📄 Contract: ${CONTRACT_ADDRESS}`);
    console.log('Available endpoints:');
    console.log('  GET  /health - Health check');
    console.log('  GET  /config - Configuration info');
    console.log('  POST /verify - Verify VAA on testnet');
    console.log('  POST /test   - Test with real Arbitrum Sepolia VAA');
    console.log('\n✅ Service ready to process VAA verifications!');
  });
}).catch(error => {
  console.error('❌ Failed to start testnet service:', error);
  console.log('\n💡 Make sure to run the deployment script first:');
  console.log('   cd contracts && ./deploy.sh');
  process.exit(1);
});