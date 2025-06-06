package keeper

import (
	"bytes"
	"context"
	"encoding/json"

	sdkstore "cosmossdk.io/core/store"
	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/store/prefix"
	storetypes "cosmossdk.io/store/types"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmoserrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/CoreumFoundation/coreum/v6/x/asset/ft/types"
	"github.com/CoreumFoundation/coreum/v6/x/wasm"
	cwasmtypes "github.com/CoreumFoundation/coreum/v6/x/wasm/types"
	wibctransfertypes "github.com/CoreumFoundation/coreum/v6/x/wibctransfer/types"
)

// ExtensionInstantiateMsg is the message passed to the extension cosmwasm contract.
// The contract must be able to properly process this message.
//
//nolint:tagliatelle // these will be exposed to rust and must be snake case.
type ExtensionInstantiateMsg struct {
	Denom       string                       `json:"denom"`
	IssuanceMsg wasmtypes.RawContractMessage `json:"issuance_msg"`
}

// Keeper is the asset module keeper.
type Keeper struct {
	cdc                    codec.BinaryCodec
	storeService           sdkstore.KVStoreService
	bankKeeper             types.BankKeeper
	delayKeeper            types.DelayKeeper
	stakingKeeper          types.StakingKeeper
	wasmKeeper             cwasmtypes.WasmKeeper
	wasmPermissionedKeeper types.WasmPermissionedKeeper
	accountKeeper          types.AccountKeeper
	authority              string
}

// NewKeeper creates a new instance of the Keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService sdkstore.KVStoreService,
	bankKeeper types.BankKeeper,
	delayKeeper types.DelayKeeper,
	stakingKeeper types.StakingKeeper,
	wasmKeeper cwasmtypes.WasmKeeper,
	wasmPermissionedKeeper types.WasmPermissionedKeeper,
	accountKeeper types.AccountKeeper,
	authority string,
) Keeper {
	return Keeper{
		cdc:                    cdc,
		storeService:           storeService,
		bankKeeper:             bankKeeper,
		delayKeeper:            delayKeeper,
		stakingKeeper:          stakingKeeper,
		wasmKeeper:             wasmKeeper,
		wasmPermissionedKeeper: wasmPermissionedKeeper,
		accountKeeper:          accountKeeper,
		authority:              authority,
	}
}

// GetParams gets the parameters of the module.
func (k Keeper) GetParams(ctx sdk.Context) (types.Params, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.ParamsKey)
	if err != nil {
		return types.Params{}, err
	}
	var params types.Params
	k.cdc.MustUnmarshal(bz, &params)
	return params, nil
}

// SetParams sets the parameters of the module.
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) error {
	bz, err := k.cdc.Marshal(&params)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Set(types.ParamsKey, bz)
}

// UpdateParams is a governance operation that sets parameters of the module.
func (k Keeper) UpdateParams(ctx sdk.Context, authority string, params types.Params) error {
	if k.authority != authority {
		return sdkerrors.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", k.authority, authority)
	}

	if err := k.checkIssueFeeIsLimitedToCore(ctx, params); err != nil {
		return err
	}

	return k.SetParams(ctx, params)
}

// GetTokens returns all fungible tokens.
func (k Keeper) GetTokens(ctx sdk.Context, pagination *query.PageRequest) ([]types.Token, *query.PageResponse, error) {
	defs, pageResponse, err := k.getDefinitions(ctx, pagination)
	if err != nil {
		return nil, nil, err
	}

	tokens, err := k.getTokensByDefinitions(ctx, defs)
	if err != nil {
		return nil, nil, err
	}

	return tokens, pageResponse, nil
}

// GetIssuerTokens returns fungible tokens issued by the issuer.
func (k Keeper) GetIssuerTokens(
	ctx sdk.Context,
	issuer sdk.AccAddress,
	pagination *query.PageRequest,
) ([]types.Token, *query.PageResponse, error) {
	definitions, pageResponse, err := k.getIssuerDefinitions(ctx, issuer, pagination)
	if err != nil {
		return nil, nil, err
	}

	tokens, err := k.getTokensByDefinitions(ctx, definitions)
	if err != nil {
		return nil, nil, err
	}

	return tokens, pageResponse, nil
}

// IterateAllDefinitions iterates over all token definitions and applies the provided callback.
// If true is returned from the callback, iteration is halted.
func (k Keeper) IterateAllDefinitions(ctx sdk.Context, cb func(types.Definition) (bool, error)) error {
	store := k.storeService.OpenKVStore(ctx)
	iterator := storetypes.KVStorePrefixIterator(runtime.KVStoreAdapter(store), types.TokenKeyPrefix)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var definition types.Definition
		k.cdc.MustUnmarshal(iterator.Value(), &definition)

		stop, err := cb(definition)
		if err != nil {
			return err
		}
		if stop {
			break
		}
	}
	return nil
}

// GetDefinition returns the Definition by the denom.
func (k Keeper) GetDefinition(ctx sdk.Context, denom string) (types.Definition, error) {
	subunit, issuer, err := types.DeconstructDenom(denom)
	if err != nil {
		return types.Definition{}, err
	}
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.CreateTokenKey(issuer, subunit))
	if err != nil {
		return types.Definition{}, err
	}
	if bz == nil {
		return types.Definition{}, sdkerrors.Wrapf(types.ErrTokenNotFound, "denom: %s", denom)
	}
	var definition types.Definition
	if err := k.cdc.Unmarshal(bz, &definition); err != nil {
		return types.Definition{}, sdkerrors.Wrap(err, "error unmarshalling definition")
	}

	return definition, nil
}

// GetToken returns the fungible token by it's denom.
func (k Keeper) GetToken(ctx sdk.Context, denom string) (types.Token, error) {
	def, err := k.GetDefinition(ctx, denom)
	if err != nil {
		return types.Token{}, err
	}

	return k.getTokenFullInfo(ctx, def)
}

// Issue issues new fungible token and returns it's denom.
func (k Keeper) Issue(ctx sdk.Context, settings types.IssueSettings) (string, error) {
	return k.IssueVersioned(ctx, settings, types.CurrentTokenVersion)
}

// IssueVersioned issues new fungible token and sets its version.
// To be used only in unit tests !!!
//
//nolint:funlen // breaking down this function will make it less readable.
func (k Keeper) IssueVersioned(ctx sdk.Context, settings types.IssueSettings, version uint32) (string, error) {
	if err := types.ValidateSubunit(settings.Subunit); err != nil {
		return "", sdkerrors.Wrapf(err, "provided subunit: %s", settings.Subunit)
	}

	if err := types.ValidatePrecision(settings.Precision); err != nil {
		return "", sdkerrors.Wrapf(err, "provided precision: %d", settings.Precision)
	}

	if err := types.ValidateFeatures(settings.Features); err != nil {
		return "", err
	}

	if err := types.ValidateBurnRate(settings.BurnRate); err != nil {
		return "", err
	}
	if err := types.ValidateSendCommissionRate(settings.SendCommissionRate); err != nil {
		return "", err
	}

	if settings.InitialAmount.GT(types.MaxMintableAmount) {
		return "", sdkerrors.Wrapf(types.ErrInvalidInput, "initial amount is greater than maximum allowed")
	}

	err := types.ValidateSymbol(settings.Symbol)
	if err != nil {
		return "", sdkerrors.Wrapf(err, "provided symbol: %s", settings.Symbol)
	}

	denom := types.BuildDenom(settings.Subunit, settings.Issuer)
	if _, found := k.bankKeeper.GetDenomMetaData(ctx, denom); found {
		return "", sdkerrors.Wrapf(
			types.ErrInvalidInput,
			"subunit %s already registered for the address %s",
			settings.Subunit,
			settings.Issuer.String(),
		)
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return "", err
	}
	if params.IssueFee.IsPositive() {
		if err = k.burnIssueFee(ctx, settings, params); err != nil {
			return "", err
		}
	}

	if err = k.SetSymbol(ctx, settings.Symbol, settings.Issuer); err != nil {
		return "", sdkerrors.Wrapf(err, "provided symbol: %s", settings.Symbol)
	}

	definition := types.Definition{
		Denom:              denom,
		Issuer:             settings.Issuer.String(),
		Features:           settings.Features,
		BurnRate:           settings.BurnRate,
		SendCommissionRate: settings.SendCommissionRate,
		Version:            version,
		URI:                settings.URI,
		URIHash:            settings.URIHash,
		Admin:              settings.Issuer.String(),
	}

	if err = k.mintIfReceivable(ctx, definition, settings.InitialAmount, settings.Issuer); err != nil {
		return "", err
	}

	if definition.IsFeatureEnabled(types.Feature_extension) {
		if settings.ExtensionSettings == nil {
			return "", types.ErrInvalidInput.Wrap("extension settings must be provided")
		}

		if len(settings.ExtensionSettings.IssuanceMsg) == 0 {
			settings.ExtensionSettings.IssuanceMsg = []byte("{}")
		}

		instantiateMsgBytes, err := json.Marshal(ExtensionInstantiateMsg{
			Denom:       denom,
			IssuanceMsg: settings.ExtensionSettings.IssuanceMsg,
		})
		if err != nil {
			return "", types.ErrInvalidInput.Wrapf("error marshalling ExtensionInstantiateMsg (%s)", err)
		}

		contractAddress, _, err := k.wasmPermissionedKeeper.Instantiate2(
			ctx,
			settings.ExtensionSettings.CodeId,
			settings.Issuer,
			settings.Issuer,
			instantiateMsgBytes,
			settings.ExtensionSettings.Label,
			settings.ExtensionSettings.Funds,
			ctx.BlockHeader().AppHash,
			true,
		)
		if err != nil {
			return "", sdkerrors.Wrapf(err, "error instantiating cw contract")
		}

		definition.ExtensionCWAddress = contractAddress.String()
	}

	if err = k.SetDenomMetadata(
		ctx,
		denom,
		settings.Symbol,
		settings.Description,
		settings.URI,
		settings.URIHash,
		settings.Precision,
	); err != nil {
		return "", err
	}

	if err = k.SetDefinition(ctx, settings.Issuer, settings.Subunit, definition); err != nil {
		return "", err
	}

	if settings.DEXSettings != nil {
		if err := types.ValidateDEXSettings(*settings.DEXSettings); err != nil {
			return "", err
		}

		if err := types.ValidateDEXSettingsAccess(*settings.DEXSettings, definition); err != nil {
			return "", err
		}

		if err := k.SetDEXSettings(ctx, denom, *settings.DEXSettings); err != nil {
			return "", err
		}
	}

	if err = ctx.EventManager().EmitTypedEvent(&types.EventIssued{
		Denom:              denom,
		Issuer:             settings.Issuer.String(),
		Symbol:             settings.Symbol,
		Subunit:            settings.Subunit,
		Precision:          settings.Precision,
		Description:        settings.Description,
		InitialAmount:      settings.InitialAmount,
		Features:           settings.Features,
		BurnRate:           settings.BurnRate,
		SendCommissionRate: settings.SendCommissionRate,
		URI:                settings.URI,
		URIHash:            settings.URIHash,
		Admin:              settings.Issuer.String(),
		DEXSettings:        settings.DEXSettings,
	}); err != nil {
		return "", sdkerrors.Wrapf(types.ErrInvalidState, "failed to emit EventIssued event: %s", err)
	}

	k.logger(ctx).Debug(
		"issued new fungible token",
		"denom", denom,
		"settings", settings,
	)

	return denom, nil
}

// SetSymbol saves the symbol to store.
func (k Keeper) SetSymbol(ctx sdk.Context, symbol string, issuer sdk.AccAddress) error {
	symbol = types.NormalizeSymbolForKey(symbol)
	isSymbolDuplicated, err := k.isSymbolDuplicated(ctx, symbol, issuer)
	if err != nil {
		return err
	}

	if isSymbolDuplicated {
		return sdkerrors.Wrapf(types.ErrInvalidInput, "duplicate symbol %s", symbol)
	}

	return k.storeService.OpenKVStore(ctx).Set(types.CreateSymbolKey(issuer, symbol), types.StoreTrue)
}

// SetDefinition stores the Definition.
func (k Keeper) SetDefinition(
	ctx sdk.Context, issuer sdk.AccAddress, subunit string, definition types.Definition,
) error {
	return k.storeService.OpenKVStore(ctx).Set(types.CreateTokenKey(issuer, subunit), k.cdc.MustMarshal(&definition))
}

// SetDenomMetadata registers denom metadata on the bank keeper.
func (k Keeper) SetDenomMetadata(
	ctx sdk.Context,
	denom, symbol, description, uri, uriHash string,
	precision uint32,
) error {
	denomMetadata := banktypes.Metadata{
		Description: description,
		// This is a cosmos sdk requirement that the first denomination unit MUST be the base
		DenomUnits: []*banktypes.DenomUnit{
			{
				Denom:    denom,
				Exponent: uint32(0),
			},
			{
				Denom:    symbol,
				Exponent: precision,
			},
		},
		// here take subunit provided by the user, generate the denom and used it as base,
		// and we take the symbol provided by the user and use it as symbol
		Base:    denom,
		Display: symbol,
		Name:    symbol,
		Symbol:  symbol,
		URI:     uri,
		URIHash: uriHash,
	}

	// in case the precision is zero, we cannot 2 zero exponents in denom units, so
	// we are force to have single entry in denom units and also Display must be the
	// same as Base.
	if precision == 0 {
		denomMetadata.DenomUnits = []*banktypes.DenomUnit{
			{
				Denom:    denom,
				Exponent: uint32(0),
			},
		}
		denomMetadata.Display = denom
	}

	if err := denomMetadata.Validate(); err != nil {
		return sdkerrors.Wrapf(types.ErrInvalidInput, "failed to validate denom metadata: %s", err)
	}

	k.bankKeeper.SetDenomMetaData(ctx, denomMetadata)
	return nil
}

// Mint mints new fungible token.
func (k Keeper) Mint(ctx sdk.Context, sender, recipient sdk.AccAddress, coin sdk.Coin) error {
	if coin.Amount.GT(types.MaxMintableAmount) {
		return sdkerrors.Wrapf(types.ErrInvalidInput, "minting amount is greater than maximum allowed")
	}
	def, err := k.GetDefinition(ctx, coin.Denom)
	if err != nil {
		return sdkerrors.Wrapf(err, "not able to get token info for denom:%s", coin.Denom)
	}

	if err = def.CheckFeatureAllowed(sender, types.Feature_minting); err != nil {
		return err
	}

	return k.mintIfReceivable(ctx, def, coin.Amount, recipient)
}

// Burn burns fungible token.
func (k Keeper) Burn(ctx sdk.Context, sender sdk.AccAddress, coin sdk.Coin) error {
	def, err := k.GetDefinition(ctx, coin.Denom)
	if err != nil {
		return sdkerrors.Wrapf(err, "not able to get token info for denom:%s", coin.Denom)
	}

	err = def.CheckFeatureAllowed(sender, types.Feature_burning)
	if err != nil {
		return err
	}

	return k.burnIfSpendable(ctx, sender, def, coin.Amount)
}

// Freeze freezes specified token from the specified account.
func (k Keeper) Freeze(ctx sdk.Context, sender, addr sdk.AccAddress, coin sdk.Coin) error {
	if !coin.IsPositive() {
		return sdkerrors.Wrap(cosmoserrors.ErrInvalidCoins, "freeze amount should be positive")
	}

	if err := k.freezingChecks(ctx, sender, addr, coin); err != nil {
		return err
	}

	frozenStore := k.frozenAccountBalanceStore(ctx, addr)
	frozenBalance := frozenStore.Balance(coin.Denom)
	newFrozenBalance := frozenBalance.Add(coin)
	frozenStore.SetBalance(newFrozenBalance)

	if err := ctx.EventManager().EmitTypedEvent(&types.EventFrozenAmountChanged{
		Account:        addr.String(),
		Denom:          coin.Denom,
		PreviousAmount: frozenBalance.Amount,
		CurrentAmount:  newFrozenBalance.Amount,
	}); err != nil {
		return sdkerrors.Wrapf(types.ErrInvalidState, "failed to emit EventFrozenAmountChanged event: %s", err)
	}

	return nil
}

// Unfreeze unfreezes specified tokens from the specified account.
func (k Keeper) Unfreeze(ctx sdk.Context, sender, addr sdk.AccAddress, coin sdk.Coin) error {
	if !coin.IsPositive() {
		return sdkerrors.Wrap(cosmoserrors.ErrInvalidCoins, "freeze amount should be positive")
	}

	if err := k.freezingChecks(ctx, sender, addr, coin); err != nil {
		return err
	}

	frozenStore := k.frozenAccountBalanceStore(ctx, addr)
	frozenBalance := frozenStore.Balance(coin.Denom)
	if !frozenBalance.IsGTE(coin) {
		return sdkerrors.Wrapf(cosmoserrors.ErrInsufficientFunds,
			"unfreeze request %s is greater than the available frozen balance %s",
			coin.String(),
			frozenBalance.String(),
		)
	}

	newFrozenBalance := frozenBalance.Sub(coin)
	frozenStore.SetBalance(newFrozenBalance)

	if err := ctx.EventManager().EmitTypedEvent(&types.EventFrozenAmountChanged{
		Account:        addr.String(),
		Denom:          coin.Denom,
		PreviousAmount: frozenBalance.Amount,
		CurrentAmount:  newFrozenBalance.Amount,
	}); err != nil {
		return sdkerrors.Wrapf(types.ErrInvalidState, "failed to emit EventFrozenAmountChanged event: %s", err)
	}

	return nil
}

// SetFrozen sets frozen amount on the specified account.
func (k Keeper) SetFrozen(ctx sdk.Context, sender, addr sdk.AccAddress, coin sdk.Coin) error {
	if coin.IsNegative() {
		return sdkerrors.Wrap(cosmoserrors.ErrInvalidCoins, "frozen amount must not be negative")
	}

	if err := k.freezingChecks(ctx, sender, addr, coin); err != nil {
		return err
	}

	frozenStore := k.frozenAccountBalanceStore(ctx, addr)
	frozenBalance := frozenStore.Balance(coin.Denom)
	frozenStore.SetBalance(coin)

	if err := ctx.EventManager().EmitTypedEvent(&types.EventFrozenAmountChanged{
		Account:        addr.String(),
		Denom:          coin.Denom,
		PreviousAmount: frozenBalance.Amount,
		CurrentAmount:  coin.Amount,
	}); err != nil {
		return sdkerrors.Wrapf(types.ErrInvalidState, "failed to emit EventFrozenAmountChanged event: %s", err)
	}

	return nil
}

// GloballyFreeze enables global freeze on a fungible token. This function is idempotent.
func (k Keeper) GloballyFreeze(ctx sdk.Context, sender sdk.AccAddress, denom string) error {
	def, err := k.GetDefinition(ctx, denom)
	if err != nil {
		return sdkerrors.Wrapf(err, "not able to get token info for denom:%s", denom)
	}

	if err = def.CheckFeatureAllowed(sender, types.Feature_freezing); err != nil {
		return err
	}

	return k.SetGlobalFreeze(ctx, denom, true)
}

// GloballyUnfreeze disables global freeze on a fungible token. This function is idempotent.
func (k Keeper) GloballyUnfreeze(ctx sdk.Context, sender sdk.AccAddress, denom string) error {
	def, err := k.GetDefinition(ctx, denom)
	if err != nil {
		return sdkerrors.Wrapf(err, "not able to get token info for denom:%s", denom)
	}

	if err = def.CheckFeatureAllowed(sender, types.Feature_freezing); err != nil {
		return err
	}

	return k.SetGlobalFreeze(ctx, denom, false)
}

// GetAccountsFrozenBalances returns the frozen balance on all the account.
func (k Keeper) GetAccountsFrozenBalances(
	ctx sdk.Context,
	pagination *query.PageRequest,
) ([]types.Balance, *query.PageResponse, error) {
	return collectBalances(k.cdc, k.frozenBalancesStore(ctx), pagination)
}

// IterateAccountsFrozenBalances iterates over all frozen balances of all accounts and applies the provided callback.
// If true is returned from the callback, iteration is stopped.
func (k Keeper) IterateAccountsFrozenBalances(ctx sdk.Context, cb func(sdk.AccAddress, sdk.Coin) bool) error {
	return k.frozenAccountsBalanceStore(ctx).IterateAllBalances(cb)
}

// GetFrozenBalances returns the frozen balance of an account.
func (k Keeper) GetFrozenBalances(
	ctx sdk.Context,
	addr sdk.AccAddress,
	pagination *query.PageRequest,
) (sdk.Coins, *query.PageResponse, error) {
	return k.frozenAccountBalanceStore(ctx, addr).Balances(pagination)
}

// GetFrozenBalance returns the frozen balance of a denom and account.
func (k Keeper) GetFrozenBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) (sdk.Coin, error) {
	isGloballyFrozen, err := k.isGloballyFrozen(ctx, denom)
	if err != nil {
		return sdk.Coin{}, err
	}

	def, err := k.getDefinitionOrNil(ctx, denom)
	if err != nil {
		return sdk.Coin{}, sdkerrors.Wrapf(err, "not able to get token info for denom:%s", denom)
	}

	if def != nil && def.HasAdminPrivileges(addr) {
		return sdk.NewCoin(denom, sdkmath.ZeroInt()), nil
	}

	if isGloballyFrozen {
		return k.bankKeeper.GetBalance(ctx, addr, denom), nil
	}
	return k.frozenAccountBalanceStore(ctx, addr).Balance(denom), nil
}

// SetFrozenBalances sets the frozen balances of a specified account.
// Pay attention that the sdk.NewCoins() sanitizes/removes the empty coins,
// hence if you need set zero amount use the slice []sdk.Coins.
func (k Keeper) SetFrozenBalances(ctx sdk.Context, addr sdk.AccAddress, coins sdk.Coins) {
	frozenStore := k.frozenAccountBalanceStore(ctx, addr)
	for _, coin := range coins {
		frozenStore.SetBalance(coin)
	}
}

// SetGlobalFreeze enables/disables global freeze on a fungible token depending on frozen arg.
func (k Keeper) SetGlobalFreeze(ctx sdk.Context, denom string, frozen bool) error {
	store := k.storeService.OpenKVStore(ctx)
	if frozen {
		return store.Set(types.CreateGlobalFreezeKey(denom), types.StoreTrue)
	}
	return store.Delete(types.CreateGlobalFreezeKey(denom))
}

// Clawback confiscates specified token from the specified account.
func (k Keeper) Clawback(ctx sdk.Context, sender, addr sdk.AccAddress, coin sdk.Coin) error {
	if !coin.IsPositive() {
		return sdkerrors.Wrap(cosmoserrors.ErrInvalidCoins, "clawback amount should be positive")
	}

	if err := k.validateClawbackAllowed(ctx, sender, addr, coin); err != nil {
		return err
	}

	if err := k.bankKeeper.SendCoins(ctx, addr, sender, sdk.NewCoins(coin)); err != nil {
		return sdkerrors.Wrapf(err, "can't send coins from account %s to issuer %s", addr.String(), sender.String())
	}

	if err := ctx.EventManager().EmitTypedEvent(&types.EventAmountClawedBack{
		Account: addr.String(),
		Denom:   coin.Denom,
		Amount:  coin.Amount,
	}); err != nil {
		return sdkerrors.Wrapf(types.ErrInvalidState, "failed to emit EventAmountClawedBack event: %s", err)
	}

	return nil
}

// SetWhitelistedBalance sets whitelisted limit for the account.
func (k Keeper) SetWhitelistedBalance(ctx sdk.Context, sender, addr sdk.AccAddress, coin sdk.Coin) error {
	if coin.IsNil() || coin.IsNegative() {
		return sdkerrors.Wrap(cosmoserrors.ErrInvalidCoins, "whitelisted limit amount should be greater than or equal to 0")
	}

	def, err := k.GetDefinition(ctx, coin.Denom)
	if err != nil {
		return sdkerrors.Wrapf(err, "not able to get token info for denom:%s", coin.Denom)
	}

	if def.IsAdmin(addr) {
		return sdkerrors.Wrap(cosmoserrors.ErrUnauthorized, "admin's balance can't be whitelisted")
	}

	if err = def.CheckFeatureAllowed(sender, types.Feature_whitelisting); err != nil {
		return err
	}

	whitelistedStore := k.whitelistedAccountBalanceStore(ctx, addr)
	previousWhitelistedBalance := whitelistedStore.Balance(coin.Denom)
	whitelistedStore.SetBalance(coin)

	if err = ctx.EventManager().EmitTypedEvent(&types.EventWhitelistedAmountChanged{
		Account:        addr.String(),
		Denom:          coin.Denom,
		PreviousAmount: previousWhitelistedBalance.Amount,
		CurrentAmount:  coin.Amount,
	}); err != nil {
		return sdkerrors.Wrapf(types.ErrInvalidState, "failed to emit EventWhitelistedAmountChanged event: %s", err)
	}

	return nil
}

// GetAccountsWhitelistedBalances returns the whitelisted balance of all the account.
func (k Keeper) GetAccountsWhitelistedBalances(
	ctx sdk.Context,
	pagination *query.PageRequest,
) ([]types.Balance, *query.PageResponse, error) {
	store := k.storeService.OpenKVStore(ctx)
	return collectBalances(
		k.cdc, prefix.NewStore(runtime.KVStoreAdapter(store), types.WhitelistedBalancesKeyPrefix), pagination)
}

// IterateAccountsWhitelistedBalances iterates over all whitelisted balances of all accounts
// and applies the provided callback.
// If true is returned from the callback, iteration is halted.
func (k Keeper) IterateAccountsWhitelistedBalances(ctx sdk.Context, cb func(sdk.AccAddress, sdk.Coin) bool) error {
	store := k.storeService.OpenKVStore(ctx)
	return newBalanceStore(k.cdc, runtime.KVStoreAdapter(store), types.WhitelistedBalancesKeyPrefix).IterateAllBalances(cb)
}

// GetWhitelistedBalances returns the whitelisted balance of an account.
func (k Keeper) GetWhitelistedBalances(
	ctx sdk.Context,
	addr sdk.AccAddress,
	pagination *query.PageRequest,
) (sdk.Coins, *query.PageResponse, error) {
	return k.whitelistedAccountBalanceStore(ctx, addr).Balances(pagination)
}

// GetWhitelistedBalance returns the whitelisted balance of a denom and account.
func (k Keeper) GetWhitelistedBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return k.whitelistedAccountBalanceStore(ctx, addr).Balance(denom)
}

// SetWhitelistedBalances sets the whitelisted balances of a specified account.
// Pay attention that the sdk.NewCoins() sanitizes/removes the empty coins, hence if you
// need set zero amount use the slice []sdk.Coins.
func (k Keeper) SetWhitelistedBalances(ctx sdk.Context, addr sdk.AccAddress, coins sdk.Coins) {
	whitelistedStore := k.whitelistedAccountBalanceStore(ctx, addr)
	for _, coin := range coins {
		whitelistedStore.SetBalance(coin)
	}
}

// GetSpendableBalance returns balance allowed to be spent.
func (k Keeper) GetSpendableBalance(
	ctx sdk.Context,
	addr sdk.AccAddress,
	denom string,
) (sdk.Coin, error) {
	balance := k.bankKeeper.GetBalance(ctx, addr, denom)
	if balance.Amount.IsZero() {
		return balance, nil
	}

	notLockedAmt := balance.Amount.
		Sub(k.GetDEXLockedBalance(ctx, addr, denom).Amount).
		Sub(k.bankKeeper.LockedCoins(ctx, addr).AmountOf(denom))
	if notLockedAmt.IsNegative() {
		return sdk.NewCoin(denom, sdkmath.ZeroInt()), nil
	}

	def, err := k.getDefinitionOrNil(ctx, denom)
	if err != nil {
		return sdk.Coin{}, err
	}
	// the spendable balance counts the frozen balance
	if def != nil && def.IsFeatureEnabled(types.Feature_freezing) {
		frozenBalance, err := k.GetFrozenBalance(ctx, addr, denom)
		if err != nil {
			return sdk.Coin{}, err
		}
		notFrozenAmt := balance.Amount.Sub(frozenBalance.Amount)
		if notFrozenAmt.IsNegative() {
			return sdk.NewCoin(denom, sdkmath.ZeroInt()), nil
		}
		spendableAmount := sdkmath.MinInt(notLockedAmt, notFrozenAmt)
		return sdk.NewCoin(denom, spendableAmount), nil
	}

	return sdk.NewCoin(denom, notLockedAmt), nil
}

// TransferAdmin changes admin of a fungible token.
func (k Keeper) TransferAdmin(ctx sdk.Context, sender, addr sdk.AccAddress, denom string) error {
	def, err := k.GetDefinition(ctx, denom)
	if err != nil {
		return sdkerrors.Wrapf(err, "not able to get token info for denom:%s", denom)
	}

	if !def.IsAdmin(sender) {
		return sdkerrors.Wrap(cosmoserrors.ErrUnauthorized, "only admin can transfer administration of an account")
	}

	previousAdmin := def.Admin

	subunit, issuer, err := types.DeconstructDenom(denom)
	if err != nil {
		return err
	}

	def.Admin = addr.String()
	if err := k.SetDefinition(ctx, issuer, subunit, def); err != nil {
		return err
	}

	if err := ctx.EventManager().EmitTypedEvent(&types.EventAdminTransferred{
		Denom:         denom,
		PreviousAdmin: previousAdmin,
		CurrentAdmin:  def.Admin,
	}); err != nil {
		return sdkerrors.Wrapf(types.ErrInvalidState, "failed to emit EventAdminTransferred event: %s", err)
	}

	return nil
}

// ClearAdmin removes admin of a fungible token.
func (k Keeper) ClearAdmin(ctx sdk.Context, sender sdk.AccAddress, denom string) error {
	def, err := k.GetDefinition(ctx, denom)
	if err != nil {
		return sdkerrors.Wrapf(err, "not able to get token info for denom:%s", denom)
	}

	if !def.IsAdmin(sender) {
		return sdkerrors.Wrap(cosmoserrors.ErrUnauthorized, "only admin can remove administration of an account")
	}

	previousAdmin := def.Admin

	subunit, issuer, err := types.DeconstructDenom(denom)
	if err != nil {
		return err
	}

	// if extension feature is disabled, after clearing admin, there is no one to send commission to, so the commission
	// rate sets to zero else only the admin is cleared and the extension receives the commission rate
	def.Admin = ""
	if !def.IsFeatureEnabled(types.Feature_extension) {
		def.SendCommissionRate = sdkmath.LegacyZeroDec()
	}

	if err := k.SetDefinition(ctx, issuer, subunit, def); err != nil {
		return err
	}

	if err := ctx.EventManager().EmitTypedEvent(&types.EventAdminCleared{
		Denom:         denom,
		PreviousAdmin: previousAdmin,
	}); err != nil {
		return sdkerrors.Wrapf(types.ErrInvalidState, "failed to emit EventAdminCleared event: %s", err)
	}

	return nil
}

// HasSupply checks if the supply of denom exists in store.
func (k Keeper) HasSupply(ctx context.Context, denom string) bool {
	return k.bankKeeper.HasSupply(ctx, denom)
}

func (k Keeper) mintIfReceivable(
	ctx sdk.Context,
	def types.Definition,
	amount sdkmath.Int,
	recipient sdk.AccAddress,
) error {
	if !amount.IsPositive() {
		return nil
	}

	if wasm.IsSmartContract(ctx, recipient, k.wasmKeeper) {
		ctx = cwasmtypes.WithSmartContractRecipient(ctx, recipient.String())
	}

	if err := k.validateCoinReceivable(ctx, recipient, def, amount); err != nil {
		return sdkerrors.Wrapf(err, "coins are not receivable")
	}

	coinsToMint := sdk.NewCoins(sdk.NewCoin(def.Denom, amount))
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, coinsToMint); err != nil {
		return sdkerrors.Wrapf(err, "can't mint %s for the module %s", coinsToMint.String(), types.ModuleName)
	}

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, coinsToMint); err != nil {
		return sdkerrors.Wrapf(
			err,
			"can't send minted coins from module %s to account %s",
			types.ModuleName,
			recipient.String(),
		)
	}

	return nil
}

func (k Keeper) burnIfSpendable(
	ctx sdk.Context,
	account sdk.AccAddress,
	def types.Definition,
	amount sdkmath.Int,
) error {
	if err := k.validateCoinSpendable(ctx, account, def, amount); err != nil {
		return sdkerrors.Wrapf(err, "coins are not spendable")
	}

	return k.burn(ctx, account, sdk.NewCoins(sdk.NewCoin(def.Denom, amount)))
}

func (k Keeper) burn(ctx sdk.Context, account sdk.AccAddress, coinsToBurn sdk.Coins) error {
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, account, types.ModuleName, coinsToBurn); err != nil {
		return sdkerrors.Wrapf(err, "can't send coins from account %s to module %s", account.String(), types.ModuleName)
	}

	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coinsToBurn); err != nil {
		return sdkerrors.Wrapf(err, "can't burn %s for the module %s", coinsToBurn.String(), types.ModuleName)
	}

	return nil
}

func (k Keeper) validateCoinSpendable(
	ctx sdk.Context,
	addr sdk.AccAddress,
	def types.Definition,
	amount sdkmath.Int,
) error {
	// This check is effective when IBC transfer is acknowledged by the peer chain. It happens in two situations:
	// - when transfer succeeded
	// - when transfer has been rejected by the other chain.
	// `validateCoinSpendable` is called only in the second case, that's why we don't need to differentiate them here.
	// So, whenever it happens here, it means transfer has been rejected. It means that funds are going to be refunded
	// back to the sender by the IBC transfer module.
	// It should succeed even if the issuer decided, for whatever reason, to freeze the escrow address.
	// It is done before checking for global freeze because refunding should not be blocked by this.
	// Otherwise, funds would be lost forever, being blocked on the escrow account.
	if wibctransfertypes.IsPurposeAck(ctx) {
		return nil
	}

	// Same thing applies if IBC fails due to timeout.
	if wibctransfertypes.IsPurposeTimeout(ctx) {
		return nil
	}

	if def.IsFeatureEnabled(types.Feature_freezing) {
		isGloballyFrozen, err := k.isGloballyFrozen(ctx, def.Denom)
		if err != nil {
			return err
		}
		if isGloballyFrozen && !def.HasAdminPrivileges(addr) {
			return sdkerrors.Wrapf(types.ErrGloballyFrozen, "%s is globally frozen", def.Denom)
		}
	}

	// Checking for IBC-received transfer is done here (after call to k.isGloballyFrozen), because those transfers
	// should be affected by the global freeze checked above. We decided that if token is frozen globally it means
	// none of the balances for that token can be affected by the IBC incoming transfer during freezing period.
	// Otherwise, the transfer must work despite the fact that escrow address might have been frozen by the issuer.
	if wibctransfertypes.IsPurposeIn(ctx) {
		return nil
	}

	if def.IsFeatureEnabled(types.Feature_block_smart_contracts) &&
		!def.HasAdminPrivileges(addr) &&
		cwasmtypes.IsTriggeredBySmartContract(ctx) {
		return sdkerrors.Wrapf(
			cosmoserrors.ErrUnauthorized,
			"transfers made by smart contracts are disabled for %s",
			def.Denom,
		)
	}

	if err := k.validateCoinIsNotLockedByDEXAndBank(ctx, addr, sdk.NewCoin(def.Denom, amount)); err != nil {
		return err
	}

	if def.IsFeatureEnabled(types.Feature_freezing) && !def.HasAdminPrivileges(addr) {
		frozenBalance, err := k.GetFrozenBalance(ctx, addr, def.Denom)
		if err != nil {
			return err
		}
		frozenAmt := frozenBalance.Amount
		balance := k.bankKeeper.GetBalance(ctx, addr, def.Denom)
		notFrozenAmt := balance.Amount.Sub(frozenAmt)
		if notFrozenAmt.LT(amount) {
			return sdkerrors.Wrapf(cosmoserrors.ErrInsufficientFunds, "%s%s is not available, available %s%s",
				amount.String(), def.Denom, notFrozenAmt.String(), def.Denom)
		}
	}

	return nil
}

func (k Keeper) validateCoinReceivable(
	ctx sdk.Context,
	addr sdk.AccAddress,
	def types.Definition,
	amount sdkmath.Int,
) error {
	// This check is effective when funds for IBC transfers are received by the escrow address.
	// If IBC is enabled we always accept escrow address as a receiver of the funds because it must work
	// despite the fact that address is not whitelisted.
	// On the other hand, if IBC is disabled for the token, we reject the transfer to the escrow address.
	// We don't block on IsPurposeIn condition when IBC transfer is received because if token cannot be sent,
	// it cannot be received back by definition.
	if wibctransfertypes.IsPurposeOut(ctx) {
		if !def.IsFeatureEnabled(types.Feature_ibc) {
			return sdkerrors.Wrapf(cosmoserrors.ErrUnauthorized, "ibc transfers are disabled for %s", def.Denom)
		}
		return nil
	}

	// This check is effective when IBC transfer is acknowledged by the peer chain. It happens in two situations:
	// - when transfer succeeded
	// - when transfer has been rejected by the other chain.
	// `validateCoinReceivable` is called only in the second case, that's why we don't need to differentiate them here.
	// So, whenever it happens here, it means transfer has been rejected. It means that funds are going to be refunded
	// back to the sender by the IBC transfer module.
	// That means we should allow to do this even if the sender is no longer whitelisted. It might happen that between
	// sending IBC transfer and receiving ACK rejecting it, issuer decided to remove whitelisting for the sender.
	// Despite that, sender should receive his funds back because otherwise they are lost forever, being blocked
	// on the escrow address.
	if wibctransfertypes.IsPurposeAck(ctx) {
		return nil
	}

	// Same thing applies if IBC fails due to timeout.
	if wibctransfertypes.IsPurposeTimeout(ctx) {
		return nil
	}

	if def.IsFeatureEnabled(types.Feature_whitelisting) && !def.HasAdminPrivileges(addr) {
		if err := k.validateWhitelistedBalance(ctx, addr, sdk.NewCoin(def.Denom, amount)); err != nil {
			return err
		}
	}

	if def.IsFeatureEnabled(types.Feature_block_smart_contracts) &&
		!def.HasAdminPrivileges(addr) &&
		cwasmtypes.IsReceivingSmartContract(ctx, addr.String()) {
		return sdkerrors.Wrapf(cosmoserrors.ErrUnauthorized, "transfers to smart contracts are disabled for %s", def.Denom)
	}

	return nil
}

func (k Keeper) isSymbolDuplicated(ctx sdk.Context, symbol string, issuer sdk.AccAddress) (bool, error) {
	compositeKey := types.CreateSymbolKey(issuer, symbol)
	rawBytes, err := k.storeService.OpenKVStore(ctx).Get(compositeKey)
	if err != nil {
		return false, err
	}
	return rawBytes != nil, nil
}

func (k Keeper) getDefinitions(
	ctx sdk.Context,
	pagination *query.PageRequest,
) ([]types.Definition, *query.PageResponse, error) {
	store := k.storeService.OpenKVStore(ctx)
	return k.getDefinitionsFromStore(prefix.NewStore(runtime.KVStoreAdapter(store), types.TokenKeyPrefix), pagination)
}

func (k Keeper) getDefinitionOrNil(ctx sdk.Context, denom string) (*types.Definition, error) {
	def, err := k.GetDefinition(ctx, denom)
	if err != nil {
		if sdkerrors.IsOf(err, types.ErrInvalidDenom, types.ErrTokenNotFound) {
			return nil, nil //nolint:nilnil //returns nil if data not found
		}

		return nil, sdkerrors.Wrapf(types.ErrInvalidState, "failed to get definton for denom: %s", denom)
	}

	return &def, nil
}

func (k Keeper) getIssuerDefinitions(
	ctx sdk.Context,
	issuer sdk.AccAddress,
	pagination *query.PageRequest,
) ([]types.Definition, *query.PageResponse, error) {
	store := k.storeService.OpenKVStore(ctx)
	return k.getDefinitionsFromStore(
		prefix.NewStore(runtime.KVStoreAdapter(store), types.CreateIssuerTokensPrefix(issuer)),
		pagination,
	)
}

func (k Keeper) getTokenFullInfo(ctx sdk.Context, definition types.Definition) (types.Token, error) {
	subunit, _, err := types.DeconstructDenom(definition.Denom)
	if err != nil {
		return types.Token{}, err
	}

	metadata, found := k.bankKeeper.GetDenomMetaData(ctx, definition.Denom)
	if !found {
		return types.Token{}, sdkerrors.Wrapf(types.ErrTokenNotFound, "metadata for %s denom not found", definition.Denom)
	}

	precision := -1
	for _, unit := range metadata.DenomUnits {
		if unit.Denom == metadata.Display {
			precision = int(unit.Exponent)
			break
		}
	}

	if precision < 0 {
		return types.Token{}, sdkerrors.Wrap(types.ErrInvalidInput, "precision not found")
	}

	dexSettings, err := k.getDEXSettingsOrNil(ctx, definition.Denom)
	if err != nil {
		return types.Token{}, err
	}

	isGloballyFrozen, err := k.isGloballyFrozen(ctx, definition.Denom)
	if err != nil {
		return types.Token{}, err
	}

	return types.Token{
		Denom:              definition.Denom,
		Issuer:             definition.Issuer,
		Symbol:             metadata.Symbol,
		Precision:          uint32(precision),
		Subunit:            subunit,
		Description:        metadata.Description,
		Features:           definition.Features,
		BurnRate:           definition.BurnRate,
		SendCommissionRate: definition.SendCommissionRate,
		GloballyFrozen:     isGloballyFrozen,
		Version:            definition.Version,
		URI:                definition.URI,
		URIHash:            definition.URIHash,
		Admin:              definition.Admin,
		ExtensionCWAddress: definition.ExtensionCWAddress,
		DEXSettings:        dexSettings,
	}, nil
}

func (k Keeper) getDefinitionsFromStore(
	store prefix.Store,
	pagination *query.PageRequest,
) ([]types.Definition, *query.PageResponse, error) {
	definitionsPointers, pageRes, err := query.GenericFilteredPaginate(
		k.cdc,
		store,
		pagination,
		// builder
		func(key []byte, definition *types.Definition) (*types.Definition, error) {
			return definition, nil
		},
		// constructor
		func() *types.Definition {
			return &types.Definition{}
		},
	)
	if err != nil {
		return nil, nil, sdkerrors.Wrapf(types.ErrInvalidInput, "failed to paginate: %s", err)
	}

	definitions := make([]types.Definition, 0, len(definitionsPointers))
	for _, definition := range definitionsPointers {
		definitions = append(definitions, *definition)
	}

	return definitions, pageRes, nil
}

func (k Keeper) getTokensByDefinitions(ctx sdk.Context, defs []types.Definition) ([]types.Token, error) {
	tokens := make([]types.Token, 0, len(defs))
	for _, definition := range defs {
		token, err := k.getTokenFullInfo(ctx, definition)
		if err != nil {
			return nil, err
		}

		tokens = append(tokens, token)
	}
	return tokens, nil
}

// frozenBalancesStore get the store for the frozen balances of all accounts.
func (k Keeper) frozenBalancesStore(ctx sdk.Context) prefix.Store {
	store := k.storeService.OpenKVStore(ctx)
	return prefix.NewStore(runtime.KVStoreAdapter(store), types.FrozenBalancesKeyPrefix)
}

// frozenAccountBalanceStore gets the store for the frozen balances of an account.
func (k Keeper) frozenAccountBalanceStore(ctx sdk.Context, addr sdk.AccAddress) balanceStore {
	store := k.storeService.OpenKVStore(ctx)
	return newBalanceStore(k.cdc, runtime.KVStoreAdapter(store), types.CreateFrozenBalancesKey(addr))
}

// frozenAccountBalanceStore gets the store for the frozen balances of an account.
func (k Keeper) frozenAccountsBalanceStore(ctx sdk.Context) balanceStore {
	store := k.storeService.OpenKVStore(ctx)
	return newBalanceStore(k.cdc, runtime.KVStoreAdapter(store), types.FrozenBalancesKeyPrefix)
}

func (k Keeper) freezingChecks(ctx sdk.Context, sender, addr sdk.AccAddress, coin sdk.Coin) error {
	def, err := k.GetDefinition(ctx, coin.Denom)
	if err != nil {
		return sdkerrors.Wrapf(err, "not able to get token info for denom:%s", coin.Denom)
	}

	if def.HasAdminPrivileges(addr) {
		return sdkerrors.Wrap(cosmoserrors.ErrUnauthorized, "admin's balance can't be frozen")
	}

	return def.CheckFeatureAllowed(sender, types.Feature_freezing)
}

func (k Keeper) isGloballyFrozen(ctx sdk.Context, denom string) (bool, error) {
	isGloballyFrozen, err := k.storeService.OpenKVStore(ctx).Get(types.CreateGlobalFreezeKey(denom))
	if err != nil {
		return false, err
	}
	return bytes.Equal(isGloballyFrozen, types.StoreTrue), nil
}

func (k Keeper) validateClawbackAllowed(ctx sdk.Context, sender, addr sdk.AccAddress, coin sdk.Coin) error {
	def, err := k.GetDefinition(ctx, coin.Denom)
	if err != nil {
		return sdkerrors.Wrapf(err, "not able to get token info for denom:%s", coin.Denom)
	}

	if _, isModuleAccount := k.accountKeeper.GetAccount(ctx, addr).(*authtypes.ModuleAccount); isModuleAccount {
		return sdkerrors.Wrap(cosmoserrors.ErrUnauthorized, "claw back from module accounts is prohibited")
	}

	if err := k.validateCoinIsNotLockedByDEXAndBank(ctx, addr, coin); err != nil {
		return err
	}

	return def.CheckFeatureAllowed(sender, types.Feature_clawback)
}

// whitelistedAccountBalanceStore gets the store for the whitelisted balances of an account.
func (k Keeper) whitelistedAccountBalanceStore(ctx sdk.Context, addr sdk.AccAddress) balanceStore {
	store := k.storeService.OpenKVStore(ctx)
	return newBalanceStore(k.cdc, runtime.KVStoreAdapter(store), types.CreateWhitelistedBalancesKey(addr))
}

func (k Keeper) validateWhitelistedBalance(ctx sdk.Context, addr sdk.AccAddress, coin sdk.Coin) error {
	balance := k.bankKeeper.GetBalance(ctx, addr, coin.Denom)
	whitelistedBalance := k.GetWhitelistedBalance(ctx, addr, coin.Denom)
	dexExpectedToReceiveBalance := k.GetDEXExpectedToReceivedBalance(ctx, addr, coin.Denom)
	availableToReceiveAmount := whitelistedBalance.Amount.
		Sub(balance.Amount).
		Sub(dexExpectedToReceiveBalance.Amount)

	if availableToReceiveAmount.LT(coin.Amount) {
		return sdkerrors.Wrapf(
			types.ErrWhitelistedLimitExceeded,
			"balance whitelisted for %s is not enough to receive %s, available to receive balance: %s%s",
			addr, coin, availableToReceiveAmount.String(), coin.Denom)
	}

	return nil
}

// logger returns the Keeper logger.
func (k Keeper) logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

func (k Keeper) burnIssueFee(ctx sdk.Context, settings types.IssueSettings, params types.Params) error {
	if err := k.checkIssueFeeIsLimitedToCore(ctx, params); err != nil {
		return err
	}

	if err := k.validateCoinIsNotLockedByDEXAndBank(ctx, settings.Issuer, params.IssueFee); err != nil {
		return sdkerrors.Wrap(err, "out of funds to pay for issue fee")
	}

	return k.burn(ctx, settings.Issuer, sdk.NewCoins(params.IssueFee))
}

func (k Keeper) checkIssueFeeIsLimitedToCore(ctx sdk.Context, params types.Params) error {
	stakingParams, err := k.stakingKeeper.GetParams(ctx)
	if err != nil {
		return sdkerrors.Wrap(err, "not able to get staking params")
	}

	if params.IssueFee.Denom != stakingParams.BondDenom {
		return sdkerrors.Wrapf(cosmoserrors.ErrInvalidCoins, "not able to burn %s for issue fee, only %s is accepted",
			params.IssueFee.Denom, stakingParams.BondDenom)
	}

	return nil
}
