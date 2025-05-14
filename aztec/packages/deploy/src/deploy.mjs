// src/deploy.mjs
import { getInitialTestAccountsWallets } from '@aztec/accounts/testing';
import { Contract, createPXEClient, loadContractArtifact, waitForPXE } from '@aztec/aztec.js';
import { ExtendedPublicLog } from '@aztec/stdlib/logs';
import WormholeJson from "../../../contracts/target/wormhole_contracts-Wormhole.json" assert { type: "json" };
import TokenJson from "../../../contracts/target/token_contract-Token.json" assert { type: "json" };

import { writeFileSync } from 'fs';

const WormholeJsonContractArtifact = loadContractArtifact(WormholeJson);
const TokenJsonContractArtifact = loadContractArtifact(TokenJson);

const { PXE_URL = 'http://localhost:8080' } = process.env;

// Call `aztec-nargo compile` to compile the contract
// Call `aztec codegen ./src -o src/artifacts/` to generate the contract artifacts

// Run first ``` aztec start --sandbox ```
// then run this script with ``` node deploy.mjs ```
async function main() {
  const pxe = createPXEClient(PXE_URL);
  await waitForPXE(pxe);

  console.log(`Connected to PXE at ${PXE_URL}`);

  const [ownerWallet, receiverWallet] = await getInitialTestAccountsWallets(pxe);
  const ownerAddress = ownerWallet.getAddress();

  console.log(`Owner address: ${ownerAddress}`);
  console.log(`Receiver address: ${receiverWallet.getAddress()}`);

  let guardians = [];

  for (let i = 0; i < 19; i++) {
    guardians[i] = []; // Initialize each subarray
    for (let j = 0; j < 20; j++) {
      guardians[i][j] = j + 1;
    }
  }

  // TODO deploy token contract
  const token = await Contract.deploy(ownerWallet, TokenJsonContractArtifact, ['PrivateToken', 'PT', 1, ownerWallet.getAddress()], 'constructor_with_minter',)
    .send()
    .deployed();
  console.log(`Token deployed at ${token.address.toString()}`);

  console.log(`Calling mint_to_public on token contract...`);
  const token_contract = await Contract.at(token.address, TokenJsonContractArtifact, ownerWallet);
  const tx_mint = await token_contract.methods.mint_to_public(ownerWallet.getAddress(), 1000).send().wait();
  console.log(`Minted tokens`);

  // Test parameters 
  // TODO: replace with real values
  let wormhole_init_params = [
    // Provider
    1,1,
    // 1,1,'0x254cd5788032e1cab39f51d63adbac4bf73e97c9b309b692ffb568903be9998b', 2,
    // guardians
    // guardians,
    // wormhole owner account
    receiverWallet.getAddress(),
    // token address
    token.address,
  ];

  const wormhole = await Contract.deploy(ownerWallet, WormholeJsonContractArtifact, wormhole_init_params)
    .send()
    .deployed();

  console.log(`Token deployed at ${wormhole.address.toString()}`);

  const addresses = { wormhole: wormhole.address.toString() };
  writeFileSync('addresses.json', JSON.stringify(addresses, null, 2));

  const contract = await Contract.at(wormhole.address, WormholeJsonContractArtifact, ownerWallet);

  // TODO: set initial guardians
  // const tx_guardians = await contract.methods.set_guardians(guardians).send().wait();

  // The message to convert
  let message = "Hello I am stavros vlach";

  // Using TextEncoder (modern approach)
  let encoder = new TextEncoder();
  let bytes = encoder.encode(message);
  
  // TODO: replace with real values
  let payload = [];
  for (let i = 0; i < 8; i++) {
    payload.push(bytes);
  }

  console.log(`Sending message: "${message}"`);

  console.log(`Calling set authwit on wormhole contract...`);

  const action = await token_contract.withWallet(ownerWallet).methods.mint_to_public(
    ownerWallet.getAddress(), 
    receiverWallet.getAddress(), 
    1000,
    10
  )

  const set_authwit = await ownerWallet.setPublicAuthWit(
    { caller: ownerWallet.getAddress(), action},
    true
  );

  console.log(`Calling publish_message on wormhole contract...`);

  const _tx = contract.methods.publish_message(100,payload, 2);


  await set_authwit.send().wait();

  const sampleLogFilter = {
    txHash: '0x100ebe8cfa848587397b272a40426223004c5ee3838d22652c33e10c7fe7d1f7',
    fromBlock: 160,
    toBlock: 190,
    contractAddress: '0x081a143b80470311c64f8fd1b67a074e2aa312bf5e22e6ebe0b17c5b3b44470b'
  };

  console.log(_tx);

  const logs = await pxe.getPublicLogs(sampleLogFilter);

  console.log(logs.logs[0]);

  const fromBlock = await pxe.getBlockNumber();
  const logFilter = {
    fromBlock,
    toBlock: fromBlock + 1,
  };
  const publicLogs = (await pxe.getPublicLogs(logFilter)).logs;

  console.log(publicLogs);
}

main().catch((err) => {
  console.error(`Error in deployment script: ${err}`);
  process.exit(1);
});