package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/CoreumFoundation/coreum/v6/x/customparams/types"
)

// QueryKeeper defines subscope of keeper methods required by query service.
type QueryKeeper interface {
	GetStakingParams(ctx sdk.Context) (types.StakingParams, error)
}

// QueryService serves grpc requests for the model.
type QueryService struct {
	keeper QueryKeeper
}

// NewQueryService creates query service.
func NewQueryService(keeper QueryKeeper) QueryService {
	return QueryService{
		keeper: keeper,
	}
}

// StakingParams returns staking params of the model.
func (qs QueryService) StakingParams(
	ctx context.Context,
	req *types.QueryStakingParamsRequest,
) (*types.QueryStakingParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	params, err := qs.keeper.GetStakingParams(sdk.UnwrapSDKContext(ctx))
	if err != nil {
		return nil, err
	}
	return &types.QueryStakingParamsResponse{Params: params}, nil
}
