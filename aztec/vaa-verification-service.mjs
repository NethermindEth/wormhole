// vaa-verification-service.mjs - TESTNET VERSION (FIXED)
import express from 'express';
import { SponsoredFeePaymentMethod, getContractInstanceFromDeployParams, Contract, loadContractArtifact, createAztecNodeClient, Fr, AztecAddress } from '@aztec/aztec.js';
import { getSchnorrAccount } from '@aztec/accounts/schnorr';
import { deriveSigningKey } from '@aztec/stdlib/keys';
import { createPXEService, getPXEServiceConfig } from '@aztec/pxe/server';
import { createStore } from "@aztec/kv-store/lmdb"
import { SPONSORED_FPC_SALT } from '@aztec/constants';
import { SponsoredFPCContract } from "@aztec/noir-contracts.js/SponsoredFPC";
import { TokenContract } from "@aztec/noir-contracts.js/Token";
import WormholeJson from "./contracts/target/wormhole_contracts-Wormhole.json" with { type: "json" };
import { ProxyLogger, captureProfile } from './utils.mjs';

const app = express();
app.use(express.json());

const PORT = process.env.PORT || 3000;

// TESTNET CONFIGURATION - UPDATED WITH FRESH DEPLOYMENT
const NODE_URL = 'https://aztec-alpha-testnet-fullnode.zkv.xyz/';
const PRIVATE_KEY = '0x9015e46f2e11a7784351ed72fc440d54d06a4a61c88b124f59892b27f9b91301'; // owner-wallet secret key. TODO: change and move to .env
const CONTRACT_ADDRESS = '0x0848d2af89dfd7c0e171238f9216399e61e908cd31b0222a920f1bf621a16ed6'; // Fresh Wormhole contract
const TOKEN_ADDRESS = '0x037e5d19d6d27e2fb7c947cfe7c36459e27d35e46dd59f5f47373a64ff491d2c'; // Token contract address
const RECEIVER_ADDRESS = '0x0d071eec273fa0c82825d9c5d2096965a40bcc33ae942714cf6c683af9632504'; // Receiver address
const SALT = '0x0000000000000000000000000000000000000000000000000000000000000000'; // Salt used in deployment

let pxe, nodeClient, wormholeContract, tokenContract, paymentMethod, wallet, isReady = false;
let currentTokenNonce = 0n; // Start with 0 like the working deployment scripts

// Helper function to get the SponsoredFPC instance
async function getSponsoredFPCInstance() {
  return await getContractInstanceFromDeployParams(SponsoredFPCContract.artifact, {
    salt: new Fr(SPONSORED_FPC_SALT),
  });
}

// Helper function to prepare payloads from message
function preparePayloads(message) {
  const messageBytes = new TextEncoder().encode(message);
  const PAYLOAD_SIZE = 31;
  
  // Create padded bytes for a single payload
  let paddedBytes = new Array(PAYLOAD_SIZE).fill(0);
  
  // Copy the message bytes into the padded array
  for (let i = 0; i < messageBytes.length && i < PAYLOAD_SIZE; i++) {
    paddedBytes[i] = messageBytes[i];
  }

  // Create 8 identical payloads as expected by the contract
  let payloads = [];
  for (let i = 0; i < 8; i++) {
    payloads.push(paddedBytes);
  }
  
  return payloads;
}

// Initialize Aztec for Testnet
async function init() {
  console.log('🔄 Initializing Aztec TESTNET connection...');
  
  if (!PRIVATE_KEY) {
    throw new Error('PRIVATE_KEY environment variable is required for testnet');
  }
  
  if (!CONTRACT_ADDRESS) {
    throw new Error('CONTRACT_ADDRESS environment variable is required for testnet');
  }
  
  try {
    // Create PXE and Node clients
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
    
    const sponsoredFPC = await getSponsoredFPCInstance();
    await pxe.registerContract({
      instance: sponsoredFPC,
      artifact: SponsoredFPCContract.artifact,
    });
    paymentMethod = new SponsoredFeePaymentMethod(sponsoredFPC.address);

    // Get Wormhole contract instance from the node (Alex's simpler approach)
    console.log('🔄 Fetching Wormhole contract instance from node...');
    const wormholeAddress = AztecAddress.fromString(CONTRACT_ADDRESS);
    const wormholeInstance = await nodeClient.getContract(wormholeAddress);
    
    if (!wormholeInstance) {
      throw new Error(`Wormhole contract instance not found at address ${CONTRACT_ADDRESS}`);
    }
    
    console.log('✅ Wormhole contract instance retrieved from node');
    console.log(`📍 Retrieved Wormhole contract address: ${wormholeInstance.address}`);
    console.log(`📍 Wormhole contract class ID: ${wormholeInstance.currentContractClassId}`);
    
    // Get Token contract instance from the node
    console.log('🔄 Fetching Token contract instance from node...');
    const tokenAddress = AztecAddress.fromString(TOKEN_ADDRESS);
    const tokenInstance = await nodeClient.getContract(tokenAddress);
    
    if (!tokenInstance) {
      throw new Error(`Token contract instance not found at address ${TOKEN_ADDRESS}`);
    }
    
    console.log('✅ Token contract instance retrieved from node');
    
    // Load contract artifacts
    const wormholeArtifact = loadContractArtifact(WormholeJson);
    
    // Register contracts with PXE (Alex's guidance)
    console.log('🔄 Registering contracts with PXE...');
    await pxe.registerContract({
      instance: wormholeInstance,
      artifact: wormholeArtifact
    });
    
    await pxe.registerContract({
      instance: tokenInstance,
      artifact: TokenContract.artifact
    });
    
    console.log('✅ Contracts registered with PXE');
    
    // Create account using the deployed owner-wallet credentials
    console.log('🔄 Setting up owner-wallet account...');
    const secretKey = Fr.fromString(PRIVATE_KEY);
    const salt = Fr.fromString(SALT);
    const signingKey = deriveSigningKey(secretKey);
    
    console.log(`🔑 Using secret key: ${secretKey.toString()}`);
    console.log(`🧂 Using salt: ${salt.toString()}`);
    
    // Create Schnorr account (this account is already deployed on testnet)
    const schnorrAccount = await getSchnorrAccount(pxe, secretKey, signingKey, salt);
    const accountAddress = schnorrAccount.getAddress();
    console.log(`📍 Account address: ${accountAddress}`);
    
    // This account should already be registered with the PXE from the deployment
    const registeredAccounts = await pxe.getRegisteredAccounts();
    const isRegistered = registeredAccounts.some(acc => acc.address.equals(accountAddress));
    
    if (isRegistered) {
      console.log('✅ Account found in PXE (from aztec-wallet deployment)');
    } else {
      console.log('⚠️  Account not in PXE, but it exists on testnet. Getting wallet anyway...');
    }
    
    // Get wallet (this should work since the account exists on testnet)
    wallet = await schnorrAccount.register();
    console.log(`✅ Using wallet: ${wallet.getAddress()}`);
    
    // Create contract objects
    console.log(`🔄 Creating contract instances...`);
    console.log(`📍 Wormhole artifact name: ${wormholeArtifact.name}`);
    
    try {
      wormholeContract = await Contract.at(wormholeAddress, wormholeArtifact, wallet);
      tokenContract = await Contract.at(tokenAddress, TokenContract.artifact, wallet);
      console.log(`✅ Contract instances created successfully`);
      console.log(`📍 Wormhole contract address: ${wormholeContract.address.toString()}`);
      console.log(`📍 Token contract address: ${tokenContract.address.toString()}`);
      
    } catch (error) {
      console.error('❌ Failed to create contract instances:', error);
      throw error;
    }
    
    isReady = true;
    console.log(`✅ Connected to Wormhole contract on TESTNET: ${CONTRACT_ADDRESS}`);
    console.log(`✅ Node URL: ${NODE_URL}`);
    
  } catch (error) {
    console.error('❌ Initialization failed:', error);
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
    wormholeContract: CONTRACT_ADDRESS,
    tokenContract: TOKEN_ADDRESS,
    receiverAddress: RECEIVER_ADDRESS,
    currentTokenNonce: currentTokenNonce.toString(),
    walletAddress: wallet ? wallet.getAddress().toString() : 'initializing'
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
    console.log(`📍 Contract object address: ${wormholeContract.address.toString()}`);
    console.log(`📍 Wallet address: ${wormholeContract.wallet.getAddress().toString()}`);
    
    // Call verify_vaa function with padded bytes and actual length
    console.log('🔄 Calling contract method verify_vaa...');
    const tx = await wormholeContract.methods
      .verify_vaa(vaaArray, actualLength)
      .send({ fee: { paymentMethod } })
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


// Test endpoint with Jorge's real Arbitrum Sepolia VAA
app.post('/test', async (req, res) => {
  // Jorge's real VAA from Arbitrum Sepolia that uses Guardian 0x13947Bd48b18E53fdAeEe77F3473391aC727C638
  // This VAA contains "Hello Wormhole!" message and has been verified on Wormholescan
  // Link: https://wormholescan.io/#/tx/0xf93fd41efeb09ff28174824d4abf6dbc06ac408953a9975aa4a403d434051efc?network=Testnet&view=advanced
  const realVAA = "010000000001004682bc4d5ff2e54dc2ee5e0eb64f5c6c07aa449ac539abc63c2be5c306a48f233e9300170a82adf3c3b7f43f23176fb079174a58d67d142477f646675d86eb6301684bfad4499602d22713000000000000000000000000697f31e074bf2c819391d52729f95506e0a72ffb0000000000000000c8000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000000e48656c6c6f20576f726d686f6c6521000000000000000000000000000000000000";
  
  console.log('🧪 Testing with real Arbitrum Sepolia VAA on TESTNET');
  console.log('📍 Guardian: 0x13947Bd48b18E53fdAeEe77F3473391aC727C638');
  console.log('📍 Signature: 0x4682bc4d5ff2e54dc2ee5e0eb64f5c6c07aa449ac539abc63c2be5c306a48f233e9300170a82adf3c3b7f43f23176fb079174a58d67d142477f646675d86eb6301');
  console.log('📍 Expected message hash: 0xe64320fba193c98f2d0acf3a8c7479ec9b163192bfc19d4024497d4e4159758c');
  console.log('📍 WormholeScan: https://wormholescan.io/#/tx/0xf93fd41efeb09ff28174824d4abf6dbc06ac408953a9975aa4a403d434051efc?network=Testnet&view=advanced');
  
  // Debug contract state before calling verify
  console.log('🔍 Pre-verification debug:');
  console.log(`   - Service ready: ${isReady}`);
  console.log(`   - Contract object exists: ${!!wormholeContract}`);
  if (wormholeContract) {
    console.log(`   - Contract address: ${wormholeContract.address.toString()}`);
    console.log(`   - Expected address: ${CONTRACT_ADDRESS}`);
  }
  
  // Set up request body and call verify logic directly
  const testReq = { 
    body: { vaaBytes: realVAA },
    // Add debug flag
    isTest: true
  };
  
  // Call verify logic directly instead of using the router
  if (!isReady) {
    return res.status(503).json({ 
      success: false, 
      error: 'Service not ready - Aztec testnet connection still initializing' 
    });
  }

  try {
  const { vaaBytes } = testReq.body;
  
  // Convert hex to buffer
  const hexString = vaaBytes.startsWith('0x') ? vaaBytes.slice(2) : vaaBytes;
  const vaaBuffer = Buffer.from(hexString, 'hex');
  
  // Debug the VAA data
  console.log('🔍 VAA Debug Info:');
  console.log(`   Raw hex length: ${hexString.length}`);
  console.log(`   Buffer length: ${vaaBuffer.length}`);
  console.log(`   First 20 bytes: ${vaaBuffer.slice(0, 20).toString('hex')}`);
  console.log(`   Last 20 bytes: ${vaaBuffer.slice(-20).toString('hex')}`);
  
  // Back to padded version (contract expects fixed size)
  const paddedVAA = Buffer.alloc(2000);
  vaaBuffer.copy(paddedVAA, 0, 0, Math.min(vaaBuffer.length, 2000));
  const vaaArray = Array.from(paddedVAA);
  const actualLength = vaaBuffer.length;
  
  console.log('🔍 Using PADDED version (contract expects fixed size):');
  console.log(`   Padded array length: ${vaaArray.length}`);
  console.log(`   Actual VAA length param: ${actualLength}`);
  console.log(`   First few padded elements: [${vaaArray.slice(0, 10).join(', ')}]`);
  console.log(`   Elements around actual length: [${vaaArray.slice(actualLength-5, actualLength+10).join(', ')}]`);
  
  console.log(`🔍 Verifying VAA on TESTNET (${vaaBuffer.length} bytes actual, ${paddedVAA.length} bytes padded)`);
  console.log(`📍 Contract: ${CONTRACT_ADDRESS}`);
  console.log(`📍 Contract object address: ${wormholeContract.address.toString()}`);
  console.log(`📍 Wallet address: ${wormholeContract.wallet.getAddress().toString()}`);
  
  // Call verify_vaa function with padded bytes and actual length
  console.log('🔄 Calling contract method verify_vaa with PADDED data...');
  const interaction = await wormholeContract.methods
      .verify_vaa(vaaArray, actualLength);

  console.log('🔄 Capturing interaction profile...');

  await captureProfile('verify_vaa', interaction);

  console.log('🔄 Sending transaction...');
  await interaction.send({ fee: { paymentMethod } }).wait();
  
  console.log(`✅ VAA verified successfully on TESTNET: ${tx.txHash}`);
  
  res.json({
    success: true,
    network: 'testnet',
    txHash: tx.txHash,
    contractAddress: CONTRACT_ADDRESS,
    message: 'VAA verified successfully on Aztec testnet (TEST ENDPOINT)',
    processedAt: new Date().toISOString()
  });
  } catch (error) {
    console.error('❌ VAA verification failed on TESTNET:', error.message);
    console.error('❌ Full error:', error);
    res.status(500).json({
      success: false,
      network: 'testnet',
      error: error.message,
      processedAt: new Date().toISOString()
    });
  }
});

// Test private message publishing endpoint
app.post('/test-publish', async (req, res) => {
  console.log('🧪 Testing private message publishing with predefined message on TESTNET');
  
  const testMessage = "Hello Wormhole from Aztec Private!";
  console.log(`📝 Test message: "${testMessage}"`);
  
  // Set up request body similar to test VAA endpoint
  const testReq = { 
    body: { 
      message: testMessage,
      nonce: 123,
      consistency: 2,
      messageFee: 1
    },
    isTest: true
  };
  
  // Debug contract state before calling publish (same pattern as VAA test)
  console.log('🔍 Pre-publish debug:');
  console.log(`   - Service ready: ${isReady}`);
  console.log(`   - Contract object exists: ${!!wormholeContract}`);
  console.log(`   - Token contract exists: ${!!tokenContract}`);
  if (wormholeContract) {
    console.log(`   - Wormhole contract address: ${wormholeContract.address.toString()}`);
    console.log(`   - Expected address: ${CONTRACT_ADDRESS}`);
    console.log(`   - Current token nonce: ${currentTokenNonce}`);
  }
  
  // Call publish logic directly (same pattern as VAA test endpoint)
  if (!isReady) {
    return res.status(503).json({ 
      success: false, 
      error: 'Service not ready - Aztec testnet connection still initializing' 
    });
  }

  try {
    const { message, nonce, consistency, messageFee } = testReq.body;
    
    const publishNonce = nonce || 100;
    const publishConsistency = consistency || 2;
    const publishMessageFee = BigInt(messageFee || 1);
    
    // Increment the nonce for this transaction (like working blueprint)
    currentTokenNonce = currentTokenNonce + 1n;
    const tokenNonceForTestTx = currentTokenNonce;
    
    console.log(`📝 Publishing test private message: "${message}"`);
    console.log(`🔢 Using nonce: ${publishNonce}, consistency: ${publishConsistency}, fee: ${publishMessageFee}`);
    console.log(`🎫 Token nonce: ${tokenNonceForTestTx}`);
    
    const payloads = preparePayloads(message);
    console.log(`📦 Prepared payloads for test message`);
    
    const ownerAddress = wallet.getAddress();
    const receiverAddress = AztecAddress.fromString(RECEIVER_ADDRESS);
    
    console.log('🔐 Creating private transfer authwit for test...');
    const privateAction = tokenContract.methods.transfer_in_private(
      ownerAddress,
      receiverAddress,
      publishMessageFee,
      tokenNonceForTestTx
    );
    
    console.log(`${ownerAddress.toString()} is transferring ${publishMessageFee} tokens to ${receiverAddress.toString()} in private (test)`);
    
    const wormholeAuthWit = await wallet.createAuthWit(
      {
        caller: wormholeContract.address,
        action: privateAction
      },
      true  // Add the 'true' flag like in working blueprint
    );
    
    console.log('✅ Generated Wormhole authwit for test');
    
    console.log('🔄 Calling contract method publish_message_in_private for test...');
    const interaction = wormholeContract.methods.publish_message_in_private(
      publishNonce,
      payloads,
      publishMessageFee,
      publishConsistency,
      ownerAddress,
      tokenNonceForTestTx
    );
    
    console.log('🔄 Capturing test interaction profile...');
    await captureProfile('publish_message_in_private_test', interaction);
    
    console.log('🔄 Sending test private message transaction...');
    const tx = await interaction.send({ 
      authWitnesses: [wormholeAuthWit],
      fee: { paymentMethod } 
    }).wait();
    
    console.log(`✅ Test private message published successfully on TESTNET: ${tx.txHash}`);
    
    res.json({
      success: true,
      network: 'testnet',
      txHash: tx.txHash,
      wormholeContract: CONTRACT_ADDRESS,
      tokenContract: TOKEN_ADDRESS,
      message: `TEST: Private message "${message}" published successfully on Aztec testnet`,
      nonce: publishNonce,
      consistency: publishConsistency,
      messageFee: publishMessageFee.toString(),
      tokenNonce: tokenNonceForTestTx.toString(),
      isTest: true,
      processedAt: new Date().toISOString()
    });
    
  } catch (error) {
    console.error('❌ Test private message publishing failed on TESTNET:', error.message);
    console.error('❌ Full error:', error);
    res.status(500).json({
      success: false,
      network: 'testnet',
      error: error.message,
      isTest: true,
      processedAt: new Date().toISOString()
    });
  }
});

// Start server
init().then(() => {
  app.listen(PORT, () => {
    console.log(`🚀 VAA Verification & Private Message Service running on port ${PORT}`);
    console.log(`🌐 Network: TESTNET`);
    console.log(`📡 Node: ${NODE_URL}`);
    console.log(`📄 Wormhole Contract: ${CONTRACT_ADDRESS}`);
    console.log(`🪙 Token Contract: ${TOKEN_ADDRESS}`);
    console.log(`📮 Receiver Address: ${RECEIVER_ADDRESS}`);
    console.log('Available endpoints:');
    console.log('  GET  /health - Health check');
    console.log('  POST /verify - Verify VAA on testnet');
    console.log('  POST /test   - Test with Jorge\'s real Arbitrum Sepolia VAA');
    console.log('  POST /publish - Publish private message');
    console.log('  POST /test-publish - Test private message publishing');
  });
}).catch(error => {
  console.error('❌ Failed to start testnet service:', error);
  console.log('\n📝 Required environment variables:');
  console.log('  PRIVATE_KEY=your_testnet_private_key');
  console.log('  CONTRACT_ADDRESS=your_deployed_contract_address');
  process.exit(1);
});