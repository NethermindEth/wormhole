# Scripts

Utility scripts used while working with the Aztec <-> Wormhole demos. The
commands assume you have a PXE reachable (either the devnet endpoint or a local
sandbox) and that the same Schnorr account/contract artifacts as the rest of the
repo are available.

> **TODO**: several scripts still hard-code assumptions from the older Aztec
> sandbox tutorials. They need to be updated to the current devnet version of
> Aztec once those flows are exercised again.

## `test-send-message.mjs`

Interactive sender that can publish the Wormhole message from your Schnorr
account either privately or publicly.

```bash
# Private message (default)
node scripts/test-send-message.mjs \
  --message "Hello Wormhole" \
  --receiver 0x...          # optional

# Public message
node scripts/test-send-message.mjs --public \
  --message "Hello Wormhole" \
  --receiver 0x...          # optional, defaults to sender
  --fee 0                   # optional token-based fee (defaults to 0)
  --consistency 1           # optional consistency level (defaults to 1)
```

The script expects the Schnorr account referenced by `PRIVATE_KEY`/`SALT` in
your `.env` to already be deployed. Contract addresses are sourced from
`contracts/addresses.json` (shared by all scripts).

## Other scripts

- `deploy.mjs` – legacy end-to-end deployer for the Wormhole contract against a
  local sandbox. Pending update to the latest devnet API.
- `deploy_token.mjs` – helper that deploys the ProverToken contract and writes
  `token_address.json`. Pending update to the latest devnet API.
- `register-contract.mjs` – quick helper to register an already deployed
  Wormhole instance with PXE.
