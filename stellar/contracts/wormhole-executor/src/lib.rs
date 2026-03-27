#![no_std]

use soroban_sdk::{
    Address, Bytes, BytesN, Env, Symbol, contract, contracterror, contractimpl, contracttype,
};
use wormhole_soroban_client::{BytesReader, ConsistencyLevel, WormholeClient, WormholeError};

const SIGNED_QUOTE_PREFIX: &[u8; 4] = b"EQ01";
const REQ_EXEC_PREFIX: &[u8; 4] = b"ERV1";

#[contracterror]
#[derive(Copy, Clone, Debug, Eq, PartialEq)]
pub enum ExecError {
    NotInitialized = 1,
    InvalidQuotePrefix = 10,
    InvalidRequestPrefix = 11,
    QuoteExpired = 12,
    QuoteChainMismatch = 13,
    RequestDstMismatch = 14,
    EmptyQuote = 15,
    EmptyRequest = 16,
    BadLengths = 17,
}

#[contracttype]
#[derive(Clone)]
pub enum DataKey {
    ChainId,
    CoreAddress,
}

#[contracttype]
#[derive(Clone)]
pub struct ExecutionRequested {
    pub quoter_wa32: Option<BytesN<32>>,
    pub payee: Address,

    pub dst_chain: u32,
    pub dst_addr_wa32: BytesN<32>,

    pub refund: Address,

    pub fee_token: Option<Address>,
    pub fee_amount: Option<i128>,

    pub signed_quote: Bytes,
    pub request: Bytes,

    pub relay_instructions: Option<Bytes>,
}

fn map_reader_err(_err: WormholeError) -> ExecError {
    ExecError::BadLengths
}

struct ParsedQuote {
    quoter_wa32: BytesN<32>,
    _payee_wa32: BytesN<32>,
    src_chain: u32,
    dst_chain: u32,
    expiry: u64,
}

fn parse_signed_quote_min(quote: &Bytes) -> Result<ParsedQuote, ExecError> {
    let mut reader = BytesReader::new(quote);

    let prefix = reader.read_bytes_n::<4>().map_err(map_reader_err)?;
    if prefix.to_array() != *SIGNED_QUOTE_PREFIX {
        return Err(ExecError::InvalidQuotePrefix);
    }

    let quoter_wa32 = reader.read_bytes_n::<32>().map_err(map_reader_err)?;
    let payee_wa32 = reader.read_bytes_n::<32>().map_err(map_reader_err)?;
    let src_chain = u32::from(reader.read_u16_be().map_err(map_reader_err)?);
    let dst_chain = u32::from(reader.read_u16_be().map_err(map_reader_err)?);
    let expiry = reader.read_u64_be().map_err(map_reader_err)?;

    Ok(ParsedQuote {
        quoter_wa32,
        _payee_wa32: payee_wa32,
        src_chain,
        dst_chain,
        expiry,
    })
}

fn parse_request_min(req: &Bytes) -> Result<(), ExecError> {
    let mut reader = BytesReader::new(req);
    let prefix = reader.read_bytes_n::<4>().map_err(map_reader_err)?;
    if prefix.to_array() != *REQ_EXEC_PREFIX {
        return Err(ExecError::InvalidRequestPrefix);
    }
    Ok(())
}

pub trait ExecutorTrait {
    fn __constructor(env: Env, chain_id: u32, core_address: Address);

    fn chain_id(env: Env) -> u32;
    #[allow(clippy::too_many_arguments)]
    fn request_execution(
        env: Env,
        dst_chain: u32,
        dst_addr_wa32: BytesN<32>,
        refund: Address,
        payee: Address,
        // Optional fee fields (log-only in v1)
        fee_token: Option<Address>,
        fee_amount: Option<i128>,
        signed_quote: Bytes,
        request: Bytes,
        relay_instructions: Option<Bytes>,
    );
}

#[contract]
pub struct Executor;

#[contractimpl]
impl ExecutorTrait for Executor {
    fn __constructor(env: Env, chain_id: u32, core_address: Address) {
        let store = env.storage().instance();
        store.set(&DataKey::ChainId, &chain_id);
        store.set(&DataKey::CoreAddress, &core_address);
    }

    fn chain_id(env: Env) -> u32 {
        let store = env.storage().instance();
        if !store.has(&DataKey::ChainId) {
            soroban_sdk::panic_with_error!(&env, ExecError::NotInitialized);
        }
        store.get::<_, u32>(&DataKey::ChainId).unwrap()
    }

    fn request_execution(
        env: Env,
        dst_chain: u32,
        dst_addr_wa32: BytesN<32>,
        refund: Address,
        payee: Address,
        fee_token: Option<Address>,
        fee_amount: Option<i128>,
        signed_quote: Bytes,
        request: Bytes,
        relay_instructions: Option<Bytes>,
    ) {
        if signed_quote.len() == 0 {
            soroban_sdk::panic_with_error!(&env, ExecError::EmptyQuote);
        }
        if request.len() == 0 {
            soroban_sdk::panic_with_error!(&env, ExecError::EmptyRequest);
        }

        let this_chain = Self::chain_id(env.clone());

        let pq = match parse_signed_quote_min(&signed_quote) {
            Ok(p) => p,
            Err(e) => soroban_sdk::panic_with_error!(&env, e),
        };

        if pq.src_chain != this_chain {
            soroban_sdk::panic_with_error!(&env, ExecError::QuoteChainMismatch);
        }
        if pq.dst_chain != dst_chain {
            soroban_sdk::panic_with_error!(&env, ExecError::RequestDstMismatch);
        }

        let now = env.ledger().timestamp();
        if pq.expiry < now {
            soroban_sdk::panic_with_error!(&env, ExecError::QuoteExpired);
        }

        if let Err(e) = parse_request_min(&request) {
            soroban_sdk::panic_with_error!(&env, e);
        }

        let evt = ExecutionRequested {
            quoter_wa32: Some(pq.quoter_wa32),
            payee,
            dst_chain,
            dst_addr_wa32,
            refund,
            fee_token,
            fee_amount,
            signed_quote,
            request,
            relay_instructions,
        };

        let core_addr = env
            .storage()
            .instance()
            .get::<_, Address>(&DataKey::CoreAddress)
            .unwrap();
        let client = WormholeClient::new(&env, &core_addr);

        let mut payload = Bytes::new(&env);
        payload.append(&evt.signed_quote);
        payload.append(&evt.request);

        let nonce = env.ledger().sequence() * 1000 + env.ledger().timestamp() as u32; // basic nonce generation

        client.post_message(
            &env.current_contract_address(),
            &nonce,
            &payload,
            &ConsistencyLevel::Confirmed,
        );

        env.events().publish(
            (
                Symbol::new(&env, "Executor"),
                Symbol::new(&env, "ExecutionRequested"),
            ),
            evt,
        );
    }
}

#[cfg(test)]
mod test {
    use super::*;
    use soroban_sdk::{
        Address, Bytes, BytesN, Env, Symbol, TryFromVal, Val, Vec, contract, contractimpl,
        testutils::{Events, Ledger},
    };

    #[contract]
    struct Dummy;
    #[contractimpl]
    impl Dummy {
        pub fn ping(_env: Env) {}
    }

    #[contract]
    struct MockCore;
    #[contractimpl]
    impl MockCore {
        pub fn post_message(
            _env: Env,
            _emitter: Address,
            _nonce: u32,
            _payload: Bytes,
            _consistency_level: u32,
        ) -> u64 {
            0
        }
    }

    // ---------------------- Helpers ----------------------

    fn register_executor(env: &Env, chain_id: u32) -> (ExecutorClient, Address) {
        let core_addr = env.register(MockCore, ());
        let exec_addr = env.register(Executor, (&chain_id, &core_addr));
        let client = ExecutorClient::new(env, &exec_addr);
        (client, exec_addr)
    }

    fn mk_signed_quote(env: &Env, src_chain: u16, dst_chain: u16, expiry_delta_secs: i64) -> Bytes {
        let mut q = Bytes::new(env);
        q.append(&Bytes::from_array(env, SIGNED_QUOTE_PREFIX)); // "EQ01"
        q.append(&Bytes::from_array(env, &[0u8; 32])); // quoter_wa32
        q.append(&Bytes::from_array(env, &[1u8; 32])); // payee_wa32 (dummy)
        q.append(&Bytes::from_array(env, &src_chain.to_be_bytes()));
        q.append(&Bytes::from_array(env, &dst_chain.to_be_bytes()));
        let expiry = (env.ledger().timestamp() as i64 + expiry_delta_secs) as u64;
        q.append(&Bytes::from_array(env, &expiry.to_be_bytes()));
        q
    }

    fn mk_badprefix_signed_quote(env: &Env) -> Bytes {
        let mut q = Bytes::new(env);
        q.append(&Bytes::from_array(env, b"BAD!"));
        q.append(&Bytes::from_array(env, &[0u8; 32]));
        q.append(&Bytes::from_array(env, &[1u8; 32]));
        q.append(&Bytes::from_array(env, &(1u16).to_be_bytes()));
        q.append(&Bytes::from_array(env, &(2u16).to_be_bytes()));
        let expiry = env.ledger().timestamp() + 600;
        q.append(&Bytes::from_array(env, &expiry.to_be_bytes()));
        q
    }

    fn mk_request(env: &Env) -> Bytes {
        let mut r = Bytes::new(env);
        r.append(&Bytes::from_array(env, REQ_EXEC_PREFIX)); // "ERV1"
        r.append(&Bytes::from_array(env, &[0u8; 36])); // dummy payload
        r
    }

    fn mk_badprefix_request(env: &Env) -> Bytes {
        let mut r = Bytes::new(env);
        r.append(&Bytes::from_array(env, b"BAD!"));
        r.append(&Bytes::from_array(env, &[0u8; 36]));
        r
    }

    /// Return last event's topics and decoded data using `soroban_sdk::Vec`
    /// APIs.
    fn decode_last_evt<T: TryFromVal<Env, Val>>(env: &Env) -> (Vec<Val>, T) {
        let entries: Vec<(Address, Vec<Val>, Val)> = env.events().all();
        let len = entries.len();
        assert!(len > 0, "no events were emitted");
        let last_idx = len - 1;
        let (_addr, topics, data) = entries.get(last_idx).expect("index out of bounds");
        let parsed: T = T::try_from_val(env, &data).expect("failed to decode event data");
        (topics, parsed)
    }

    /// Convert topics (Vec<Val>) to Vec<Symbol> without std::iter::collect().
    fn topic_symbols(env: &Env, topics: &Vec<Val>) -> Vec<Symbol> {
        let mut out: Vec<Symbol> = Vec::new(env);
        let n = topics.len();
        let mut i = 0u32;
        while i < n {
            let v = topics.get(i).unwrap();
            if let Ok(sym) = Symbol::try_from_val(env, &v) {
                out.push_back(sym);
            }
            i += 1;
        }
        out
    }

    // ---------------------- Tests ----------------------

    #[test]
    fn init_and_chain_id_roundtrip() {
        let env = Env::default();
        env.mock_all_auths();

        let (client, _addr) = register_executor(&env, 1234u32);
        let got = client.chain_id();
        assert_eq!(got, 1234u32);
    }

    #[test]
    fn request_happy_path_emits_event_with_fields() {
        let env = Env::default();
        env.mock_all_auths();

        let src_chain = 1234u16;
        let dst_chain = 4321u16;
        let (client, _addr) = register_executor(&env, src_chain as u32);

        let signed_quote = mk_signed_quote(&env, src_chain, dst_chain, 600);
        let request = mk_request(&env);

        let payee = env.register(Dummy, ());
        let refund = env.register(Dummy, ());
        let dst_addr_wa32 = BytesN::<32>::from_array(&env, &[9u8; 32]);

        client.request_execution(
            &(dst_chain as u32),
            &dst_addr_wa32,
            &refund,
            &payee,
            &None,
            &None,
            &signed_quote,
            &request,
            &None,
        );

        let (topics, evt): (Vec<Val>, ExecutionRequested) = decode_last_evt(&env);
        let syms = topic_symbols(&env, &topics);

        // Expect topics contain "Executor" and "ExecutionRequested"
        let mut found_exec = false;
        let mut found_er = false;
        let n = syms.len();
        let mut i = 0;
        while i < n {
            let s = syms.get(i).unwrap();
            if s == Symbol::new(&env, "Executor") {
                found_exec = true;
            }
            if s == Symbol::new(&env, "ExecutionRequested") {
                found_er = true;
            }
            i += 1;
        }
        assert!(
            found_exec && found_er,
            "expected Executor/ExecutionRequested topics"
        );

        assert_eq!(evt.dst_chain, dst_chain as u32);
        assert_eq!(evt.dst_addr_wa32, dst_addr_wa32);
        assert_eq!(evt.payee, payee);
        assert_eq!(evt.refund, refund);
        assert!(evt.quoter_wa32.is_some());
        assert_eq!(evt.fee_token, None);
        assert_eq!(evt.fee_amount, None);
        assert_eq!(evt.signed_quote.len(), signed_quote.len());
        assert_eq!(evt.request.len(), request.len());
    }

    #[test]
    fn multiple_requests_emit_twice_with_different_payloads() {
        let env = Env::default();
        env.mock_all_auths();

        let (client, _addr) = register_executor(&env, 100);
        let payee = env.register(Dummy, ());
        let refund = env.register(Dummy, ());
        let dst_addr_wa32 = BytesN::<32>::from_array(&env, &[8u8; 32]);

        // First request: expect dst_chain = 200
        {
            let q = mk_signed_quote(&env, 100, 200, 600);
            let r = mk_request(&env);
            client.request_execution(
                &200u32,
                &dst_addr_wa32,
                &refund,
                &payee,
                &None,
                &None,
                &q,
                &r,
                &None,
            );
            let (_t, evt): (Vec<Val>, ExecutionRequested) = decode_last_evt(&env);
            assert_eq!(evt.dst_chain, 200u32);
        }

        // Second request: expect dst_chain = 201 (verifies a new latest event)
        {
            let q = mk_signed_quote(&env, 100, 201, 600);
            let r = mk_request(&env);
            client.request_execution(
                &201u32,
                &dst_addr_wa32,
                &refund,
                &payee,
                &None,
                &None,
                &q,
                &r,
                &None,
            );
            let (_t, evt): (Vec<Val>, ExecutionRequested) = decode_last_evt(&env);
            assert_eq!(evt.dst_chain, 201u32);
        }
    }

    // ---------------- Negative-path validations (panic code checked)
    // ----------------

    #[test]
    #[should_panic(expected = "Error(Contract, #10)")] // InvalidQuotePrefix
    fn request_rejects_bad_quote_prefix() {
        let env = Env::default();
        env.mock_all_auths();

        let (client, _addr) = register_executor(&env, 10);

        let signed_quote = mk_badprefix_signed_quote(&env);
        let request = mk_request(&env);

        let payee = env.register(Dummy, ());
        let refund = env.register(Dummy, ());
        let dst_addr_wa32 = BytesN::<32>::from_array(&env, &[1u8; 32]);

        client.request_execution(
            &9999,
            &dst_addr_wa32,
            &refund,
            &payee,
            &None,
            &None,
            &signed_quote,
            &request,
            &None,
        );
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #11)")]
    fn request_rejects_bad_request_prefix() {
        let env = Env::default();
        env.mock_all_auths();

        let src_chain = 20u16;
        let dst_chain = 30u16;
        let (client, _addr) = register_executor(&env, src_chain as u32);

        let signed_quote = mk_signed_quote(&env, src_chain, dst_chain, 600);
        let request = mk_badprefix_request(&env);

        let payee = env.register(Dummy, ());
        let refund = env.register(Dummy, ());
        let dst_addr_wa32 = BytesN::<32>::from_array(&env, &[2u8; 32]);

        client.request_execution(
            &(dst_chain as u32),
            &dst_addr_wa32,
            &refund,
            &payee,
            &None,
            &None,
            &signed_quote,
            &request,
            &None,
        );
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #12)")] // QuoteExpired
    fn request_rejects_expired_quote() {
        let env = Env::default();
        env.mock_all_auths();
        env.ledger().with_mut(|li| li.timestamp = 1000);

        let src_chain = 111u16;
        let dst_chain = 222u16;
        let (client, _addr) = register_executor(&env, src_chain as u32);

        let signed_quote = mk_signed_quote(&env, src_chain, dst_chain, -1); // already expired
        let request = mk_request(&env);

        let payee = env.register(Dummy, ());
        let refund = env.register(Dummy, ());
        let dst_addr_wa32 = BytesN::<32>::from_array(&env, &[3u8; 32]);

        client.request_execution(
            &(dst_chain as u32),
            &dst_addr_wa32,
            &refund,
            &payee,
            &None,
            &None,
            &signed_quote,
            &request,
            &None,
        );
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #13)")] // QuoteChainMismatch (source)
    fn request_rejects_source_chain_mismatch() {
        let env = Env::default();
        env.mock_all_auths();

        let src_chain = 77u16;
        let dst_chain = 88u16;

        // Initialize wormhole-executor with WRONG chain id to trigger mismatch
        let (client, _addr) = register_executor(&env, 9999);

        let signed_quote = mk_signed_quote(&env, src_chain, dst_chain, 600);
        let request = mk_request(&env);

        let payee = env.register(Dummy, ());
        let refund = env.register(Dummy, ());
        let dst_addr_wa32 = BytesN::<32>::from_array(&env, &[4u8; 32]);

        client.request_execution(
            &(dst_chain as u32),
            &dst_addr_wa32,
            &refund,
            &payee,
            &None,
            &None,
            &signed_quote,
            &request,
            &None,
        );
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #14)")]
    fn request_rejects_dst_chain_mismatch() {
        let env = Env::default();
        env.mock_all_auths();

        let src_chain = 55u16;
        let dst_chain = 66u16;
        let (client, _addr) = register_executor(&env, src_chain as u32);

        let signed_quote = mk_signed_quote(&env, src_chain, dst_chain, 600);
        let request = mk_request(&env);

        let payee = env.register(Dummy, ());
        let refund = env.register(Dummy, ());
        let dst_addr_wa32 = BytesN::<32>::from_array(&env, &[5u8; 32]);

        // Intentionally pass the WRONG destination chain id (quote says 66, we pass 99)
        client.request_execution(
            &99u32,
            &dst_addr_wa32,
            &refund,
            &payee,
            &None,
            &None,
            &signed_quote,
            &request,
            &None,
        );
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #15)")]
    fn request_rejects_empty_quote() {
        let env = Env::default();
        env.mock_all_auths();

        let (client, _addr) = register_executor(&env, 1);

        let signed_quote = Bytes::new(&env);
        let request = mk_request(&env);

        let payee = env.register(Dummy, ());
        let refund = env.register(Dummy, ());
        let dst_addr_wa32 = BytesN::<32>::from_array(&env, &[6u8; 32]);

        client.request_execution(
            &2u32,
            &dst_addr_wa32,
            &refund,
            &payee,
            &None,
            &None,
            &signed_quote,
            &request,
            &None,
        );
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #16)")]
    fn request_rejects_empty_request() {
        let env = Env::default();
        env.mock_all_auths();

        let (client, _addr) = register_executor(&env, 1);

        let signed_quote = mk_signed_quote(&env, 1, 2, 600);
        let request = Bytes::new(&env);

        let payee = env.register(Dummy, ());
        let refund = env.register(Dummy, ());
        let dst_addr_wa32 = BytesN::<32>::from_array(&env, &[7u8; 32]);

        client.request_execution(
            &2u32,
            &dst_addr_wa32,
            &refund,
            &payee,
            &None,
            &None,
            &signed_quote,
            &request,
            &None,
        );
    }
}
