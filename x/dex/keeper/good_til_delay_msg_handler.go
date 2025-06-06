package keeper

import (
	sdkerrors "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmoserrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/gogoproto/proto"

	"github.com/CoreumFoundation/coreum/v6/x/dex/types"
)

// CancelOrderKeeper is keeper interface required for CancelGoodTil.
type CancelOrderKeeper interface {
	CancelOrderBySequence(ctx sdk.Context, acc sdk.AccAddress, orderSequence uint64) error
}

// NewDelayCancelOrderHandler handles order cancellation.
func NewDelayCancelOrderHandler(keeper CancelOrderKeeper) func(ctx sdk.Context, data proto.Message) error {
	return func(ctx sdk.Context, data proto.Message) error {
		msg, ok := data.(*types.CancelGoodTil)
		if !ok {
			return sdkerrors.Wrapf(types.ErrInvalidState, "unrecognized %s message type: %T", types.ModuleName, data)
		}
		sender, err := sdk.AccAddressFromBech32(msg.Creator)
		if err != nil {
			return sdkerrors.Wrap(cosmoserrors.ErrInvalidAddress, "invalid sender")
		}

		return keeper.CancelOrderBySequence(ctx, sender, msg.OrderSequence)
	}
}
