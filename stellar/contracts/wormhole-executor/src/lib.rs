#![no_std]

use soroban_sdk::{
    Address, Bytes, BytesN, Env, String, contract, contractevent, contractimpl, contracttype, token,
};
use wormhole_soroban_client::{
    ExecutorError, ExecutorInterface, NATIVE_TOKEN_ADDRESS, SignedQuote,
};

#[cfg(test)]
mod tests;

const EXECUTOR_VERSION: &str = "Executor-0.0.1";

#[contracttype]
#[derive(Clone)]
pub enum DataKey {
    ChainId,
}

#[contractevent(topics = ["Executor", "RequestForExecution"])]
#[derive(Clone)]
pub struct RequestForExecution {
    pub quoter: Address,
    pub amt_paid: i128,
    pub dst_chain: u32,
    pub dst_addr_wa32: BytesN<32>,
    pub refund: Address,
    pub signed_quote: SignedQuote,
    pub request: Bytes,
    pub relay_instructions: Bytes,
}

fn get_native_token_address(env: &Env) -> Address {
    Address::from_string(&String::from_str(env, NATIVE_TOKEN_ADDRESS))
}

#[contract]
pub struct Executor;

#[contractimpl]
impl ExecutorTrait for Executor {
    fn __constructor(env: Env, chain_id: u32) {
        env.storage().instance().set(&DataKey::ChainId, &chain_id);
    }

    fn chain_id(env: Env) -> u32 {
        env.storage().instance().get(&DataKey::ChainId).unwrap()
    }

    fn executor_version(env: Env) -> String {
        String::from_str(&env, EXECUTOR_VERSION)
    }

    #[allow(clippy::too_many_arguments)]
    fn request_execution(
        env: Env,
        dst_chain: u32,
        dst_addr_wa32: BytesN<32>,
        refund: Address,
        payer: Address,
        amount: i128,
        signed_quote: SignedQuote,
        request: Bytes,
        relay_instructions: Bytes,
    ) -> Result<(), ExecError> {
        if amount < 0 {
            return Err(ExecError::InvalidAmount);
        }

        let this_chain = Self::chain_id(env.clone());

        if signed_quote.src_chain != this_chain {
            return Err(ExecError::QuoteSrcChainMismatch);
        }
        if signed_quote.dst_chain != dst_chain {
            return Err(ExecError::QuoteDstChainMismatch);
        }
        if signed_quote.expiry <= env.ledger().timestamp() {
            return Err(ExecError::QuoteExpired);
        }

        payer.require_auth();

        let event = RequestForExecution {
            quoter: signed_quote.quoter.clone(),
            amt_paid: amount,
            dst_chain,
            dst_addr_wa32,
            refund,
            signed_quote,
            request,
            relay_instructions,
        };

        let native_token = get_native_token_address(&env);
        let token_client = token::TokenClient::new(&env, &native_token);
        token_client.transfer(&payer, &event.signed_quote.payee, &amount);
        event.publish(&env);

        Ok(())
    }
}
