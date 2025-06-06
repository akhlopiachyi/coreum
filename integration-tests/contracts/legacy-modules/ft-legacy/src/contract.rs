use coreum_wasm_sdk::assetft::{
    self, BalanceResponse, FrozenBalanceResponse, FrozenBalancesResponse, ParamsResponse, Query,
    TokenResponse, TokensResponse, WhitelistedBalanceResponse, WhitelistedBalancesResponse,
};
use coreum_wasm_sdk::core::{CoreumMsg, CoreumQueries, CoreumResult};
use coreum_wasm_sdk::pagination::PageRequest;
use cosmwasm_std::{coin, entry_point, to_json_binary, Binary, Deps, QueryRequest, StdResult};
use cosmwasm_std::{DepsMut, Env, MessageInfo, Response};
use cw2::set_contract_version;
use cw_ownable::{assert_owner, initialize_owner};

use crate::error::ContractError;
use crate::msg::{ExecuteMsg, InstantiateMsg, QueryMsg};
use crate::state::DENOM;

// version info for migration info
const CONTRACT_NAME: &str = env!("CARGO_PKG_NAME");
const CONTRACT_VERSION: &str = env!("CARGO_PKG_VERSION");

// ********** Instantiate **********

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn instantiate(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    msg: InstantiateMsg,
) -> CoreumResult<ContractError> {
    set_contract_version(deps.storage, CONTRACT_NAME, CONTRACT_VERSION)?;
    initialize_owner(deps.storage, deps.api, Some(info.sender.as_ref()))?;

    let issue_msg = CoreumMsg::AssetFT(assetft::Msg::Issue {
        symbol: msg.symbol,
        subunit: msg.subunit.clone(),
        precision: msg.precision,
        initial_amount: msg.initial_amount,
        description: msg.description,
        features: msg.features,
        burn_rate: msg.burn_rate,
        send_commission_rate: msg.send_commission_rate,
        uri: msg.uri,
        uri_hash: msg.uri_hash,
    });

    let denom = format!("{}-{}", msg.subunit, env.contract.address).to_lowercase();

    DENOM.save(deps.storage, &denom)?;

    Ok(Response::new()
        .add_attribute("owner", info.sender)
        .add_attribute("denom", denom)
        .add_message(issue_msg))
}

// ********** Execute **********

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn execute(
    deps: DepsMut,
    _env: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> CoreumResult<ContractError> {
    match msg {
        ExecuteMsg::Mint { amount, recipient } => mint(deps, info, amount, recipient),
        ExecuteMsg::Burn { amount } => burn(deps, info, amount),
        ExecuteMsg::Freeze { account, amount } => freeze(deps, info, account, amount),
        ExecuteMsg::Unfreeze { account, amount } => unfreeze(deps, info, account, amount),
        ExecuteMsg::SetFrozen { account, amount } => set_frozen(deps, info, account, amount),
        ExecuteMsg::GloballyFreeze {} => globally_freeze(deps, info),
        ExecuteMsg::GloballyUnfreeze {} => globally_unfreeze(deps, info),
        ExecuteMsg::SetWhitelistedLimit { account, amount } => {
            set_whitelisted_limit(deps, info, account, amount)
        }
    }
}

// ********** Transactions **********

fn mint(
    deps: DepsMut,
    info: MessageInfo,
    amount: u128,
    recipient: Option<String>,
) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &info.sender)?;
    let denom = DENOM.load(deps.storage)?;
    let msg = CoreumMsg::AssetFT(assetft::Msg::Mint {
        coin: coin(amount, denom.clone()),
        recipient,
    });

    Ok(Response::new()
        .add_attribute("method", "mint")
        .add_attribute("denom", denom)
        .add_attribute("amount", amount.to_string())
        .add_message(msg))
}

fn burn(deps: DepsMut, info: MessageInfo, amount: u128) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &info.sender)?;
    let denom = DENOM.load(deps.storage)?;

    let msg = CoreumMsg::AssetFT(assetft::Msg::Burn {
        coin: coin(amount, denom.clone()),
    });

    Ok(Response::new()
        .add_attribute("method", "burn")
        .add_attribute("denom", denom)
        .add_attribute("amount", amount.to_string())
        .add_message(msg))
}

fn freeze(
    deps: DepsMut,
    info: MessageInfo,
    account: String,
    amount: u128,
) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &info.sender)?;
    let denom = DENOM.load(deps.storage)?;

    let msg = CoreumMsg::AssetFT(assetft::Msg::Freeze {
        account,
        coin: coin(amount, denom.clone()),
    });

    Ok(Response::new()
        .add_attribute("method", "freeze")
        .add_attribute("denom", denom)
        .add_attribute("amount", amount.to_string())
        .add_message(msg))
}

fn unfreeze(
    deps: DepsMut,
    info: MessageInfo,
    account: String,
    amount: u128,
) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &info.sender)?;
    let denom = DENOM.load(deps.storage)?;

    let msg = CoreumMsg::AssetFT(assetft::Msg::Unfreeze {
        account,
        coin: coin(amount, denom.clone()),
    });

    Ok(Response::new()
        .add_attribute("method", "unfreeze")
        .add_attribute("denom", denom)
        .add_attribute("amount", amount.to_string())
        .add_message(msg))
}

fn set_frozen(
    deps: DepsMut,
    info: MessageInfo,
    account: String,
    amount: u128,
) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &info.sender)?;
    let denom = DENOM.load(deps.storage)?;

    let msg = CoreumMsg::AssetFT(assetft::Msg::SetFrozen {
        account,
        coin: coin(amount, denom.clone()),
    });

    Ok(Response::new()
        .add_attribute("method", "set_frozen")
        .add_attribute("denom", denom)
        .add_attribute("amount", amount.to_string())
        .add_message(msg))
}

fn globally_freeze(deps: DepsMut, info: MessageInfo) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &info.sender)?;
    let denom = DENOM.load(deps.storage)?;

    let msg = CoreumMsg::AssetFT(assetft::Msg::GloballyFreeze {
        denom: denom.clone(),
    });

    Ok(Response::new()
        .add_attribute("method", "globally_freeze")
        .add_attribute("denom", denom)
        .add_message(msg))
}

fn globally_unfreeze(deps: DepsMut, info: MessageInfo) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &info.sender)?;
    let denom = DENOM.load(deps.storage)?;

    let msg = CoreumMsg::AssetFT(assetft::Msg::GloballyUnfreeze {
        denom: denom.clone(),
    });

    Ok(Response::new()
        .add_attribute("method", "globally_unfreeze")
        .add_attribute("denom", denom)
        .add_message(msg))
}

fn set_whitelisted_limit(
    deps: DepsMut,
    info: MessageInfo,
    account: String,
    amount: u128,
) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &info.sender)?;
    let denom = DENOM.load(deps.storage)?;

    let msg = CoreumMsg::AssetFT(assetft::Msg::SetWhitelistedLimit {
        account,
        coin: coin(amount, denom.clone()),
    });

    Ok(Response::new()
        .add_attribute("method", "set_whitelisted_limit")
        .add_attribute("denom", denom)
        .add_attribute("amount", amount.to_string())
        .add_message(msg))
}

// ********** Queries **********
#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(deps: Deps<CoreumQueries>, _env: Env, msg: QueryMsg) -> StdResult<Binary> {
    match msg {
        QueryMsg::Params {} => to_json_binary(&query_params(deps)?),
        QueryMsg::Token {} => to_json_binary(&query_token(deps)?),
        QueryMsg::Tokens { issuer } => to_json_binary(&query_tokens(deps, issuer)?),
        QueryMsg::FrozenBalance { account } => {
            to_json_binary(&query_frozen_balance(deps, account)?)
        }
        QueryMsg::WhitelistedBalance { account } => {
            to_json_binary(&query_whitelisted_balance(deps, account)?)
        }
        QueryMsg::Balance { account } => to_json_binary(&query_balance(deps, account)?),
        QueryMsg::FrozenBalances { account } => {
            to_json_binary(&query_frozen_balances(deps, account)?)
        }
        QueryMsg::WhitelistedBalances { account } => {
            to_json_binary(&query_whitelisted_balances(deps, account)?)
        }
    }
}

fn query_params(deps: Deps<CoreumQueries>) -> StdResult<ParamsResponse> {
    let request = CoreumQueries::AssetFT(Query::Params {}).into();
    let res = deps.querier.query(&request)?;
    Ok(res)
}

fn query_token(deps: Deps<CoreumQueries>) -> StdResult<TokenResponse> {
    let denom = DENOM.load(deps.storage)?;
    let request = CoreumQueries::AssetFT(Query::Token { denom }).into();
    let res = deps.querier.query(&request)?;
    Ok(res)
}

fn query_tokens(deps: Deps<CoreumQueries>, issuer: String) -> StdResult<TokensResponse> {
    let mut pagination = None;
    let mut tokens = vec![];
    let mut res: TokensResponse;
    loop {
        let request = CoreumQueries::AssetFT(Query::Tokens {
            pagination,
            issuer: issuer.clone(),
        })
        .into();
        res = deps.querier.query(&request)?;
        tokens.append(&mut res.tokens);
        if res.pagination.next_key.is_none() {
            break;
        } else {
            pagination = Some(PageRequest {
                key: res.pagination.next_key,
                offset: None,
                limit: None,
                count_total: None,
                reverse: None,
            })
        }
    }
    let res = TokensResponse {
        pagination: res.pagination,
        tokens,
    };
    Ok(res)
}

fn query_balance(deps: Deps<CoreumQueries>, account: String) -> StdResult<BalanceResponse> {
    let denom = DENOM.load(deps.storage)?;
    let request = CoreumQueries::AssetFT(Query::Balance { account, denom }).into();
    let res = deps.querier.query(&request)?;
    Ok(res)
}

fn query_frozen_balance(
    deps: Deps<CoreumQueries>,
    account: String,
) -> StdResult<FrozenBalanceResponse> {
    let denom = DENOM.load(deps.storage)?;
    let request: QueryRequest<CoreumQueries> =
        CoreumQueries::AssetFT(Query::FrozenBalance { denom, account }).into();
    let res = deps.querier.query(&request)?;
    Ok(res)
}

fn query_frozen_balances(
    deps: Deps<CoreumQueries>,
    account: String,
) -> StdResult<FrozenBalancesResponse> {
    let mut pagination = None;
    let mut balances = vec![];
    let mut res: FrozenBalancesResponse;
    loop {
        let request = CoreumQueries::AssetFT(Query::FrozenBalances {
            pagination,
            account: account.clone(),
        })
        .into();
        res = deps.querier.query(&request)?;
        balances.append(&mut res.balances);
        if res.pagination.next_key.is_none() {
            break;
        } else {
            pagination = Some(PageRequest {
                key: res.pagination.next_key,
                offset: None,
                limit: None,
                count_total: None,
                reverse: None,
            })
        }
    }
    let res = FrozenBalancesResponse {
        pagination: res.pagination,
        balances,
    };
    Ok(res)
}

fn query_whitelisted_balance(
    deps: Deps<CoreumQueries>,
    account: String,
) -> StdResult<WhitelistedBalanceResponse> {
    let denom = DENOM.load(deps.storage)?;
    let request: QueryRequest<CoreumQueries> =
        CoreumQueries::AssetFT(Query::WhitelistedBalance { denom, account }).into();
    let res = deps.querier.query(&request)?;
    Ok(res)
}

fn query_whitelisted_balances(
    deps: Deps<CoreumQueries>,
    account: String,
) -> StdResult<WhitelistedBalancesResponse> {
    let mut pagination = None;
    let mut balances = vec![];
    let mut res: WhitelistedBalancesResponse;
    loop {
        let request = CoreumQueries::AssetFT(Query::WhitelistedBalances {
            pagination,
            account: account.clone(),
        })
        .into();
        res = deps.querier.query(&request)?;
        balances.append(&mut res.balances);
        if res.pagination.next_key.is_none() {
            break;
        } else {
            pagination = Some(PageRequest {
                key: res.pagination.next_key,
                offset: None,
                limit: None,
                count_total: None,
                reverse: None,
            })
        }
    }
    let res = WhitelistedBalancesResponse {
        pagination: res.pagination,
        balances,
    };
    Ok(res)
}
