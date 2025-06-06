package keeper

import (
	"context"

	sdkerrors "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	wstakingtypes "github.com/CoreumFoundation/coreum/v6/x/wstaking/types"
)

// MsgServer is wrapper staking customParamsKeeper message server.
type MsgServer struct {
	stakingtypes.MsgServer
	customParamsKeeper wstakingtypes.CustomParamsKeeper
}

// NewMsgServerImpl returns an implementation of the staking wrapped MsgServer.
func NewMsgServerImpl(
	stakingMsgSrv stakingtypes.MsgServer, customParamsKeeper wstakingtypes.CustomParamsKeeper,
) stakingtypes.MsgServer {
	return MsgServer{
		MsgServer:          stakingMsgSrv,
		customParamsKeeper: customParamsKeeper,
	}
}

// CreateValidator defines wrapped method for creating a new validator.
func (s MsgServer) CreateValidator(
	goCtx context.Context, msg *stakingtypes.MsgCreateValidator,
) (*stakingtypes.MsgCreateValidatorResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	params, err := s.customParamsKeeper.GetStakingParams(ctx)
	if err != nil {
		return nil, err
	}
	expectedMinSelfDelegation := params.MinSelfDelegation
	if expectedMinSelfDelegation.GT(msg.MinSelfDelegation) {
		return nil, sdkerrors.Wrapf(
			stakingtypes.ErrSelfDelegationBelowMinimum,
			"min self delegation must be greater than or equal to global min self delegation: %s",
			msg.MinSelfDelegation,
		)
	}

	return s.MsgServer.CreateValidator(goCtx, msg)
}
