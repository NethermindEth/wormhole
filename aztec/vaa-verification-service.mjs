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
import { readFileSync, writeFileSync, existsSync } from 'fs';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';

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
let currentTokenNonce = 0n;
let currentWalletNonce = 100n; // Starting wallet nonce

// Nonce management functions
// Get the directory of the current module (ES module equivalent of __dirname)
const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

// Try multiple possible paths for nonce.json
const possiblePaths = [
  join(process.cwd(), 'packages', 'deploy', 'src', 'nonce.json'), // When running from aztec/
  join(process.cwd(), 'aztec', 'packages', 'deploy', 'src', 'nonce.json'), // When running from project root
  join(__dirname, 'packages', 'deploy', 'src', 'nonce.json'), // Relative to script location
];

function findNonceFilePath() {
  for (const path of possiblePaths) {
    if (existsSync(path)) {
      console.log(`📄 Found nonce file at: ${path}`);
      return path;
    }
  }
  console.log('📄 No nonce file found in any expected location');
  console.log('📄 Tried paths:', possiblePaths);
  return possiblePaths[0]; // Return first path as default
}

const NONCE_FILE_PATH = findNonceFilePath();

function loadNoncesFromFile() {
  try {
    if (existsSync(NONCE_FILE_PATH)) {
      const nonceData = JSON.parse(readFileSync(NONCE_FILE_PATH, 'utf8'));
      const tokenNonce = nonceData.token_nonce ? BigInt(nonceData.token_nonce) : 0n;
      const walletNonce = nonceData.wallet_nonce ? BigInt(nonceData.wallet_nonce) : 100n;
      console.log(`📄 Loaded nonces from file - Token: ${tokenNonce}, Wallet: ${walletNonce}`);
      return { tokenNonce, walletNonce };
    } else {
      console.log('📄 No nonce file found, starting with default nonces');
      return { tokenNonce: 0n, walletNonce: 100n };
    }
  } catch (error) {
    console.error('❌ Error loading nonces from file:', error);
    return { tokenNonce: 0n, walletNonce: 100n };
  }
}

function saveNoncesToFile(tokenNonce, walletNonce) {
  try {
    const nonceData = { 
      token_nonce: tokenNonce.toString(),
      wallet_nonce: walletNonce.toString()
    };
    writeFileSync(NONCE_FILE_PATH, JSON.stringify(nonceData, null, 2));
    console.log(`💾 Saved nonces to file - Token: ${tokenNonce}, Wallet: ${walletNonce}`);
  } catch (error) {
    console.error('❌ Error saving nonces to file:', error);
    throw error;
  }
}

function getNextTokenNonce() {
  currentTokenNonce = currentTokenNonce + 1n;
  saveNoncesToFile(currentTokenNonce, currentWalletNonce);
  return currentTokenNonce;
}

function getNextWalletNonce() {
  currentWalletNonce = currentWalletNonce + 1n;
  saveNoncesToFile(currentTokenNonce, currentWalletNonce);
  return currentWalletNonce;
}

function validateTokenNonce(nonce) {
  if (nonce <= currentTokenNonce) {
    throw new Error(`Token nonce ${nonce} is not greater than current token nonce ${currentTokenNonce}. This will cause existingnullifier errors.`);
  }
  return true;
}

function validateWalletNonce(nonce) {
  if (nonce <= currentWalletNonce) {
    throw new Error(`Wallet nonce ${nonce} is not greater than current wallet nonce ${currentWalletNonce}. This will cause existingnullifier errors.`);
  }
  return true;
}

function recoverNoncesFromFile() {
  console.log('🔄 Recovering nonces from file...');
  const { tokenNonce, walletNonce } = loadNoncesFromFile();
  let updated = false;
  
  if (tokenNonce > currentTokenNonce) {
    console.log(`📈 Updating token nonce from ${currentTokenNonce} to ${tokenNonce}`);
    currentTokenNonce = tokenNonce;
    updated = true;
  }
  
  if (walletNonce > currentWalletNonce) {
    console.log(`📈 Updating wallet nonce from ${currentWalletNonce} to ${walletNonce}`);
    currentWalletNonce = walletNonce;
    updated = true;
  }
  
  if (!updated) {
    console.log(`📄 File nonces are not greater than current nonces`);
  }
  
  return { tokenNonce: currentTokenNonce, walletNonce: currentWalletNonce };
}

function handleNonceError(error) {
  if (error.message.includes('existingnullifier') || error.message.includes('Existing nullifier')) {
    console.error('🚨 Nonce conflict detected! Attempting recovery...');
    try {
      const recovered = recoverNoncesFromFile();
      console.log('✅ Nonces recovered from file');
      console.log(`   Token nonce: ${recovered.tokenNonce}, Wallet nonce: ${recovered.walletNonce}`);
      return true; // Indicates recovery was attempted
    } catch (recoveryError) {
      console.error('❌ Nonce recovery failed:', recoveryError);
      return false;
    }
  }
  return false;
}

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
  
  // Load nonces from file first
  const { tokenNonce, walletNonce } = loadNoncesFromFile();
  currentTokenNonce = tokenNonce;
  currentWalletNonce = walletNonce;
  console.log(`🎫 Initialized with nonces - Token: ${currentTokenNonce}, Wallet: ${currentWalletNonce}`);
  
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
    currentWalletNonce: currentWalletNonce.toString(),
    walletAddress: wallet ? wallet.getAddress().toString() : 'initializing'
  });
});

// Nonce status endpoint
app.get('/nonce-status', (req, res) => {
  try {
    const { tokenNonce, walletNonce } = loadNoncesFromFile();
    const tokenInSync = tokenNonce === currentTokenNonce;
    const walletInSync = walletNonce === currentWalletNonce;
    
    res.json({
      success: true,
      currentTokenNonce: currentTokenNonce.toString(),
      currentWalletNonce: currentWalletNonce.toString(),
      fileTokenNonce: tokenNonce.toString(),
      fileWalletNonce: walletNonce.toString(),
      tokenInSync: tokenInSync,
      walletInSync: walletInSync,
      allInSync: tokenInSync && walletInSync,
      nonceFilePath: NONCE_FILE_PATH,
      timestamp: new Date().toISOString()
    });
  } catch (error) {
    res.status(500).json({
      success: false,
      error: error.message,
      timestamp: new Date().toISOString()
    });
  }
});

// Nonce recovery endpoint
app.post('/recover-nonce', (req, res) => {
  try {
    const recovered = recoverNoncesFromFile();
    res.json({
      success: true,
      message: 'Nonces recovered from file',
      previousTokenNonce: currentTokenNonce.toString(),
      previousWalletNonce: currentWalletNonce.toString(),
      recoveredTokenNonce: recovered.tokenNonce.toString(),
      recoveredWalletNonce: recovered.walletNonce.toString(),
      timestamp: new Date().toISOString()
    });
  } catch (error) {
    res.status(500).json({
      success: false,
      error: error.message,
      timestamp: new Date().toISOString()
    });
  }
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
      nonce: 99999,
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
    
    // Get next wallet nonce for the wormhole message
    const publishNonce = getNextWalletNonce();
    const publishConsistency = consistency || 2;
    const publishMessageFee = BigInt(messageFee || 1);
    
    // Get next token nonce for the token transfer
    const tokenNonceForTestTx = getNextTokenNonce();
    
    console.log(`📝 Publishing test private message: "${message}"`);
    console.log(`🔢 Using wallet nonce: ${publishNonce}, consistency: ${publishConsistency}, fee: ${publishMessageFee}`);
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
    
    //console.log('🔄 Capturing test interaction profile...');
    //await captureProfile('publish_message_in_private', interaction);
    
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
    
    // Handle nonce-related errors
    const recoveryAttempted = handleNonceError(error);
    
    res.status(500).json({
      success: false,
      network: 'testnet',
      error: error.message,
      isTest: true,
      recoveryAttempted: recoveryAttempted,
      processedAt: new Date().toISOString()
    });
  }
});

// Publish private message endpoint (production version)
app.post('/publish', async (req, res) => {
  console.log('📝 Publishing private message on TESTNET');
  
  if (!isReady) {
    return res.status(503).json({ 
      success: false, 
      error: 'Service not ready - Aztec testnet connection still initializing' 
    });
  }

  try {
    const { message, nonce, consistency, messageFee } = req.body;
    
    if (!message) {
      return res.status(400).json({
        success: false,
        error: 'message is required'
      });
    }
    
    // Get next wallet nonce for the wormhole message
    const publishNonce = getNextWalletNonce();
    const publishConsistency = consistency || 2;
    const publishMessageFee = BigInt(messageFee || 1);
    
    // Get next token nonce for the token transfer
    const tokenNonceForTestTx = getNextTokenNonce();
    
    console.log(`📝 Publishing private message: "${message}"`);
    console.log(`🔢 Using wallet nonce: ${publishNonce}, consistency: ${publishConsistency}, fee: ${publishMessageFee}`);
    console.log(`🎫 Token nonce: ${tokenNonceForTestTx}`);
    
    const payloads = preparePayloads(message);
    console.log(`📦 Prepared payloads for message`);
    
    const ownerAddress = wallet.getAddress();
    const receiverAddress = AztecAddress.fromString(RECEIVER_ADDRESS);
    
    console.log('🔐 Creating private transfer authwit...');
    const privateAction = tokenContract.methods.transfer_in_private(
      ownerAddress,
      receiverAddress,
      publishMessageFee,
      tokenNonceForTestTx
    );
    
    console.log(`${ownerAddress.toString()} is transferring ${publishMessageFee} tokens to ${receiverAddress.toString()} in private`);
    
    const wormholeAuthWit = await wallet.createAuthWit(
      {
        caller: wormholeContract.address,
        action: privateAction
      },
      true
    );
    
    console.log('✅ Generated Wormhole authwit');
    
    console.log('🔄 Calling contract method publish_message_in_private...');
    const interaction = wormholeContract.methods.publish_message_in_private(
      publishNonce,
      payloads,
      publishMessageFee,
      publishConsistency,
      ownerAddress,
      tokenNonceForTestTx
    );
    
    console.log('🔄 Sending private message transaction...');
    const tx = await interaction.send({ 
      authWitnesses: [wormholeAuthWit],
      fee: { paymentMethod } 
    }).wait();
    
    console.log(`✅ Private message published successfully on TESTNET: ${tx.txHash}`);
    
    res.json({
      success: true,
      network: 'testnet',
      txHash: tx.txHash,
      wormholeContract: CONTRACT_ADDRESS,
      tokenContract: TOKEN_ADDRESS,
      message: `Private message "${message}" published successfully on Aztec testnet`,
      nonce: publishNonce,
      consistency: publishConsistency,
      messageFee: publishMessageFee.toString(),
      tokenNonce: tokenNonceForTestTx.toString(),
      processedAt: new Date().toISOString()
    });
    
  } catch (error) {
    console.error('❌ Private message publishing failed on TESTNET:', error.message);
    console.error('❌ Full error:', error);
    
    // Handle nonce-related errors
    const recoveryAttempted = handleNonceError(error);
    
    res.status(500).json({
      success: false,
      network: 'testnet',
      error: error.message,
      recoveryAttempted: recoveryAttempted,
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
    console.log('  GET  /nonce-status - Check nonce synchronization status');
    console.log('  POST /recover-nonce - Recover nonce from file');
    console.log('  POST /verify - Verify VAA on testnet');
    console.log('  POST /test   - Test with Jorge\'s real Arbitrum Sepolia VAA');
    console.log('  POST /publish - Publish private message (production)');
    console.log('  POST /test-publish - Test private message publishing');
  });
}).catch(error => {
  console.error('❌ Failed to start testnet service:', error);
  console.log('\n📝 Required environment variables:');
  console.log('  PRIVATE_KEY=your_testnet_private_key');
  console.log('  CONTRACT_ADDRESS=your_deployed_contract_address');
  process.exit(1);
});