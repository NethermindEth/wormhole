// Minimal Wormhole message sender for Aztec devnet 3.0.0
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
import { SchnorrAccountContract } from '@aztec/accounts/schnorr';
import { deriveSigningKey } from '@aztec/stdlib/keys';
import { createPXE, getPXEConfig } from '@aztec/pxe/server';
import { createStore } from '@aztec/kv-store/lmdb';
import { SPONSORED_FPC_SALT } from '@aztec/constants';
import { SponsoredFPCContract } from '@aztec/noir-contracts.js/SponsoredFPC';

import WormholeJson from '../contracts/target/wormhole_contracts-Wormhole.json' with { type: 'json' };

dotenv.config();

const {
  NODE_URL = 'https://devnet.aztec-labs.com',
  PRIVATE_KEY,
  SALT = '0x0000000000000000000000000000000000000000000000000000000000000000',
  MESSAGE = 'Hello Wormhole from Aztec devnet!',
  MODE = 'private',
  RECEIVER,
  FEE_TOKENS = '0',
  CONSISTENCY_LEVEL = '1',
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

function parseArgs() {
  const args = process.argv.slice(2);
  const parsed = {};
  for (const arg of args) {
    if (arg === '--public') {
      parsed.mode = 'public';
    } else if (arg === '--private') {
      parsed.mode = 'private';
    } else if (arg.startsWith('--message=')) {
      parsed.message = arg.slice('--message='.length);
    } else if (arg.startsWith('--receiver=')) {
      parsed.receiver = arg.slice('--receiver='.length);
    } else if (arg.startsWith('--fee=')) {
      parsed.feeTokens = arg.slice('--fee='.length);
    } else if (arg.startsWith('--consistency=')) {
      parsed.consistencyLevel = arg.slice('--consistency='.length);
    }
  }
  return parsed;
}

function loadAddresses() {
  const addressesPath = join(__dirname, '..', 'contracts', 'addresses.json');
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

  console.log('⏳ Waiting for PXE to sync account notes...');
  await new Promise(resolve => setTimeout(resolve, 3000));

  const account = await accountManager.getAccount();
  return new PXEWallet(account, pxe, nodeClient);
}

async function sendPrivate({ contract, wallet, paymentMethod, payloads }) {
  const senderAddress = wallet.getAddress();

  console.log('🛠️  Preparing private message transaction...');
  const tx = await contract.methods
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
      fee: { paymentMethod },
    })
    .wait();

  console.log(`✅ Private message published! Tx hash: ${tx.txHash}`);
}

async function sendPublic({ contract, wallet, paymentMethod, payloads, options }) {
  const senderAddress = wallet.getAddress();
  const receiver = options.receiver ?? senderAddress;
  const feeAmount = BigInt(options.feeTokens ?? '0');
  const consistencyLevel = Number(options.consistencyLevel ?? '1');
  const nonce = 0n;

  console.log('🛠️  Preparing public message transaction...');
  const tx = await contract.methods
    .publish_message_in_public(
      1,
      payloads,
      feeAmount,
      consistencyLevel,
      receiver,
      nonce,
    )
    .send({
      from: senderAddress,
      fee: { paymentMethod },
    })
    .wait();

  console.log(`✅ Public message published! Tx hash: ${tx.txHash}`);
}

async function main() {
  const cliOptions = parseArgs();
  const mode = (cliOptions.mode ?? MODE).toLowerCase();
  const message = cliOptions.message ?? MESSAGE;
  const receiverOverride = cliOptions.receiver ?? RECEIVER;
  const feeTokens = cliOptions.feeTokens ?? FEE_TOKENS;
  const consistencyLevel = cliOptions.consistencyLevel ?? CONSISTENCY_LEVEL;

  if (!['private', 'public'].includes(mode)) {
    throw new Error(`Unsupported MODE "${mode}". Use "private" or "public".`);
  }

  console.log(`🔄 Connecting to Aztec devnet (mode=${mode})...`);

  const nodeClient = createAztecNodeClient(NODE_URL);
  const store = await createStore('pxe', {
    dataDirectory: join(__dirname, '..', 'store'),
    dataStoreMapSizeKB: 1e6,
  });
  const config = getPXEConfig();
  const pxe = await createPXE(nodeClient, config, { store });
  console.log(`✅ Connected PXE to Aztec node`);

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

  const payloads = buildPayloads(message);

  if (mode === 'public') {
    await sendPublic({
      contract: wormholeContract,
      wallet,
      paymentMethod,
      payloads,
      options: {
        receiver: receiverOverride ? AztecAddress.fromString(receiverOverride) : undefined,
        feeTokens,
        consistencyLevel,
      },
    });
  } else {
    await sendPrivate({ contract: wormholeContract, wallet, paymentMethod, payloads });
  }
}

main().catch((err) => {
  console.error('❌ Error while sending message:', err);
  process.exit(1);
});