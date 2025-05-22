// src/deploy.mjs
import { getInitialTestAccountsWallets } from '@aztec/accounts/testing';
import { Contract, createPXEClient, loadContractArtifact, waitForPXE } from '@aztec/aztec.js';
import WormholeJson from "../../../contracts/target/wormhole_contracts-Wormhole.json" assert { type: "json" };
import { TokenContract } from '@aztec/noir-contracts.js/Token'; 

import { writeFileSync } from 'fs';

const WormholeJsonContractArtifact = loadContractArtifact(WormholeJson);

const { PXE_URL = 'http://localhost:8090' } = process.env;

// Call `aztec-nargo compile` to compile the contract
// Call `aztec codegen ./src -o src/artifacts/` to generate the contract artifacts

// Run first ``` aztec start --sandbox ```
// then run this script with ``` node deploy.mjs ```


// Following: https://docs.aztec.network/developers/tutorials/codealong/js_tutorials/aztecjs-getting-started#set-up-the-project
async function deployToken(
  adminWallet,
  initialAdminBalance,
) {
  const contract = await TokenContract.deploy(
    adminWallet,
    adminWallet.getAddress(),
    "WormholeToken",
    "WORM",
    18
  )
    .send()
    .deployed();

  if (initialAdminBalance > 0n) {
    // Minter is minting to herself so contract as minter is the same as contract as recipient
    await mintTokensToPublic(
      contract,
      adminWallet,
      adminWallet.getAddress(),
      initialAdminBalance
    );
  }

  return contract;
}

async function mintTokensToPublic(
  token, // TokenContract
  minterWallet, 
  recipient,
  amount
) {
  const tokenAsMinter = await TokenContract.at(token.address, minterWallet);
  await tokenAsMinter.methods
    .mint_to_public(recipient, amount)
    .send()
    .wait();
}

async function main() {
  const pxe = createPXEClient(PXE_URL);
  await waitForPXE(pxe);

  console.log(`Connected to PXE at ${PXE_URL}`);

  const [ownerWallet, receiverWallet] = await getInitialTestAccountsWallets(pxe);
  const ownerAddress = ownerWallet.getAddress();

  console.log(`Owner address: ${ownerAddress}`);
  console.log(`Receiver address: ${receiverWallet.getAddress()}`);

  let token = await deployToken(ownerWallet, 1000n);
  console.log(`Deployed token contract at ${token.address}`)

  // Test parameters 
  let wormhole_init_params = [
    // Provider
    1,1,
    // wormhole owner account
    receiverWallet.getAddress(),
    // token address
    token.address,
  ];

  const wormhole = await Contract.deploy(ownerWallet, WormholeJsonContractArtifact, wormhole_init_params, "init")
    .send()
    .deployed();

  console.log(`Wormhole deployed at ${wormhole.address.toString()}`);

  const addresses = { wormhole: wormhole.address.toString() };
  writeFileSync('addresses.json', JSON.stringify(addresses, null, 2));

  const contract = await Contract.at(wormhole.address, WormholeJsonContractArtifact, ownerWallet);

  // The message to convert
  let message = "Hello I am stavros vlach";

  // Using TextEncoder (modern approach)
  let encoder = new TextEncoder();
  let bytes = encoder.encode(message);

  // Create a padded array (try different sizes - this one is 31 bytes)
  const PAYLOAD_SIZE = 31;
  let paddedBytes = new Array(PAYLOAD_SIZE).fill(0);
  
  // Copy the message bytes into the padded array
  for (let i = 0; i < bytes.length && i < PAYLOAD_SIZE; i++) {
    paddedBytes[i] = bytes[i];
  }
  
  let payload = [];
  for (let i = 0; i < 8; i++) {
    payload.push(paddedBytes);
  }

  console.log(`Calling publish_message with message "${message}" on wormhole contract...`);
  console.log(`Payload: ${payload}`);

  // action to be taken using authwit
  const tokenTransferAction = token.methods.transfer_in_public(
    ownerAddress,
    receiverWallet.getAddress(),
    2n,
    31n
  );  
  // generate authwit to allow for wormhole to send funds to itself on behalf of owner
  const validateActionInteraction = await ownerWallet.setPublicAuthWit(
    {
      caller: wormhole.address,
      action: tokenTransferAction
    },
    true
  );

  await validateActionInteraction.send().wait();

  const _tx = await contract.methods.publish_message(100, payload, 2n,2).send().wait();

  let receiver_balance = await token.methods.balance_of_public(receiverWallet.getAddress()).simulate();

  if (receiver_balance != 2) {
    throw new Error(`Incorrect receiver balance ${receiver_balance}`);
  }

  const sampleLogFilter = {
    fromBlock: 0,
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