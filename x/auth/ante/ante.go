// This content was copied and modified based on github.com/cosmos/cosmos-sdk/x/auth/ante/ante.go
// Original content:
// https://github.com/cosmos/cosmos-sdk/blob/ad9e5620fb3445c716e9de45cfcdb56e8f1745bf/x/auth/ante/ante.go

package ante

import (
	"cosmossdk.io/core/store"
	sdkerrors "cosmossdk.io/errors"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmoserrors "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	crisistypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	ibcante "github.com/cosmos/ibc-go/v8/modules/core/ante"
	ibckeeper "github.com/cosmos/ibc-go/v8/modules/core/keeper"

	authkeeper "github.com/CoreumFoundation/coreum/v6/x/auth/keeper"
	"github.com/CoreumFoundation/coreum/v6/x/deterministicgas"
	deterministicgasante "github.com/CoreumFoundation/coreum/v6/x/deterministicgas/ante"
	feemodelante "github.com/CoreumFoundation/coreum/v6/x/feemodel/ante"
)

// HandlerOptions are the options required for constructing a default SDK AnteHandler.
type HandlerOptions struct {
	authante.HandlerOptions
	DeterministicGasConfig deterministicgas.Config
	FeeModelKeeper         feemodelante.Keeper
	WasmConfig             wasmtypes.NodeConfig
	IBCKeeper              *ibckeeper.Keeper
	GovKeeper              *govkeeper.Keeper
	WasmTXCounterStoreKey  store.KVStoreService
}

// NewAnteHandler returns an AnteHandler that checks and increments sequence
// numbers, checks signatures & account numbers, and deducts fees from the first
// signer.
func NewAnteHandler(options HandlerOptions) (sdk.AnteHandler, error) {
	if options.AccountKeeper == nil {
		return nil, sdkerrors.Wrap(cosmoserrors.ErrLogic, "account keeper is required for ante builder")
	}

	if options.BankKeeper == nil {
		return nil, sdkerrors.Wrap(cosmoserrors.ErrLogic, "bank keeper is required for ante builder")
	}

	if options.FeeModelKeeper == nil {
		return nil, sdkerrors.Wrap(cosmoserrors.ErrLogic, "fee model keeper is required for ante builder")
	}

	if options.IBCKeeper == nil {
		return nil, sdkerrors.Wrap(cosmoserrors.ErrLogic, "IBC keeper is required for ante builder")
	}

	if options.GovKeeper == nil {
		return nil, sdkerrors.Wrap(cosmoserrors.ErrLogic, "gov keeper is required for ante builder")
	}

	if options.SignModeHandler == nil {
		return nil, sdkerrors.Wrap(cosmoserrors.ErrLogic, "sign mode handler is required for ante builder")
	}

	if options.SigGasConsumer == nil {
		options.SigGasConsumer = authante.DefaultSigVerificationGasConsumer
	}

	if options.WasmTXCounterStoreKey == nil {
		return nil, sdkerrors.Wrap(cosmoserrors.ErrLogic, "tx counter key is required for ante builder")
	}

	infiniteAccountKeeper := authkeeper.NewInfiniteAccountKeeper(options.AccountKeeper)

	anteDecorators := []sdk.AnteDecorator{
		// We added 3 special decorators working together to provide deterministic gas consumption mechanism
		// for selected message types.
		// The decorators are:
		// - NewSetInfiniteGasMeterDecorator
		// - NewAddBaseGasDecorator
		// - NewChargeFixedGasDecorator
		//
		// We consume gas as follows:
		// - constant preliminary fee (`FixedGas`) is charged on every tx to cover the cost of running some ante decorators
		// - bonus gas is added for free to cover cost related to transaction size (`freeBytes`) and
		//   signatures (`freeSignatures`)
		// - at the end we compute gas available to message handlers
		//
		// Details:
		//
		// `SetInfiniteGasMeterDecorator` serves two purposes:
		// - verifies that at least `FixedGas` gas amount is provided by the sender
		// - replaces original gas meter with an infinite one to let all the preliminary ante decorators to pass without
		//   consuming real gas. It doesn't mean they run for free, the cost of running them is covered later by charging
		//   `FixedGas` on the final gas meter
		//
		// `AddBaseGasDecorator` is there to add some bonus gas covering cost of a tx size up to limit defined by `freeBytes`
		// and signature verification up to `freeSignatures` signatures.
		//
		// `ChargeFixedGasDecorator` creates final gas meter passed to message handlers and computes and charges final gas
		// to be consumed by the entire ante handler. Consumed gas is computed as follows:
		// - new gas meter is created with gas limit set to the amount declared by the tx sender
		// - `FixedGas` gas is consumed
		// - if more than bonus gas assigned by `AddBaseGasDecorator` was consumed by `ConsumeGasForTxSizeDecorator` and
		//   `SigGasConsumeDecorator`, the difference between bonus and real consumption is charged on the final gas meter.
		//   IMPORTANT: If they consumed less, the rest **IS NOT** given to the message handlers for free.

		authante.NewSetUpContextDecorator(), // outermost AnteDecorator. SetUpContext must be called first
		deterministicgasante.NewSetInfiniteGasMeterDecorator(options.DeterministicGasConfig),
		NewDenyMessagesDecorator(&crisistypes.MsgVerifyInvariant{}),
		authante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
		authante.NewValidateBasicDecorator(),
		authante.NewTxTimeoutHeightDecorator(),
		// after setup context to enforce limits early
		wasmkeeper.NewLimitSimulationGasDecorator(options.WasmConfig.SimulationGasLimit),
		wasmkeeper.NewCountTXDecorator(options.WasmTXCounterStoreKey),
		authante.NewValidateMemoDecorator(options.AccountKeeper),
		feemodelante.NewFeeDecorator(options.FeeModelKeeper),
		authante.NewDeductFeeDecorator(
			options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.TxFeeChecker,
		),
		// SetPubKeyDecorator must be called before all signature verification decorators
		authante.NewSetPubKeyDecorator(options.AccountKeeper),
		authante.NewValidateSigCountDecorator(options.AccountKeeper),
		authante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler),
		authante.NewIncrementSequenceDecorator(options.AccountKeeper),
		deterministicgasante.NewAddBaseGasDecorator(infiniteAccountKeeper, options.DeterministicGasConfig),
		authante.NewConsumeGasForTxSizeDecorator(infiniteAccountKeeper),
		authante.NewSigGasConsumeDecorator(infiniteAccountKeeper, options.SigGasConsumer),
		deterministicgasante.NewChargeFixedGasDecorator(infiniteAccountKeeper, options.DeterministicGasConfig),
		ibcante.NewRedundantRelayDecorator(options.IBCKeeper),
	}

	return sdk.ChainAnteDecorators(anteDecorators...), nil
}
