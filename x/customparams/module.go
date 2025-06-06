package customparams

import (
	"context"
	"encoding/json"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/CoreumFoundation/coreum/v6/x/customparams/keeper"
	"github.com/CoreumFoundation/coreum/v6/x/customparams/types"
)

var (
	_ module.AppModule           = AppModule{}
	_ module.AppModuleBasic      = AppModule{}
	_ module.AppModuleSimulation = AppModule{}
	_ module.HasGenesis          = AppModule{}
	_ module.HasServices         = AppModule{}

	_ appmodule.AppModule = AppModule{}
)

// AppModuleBasic defines the basic application module used by the customparams module.
type AppModuleBasic struct{}

// Name returns the customparams module's name.
func (AppModuleBasic) Name() string { return types.ModuleName }

// RegisterLegacyAminoCodec registers the customparams module's types on the LegacyAmino codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// DefaultGenesis returns default genesis state as raw bytes for the customparams
// module.
func (amb AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesisState())
}

// ValidateGenesis performs genesis state validation for the customparams module.
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var genesis types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &genesis); err != nil {
		return errors.Wrapf(err, "failed to unmarshal %s genesis state", types.ModuleName)
	}
	return genesis.Validate()
}

// RegisterRESTRoutes registers the REST routes for the customparams module.
func (AppModuleBasic) RegisterRESTRoutes(clientCtx client.Context, rtr *mux.Router) {}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the customparams module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// GetTxCmd returns the root tx command for the customparams module.
func (AppModuleBasic) GetTxCmd() *cobra.Command {
	return nil
}

// GetQueryCmd returns no root query command for the customparams module.
func (AppModuleBasic) GetQueryCmd() *cobra.Command {
	return nil
}

// RegisterInterfaces registers interfaces and implementations of the customparams module.
func (AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// AppModule implements an application module for the customparams module.
type AppModule struct {
	AppModuleBasic

	keeper       keeper.Keeper
	paramsKeeper types.ParamsKeeper
}

// NewAppModule creates a new AppModule object.
func NewAppModule(keeper keeper.Keeper, paramsKeeper types.ParamsKeeper) AppModule {
	return AppModule{
		keeper:       keeper,
		paramsKeeper: paramsKeeper,
	}
}

// RegisterServices registers module services.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServer(am.keeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryService(am.keeper))

	m := keeper.NewMigrator(am.keeper, am.paramsKeeper)
	if err := cfg.RegisterMigration(types.ModuleName, 1, m.Migrate1to2); err != nil {
		panic(errors.Errorf("can't register module %s migrations, err: %s", types.ModuleName, err))
	}
}

// Name returns the customparams module's name.
func (AppModule) Name() string { return types.ModuleName }

// IsAppModule implements the appmodule.AppModule interface.
func (am AppModule) IsAppModule() {}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (am AppModule) IsOnePerModuleType() {}

// RegisterInvariants registers the customparams module invariants.
func (am AppModule) RegisterInvariants(ir sdk.InvariantRegistry) {}

// QuerierRoute returns the customparams module's querier route name.
func (AppModule) QuerierRoute() string { return types.RouterKey }

// InitGenesis performs genesis initialization for the customparams module. It returns
// no validator updates.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) {
	genesis := &types.GenesisState{}
	cdc.MustUnmarshalJSON(data, genesis)

	am.keeper.InitGenesis(ctx, *genesis)
}

// ExportGenesis returns the exported genesis state as raw bytes for the customparams
// module.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	genState := am.keeper.ExportGenesis(ctx)
	return cdc.MustMarshalJSON(genState)
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return 2 }

// AppModuleSimulation functions

// GenerateGenesisState creates a randomized GenState of the customparams module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(types.DefaultGenesisState())
}

// RegisterStoreDecoder registers a decoder for supply module's types.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	return nil
}
