// Minimal private Wormhole message sender for Aztec devnet 3.0.0
import dotenv from 'dotenv';
import { readFileSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';

import { AztecAddress } from '@aztec/aztec.js/addresses';
import { Fr } from '@aztec/aztec.js/fields';
import { Contract, getContractInstanceFromInstantiationParams } from '@aztec/aztec.js/contracts';
import { loadContractArtifact } from '@aztec/aztec.js/abi';
import { createAztecNodeClient } from '@aztec/aztec.js/node';
import { SponsoredFeePaymentMethod } from '@aztec/aztec.js/fee';
import { AccountManager, BaseWallet } from '@aztec/aztec.js/wallet';
import { SchnorrAccountContract, getSchnorrAccountContractAddress } from '@aztec/accounts/schnorr';
import { deriveSigningKey } from '@aztec/stdlib/keys';
import { createPXE, getPXEConfig } from '@aztec/pxe/server';
import { createStore } from '@aztec/kv-store/lmdb';
import { SPONSORED_FPC_SALT } from '@aztec/constants';
import { SponsoredFPCContract } from '@aztec/noir-contracts.js/SponsoredFPC';

import WormholeJson from '../../../contracts/target/wormhole_contracts-Wormhole.json' with { type: 'json' };

dotenv.config();

const {
  NODE_URL = 'https://devnet.aztec-labs.com',
  PRIVATE_KEY,
  SALT = '0x0000000000000000000000000000000000000000000000000000000000000000',
} = process.env;

const WormholeContractArtifact = loadContractArtifact(WormholeJson);
const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

class PXEWallet extends BaseWallet {
  constructor(account, pxeInstance, aztecNode) {
    super(pxeInstance, aztecNode);
    this.account = account;
  }

  getAddress() {
    return this.account.getAddress();
  }

  async getAccounts() {
    const registered = await this.pxe.getRegisteredAccounts();
    return registered.map(({ address }) => ({ item: address, alias: '' }));
  }

  async getAccountFromAddress(address) {
    if (address.equals(this.account.getAddress())) {
      return this.account;
    }
    throw new Error(`Account ${address.toString()} not loaded in wallet`);
  }
}

function loadAddresses() {
  const addressesPath = join(__dirname, 'addresses.json');
  try {
    const raw = readFileSync(addressesPath, 'utf8');
    const parsed = JSON.parse(raw);

    if (!parsed?.wormhole) {
      throw new Error('Missing wormhole address in addresses.json');
    }

    return parsed;
  } catch (error) {
    throw new Error(`Failed to read addresses.json: ${error.message}`);
  }
}

function buildPayloads(message) {
  const encoder = new TextEncoder();
  const messageBytes = encoder.encode(message);
  const maxLength = 31;

  const firstChunk = new Array(maxLength).fill(0);
  for (let i = 0; i < Math.min(messageBytes.length, maxLength); i += 1) {
    firstChunk[i] = messageBytes[i];
  }

  const payloads = Array.from({ length: 8 }, () => new Array(maxLength).fill(0));
  payloads[0] = firstChunk;

  return payloads;
}

async function setupWallet(pxe, nodeClient) {
  if (!PRIVATE_KEY) {
    throw new Error('PRIVATE_KEY environment variable is required');
  }

  const secretKey = Fr.fromString(PRIVATE_KEY);
  const salt = Fr.fromString(SALT);
  const signingKey = deriveSigningKey(secretKey);

  const accountContract = new SchnorrAccountContract(signingKey);
  const accountManager = await AccountManager.create({
    getChainInfo: async () => {
      const { l1ChainId, rollupVersion } = await nodeClient.getNodeInfo();
      return {
        chainId: new Fr(l1ChainId),
        version: new Fr(rollupVersion),
      };
    },
    registerContract: async (instanceData, artifact) =>
      pxe.registerContract({ instance: instanceData, artifact }),
  }, secretKey, accountContract, salt);

  const completeAddress = await accountManager.getCompleteAddress();
  const accountInstance = accountManager.getInstance();
  const accountArtifact = await accountContract.getContractArtifact();

  await pxe.registerContract({ instance: accountInstance, artifact: accountArtifact });
  await pxe.registerAccount(secretKey, completeAddress.partialAddress);

  // Wait for PXE to sync account notes
  console.log('⏳ Waiting for PXE to sync account notes...');
  await new Promise(resolve => setTimeout(resolve, 3000));

  const account = await accountManager.getAccount();
  return new PXEWallet(account, pxe, nodeClient);
}

async function main() {
  console.log('🔄 Connecting to Aztec devnet...');
  
  const nodeClient = createAztecNodeClient(NODE_URL);
  const store = await createStore('pxe', {
    dataDirectory: join(__dirname, '..', 'store'),
    dataStoreMapSizeKB: 1e6,
  });
  const config = getPXEConfig();
  const pxe = await createPXE(nodeClient, config, { store });
  console.log(`✅ Connected PXE to Aztec node`);

  // Setup sponsored fee payment method
  console.log('🔄 Setting up sponsored fee payment...');
  const sponsoredFPC = await getContractInstanceFromInstantiationParams(SponsoredFPCContract.artifact, {
    salt: new Fr(SPONSORED_FPC_SALT),
  });
  await pxe.registerContract({
    instance: sponsoredFPC,
    artifact: SponsoredFPCContract.artifact,
  });
  const paymentMethod = new SponsoredFeePaymentMethod(sponsoredFPC.address);
  console.log(`✅ Sponsored fee payment method configured`);

  const wallet = await setupWallet(pxe, nodeClient);
  const senderAddress = wallet.getAddress();
  console.log(`👛 Using account ${senderAddress.toString()}`);

  const addresses = loadAddresses();
  const wormholeAddress = AztecAddress.fromString(addresses.wormhole);
  console.log(`🔗 Target Wormhole core contract: ${wormholeAddress.toString()}`);

  const wormholeInstance = await nodeClient.getContract(wormholeAddress);
  if (!wormholeInstance) {
    throw new Error(`No contract instance found at ${wormholeAddress.toString()}`);
  }

  await pxe.registerContract({ instance: wormholeInstance, artifact: WormholeContractArtifact });
  const wormholeContract = new Contract(wormholeInstance, WormholeContractArtifact, wallet);

  const message = 'Hello Wormhole from Aztec devnet!';
  const payloads = buildPayloads(message);

  console.log('🛠️  Prepared payloads, publishing private message with zero fee...');

  const tx = await wormholeContract.methods
    .publish_message_in_private(
      1,
      payloads,
      0n,
      1,
      senderAddress,
      new Fr(0n),
    )
    .send({ 
      from: senderAddress,
      fee: { paymentMethod } 
    })
    .wait();

  console.log(`✅ Private message published! Tx hash: ${tx.txHash}`);
}

main().catch((err) => {
  console.error('❌ Error while sending private message:', err);
  process.exit(1);
});