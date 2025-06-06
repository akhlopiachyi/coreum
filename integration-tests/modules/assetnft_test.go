//go:build integrationtests

package modules

import (
	"bytes"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/x/nft"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmoserrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govtypesv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/gogoproto/proto"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integrationtests "github.com/CoreumFoundation/coreum/v6/integration-tests"
	"github.com/CoreumFoundation/coreum/v6/pkg/client"
	"github.com/CoreumFoundation/coreum/v6/testutil/event"
	"github.com/CoreumFoundation/coreum/v6/testutil/integration"
	assetnfttypes "github.com/CoreumFoundation/coreum/v6/x/asset/nft/types"
)

// TestAssetNFTQueryParams queries parameters of asset/nft module.
func TestAssetNFTQueryParams(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)
	mintFee := chain.QueryAssetNFTParams(ctx, t).MintFee

	assert.Equal(t, chain.ChainSettings.Denom, mintFee.Denom)
}

// TestAssetNFTIssueClass tests non-fungible token class creation.
func TestAssetNFTIssueClass(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()

	assetNftClient := assetnfttypes.NewQueryClient(chain.ClientContext)

	// prepare issue message

	jsonData := []byte(`{"name": "Name", "description": "Description"}`)
	data, err := codectypes.NewAnyWithValue(&assetnfttypes.DataBytes{Data: jsonData})
	requireT.NoError(err)

	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer:      issuer.String(),
		Symbol:      "symbol",
		Name:        "name",
		Description: "description",
		URI:         "https://my-class-meta.invalid/1",
		URIHash:     "content-hash",
		Data:        data,
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_burning,
			assetnfttypes.ClassFeature_disable_sending,
		},
		RoyaltyRate: sdkmath.LegacyMustNewDecFromStr("0.1"),
	}

	chain.FundAccountWithOptions(ctx, t, issuer, integration.BalancesOptions{
		Messages: []sdk.Msg{
			issueMsg,
		},
	})

	// issue new NFT class with invalid data type

	invalidData, err := codectypes.NewAnyWithValue(&assetnfttypes.MsgMint{})
	requireT.NoError(err)

	invalidIssueMsg := &assetnfttypes.MsgIssueClass{
		Issuer:      issuer.String(),
		Symbol:      "symbol",
		Name:        "name",
		Description: "description",
		URI:         "https://my-class-meta.invalid/1",
		URIHash:     "content-hash",
		Data:        invalidData,
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_burning,
		},
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(invalidIssueMsg)),
		invalidIssueMsg,
	)
	requireT.True(assetnfttypes.ErrInvalidInput.Is(err))

	// issue new NFT class

	// we need to do this, otherwise assertion fails because some private fields are set differently
	dataToCompare := &codectypes.Any{
		TypeUrl: data.TypeUrl,
		Value:   data.Value,
	}

	res, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.NoError(err)
	requireT.Equal(chain.GasLimitByMsgs(issueMsg), uint64(res.GasUsed))
	tokenIssuedEvents, err := event.FindTypedEvents[*assetnfttypes.EventClassIssued](res.Events)
	requireT.NoError(err)
	issuedEvent := tokenIssuedEvents[0]

	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)
	requireT.Equal(&assetnfttypes.EventClassIssued{
		ID:          classID,
		Issuer:      issuer.String(),
		Symbol:      issueMsg.Symbol,
		Name:        issueMsg.Name,
		Description: issueMsg.Description,
		URI:         issueMsg.URI,
		URIHash:     issueMsg.URIHash,
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_burning,
			assetnfttypes.ClassFeature_disable_sending,
		},
		RoyaltyRate: sdkmath.LegacyMustNewDecFromStr("0.1"),
	}, issuedEvent)

	// query nft asset with features
	assetNftClassRes, err := assetNftClient.Class(ctx, &assetnfttypes.QueryClassRequest{
		Id: classID,
	})
	requireT.NoError(err)

	expectedClass := assetnfttypes.Class{
		Id:          classID,
		Issuer:      issuer.String(),
		Symbol:      issueMsg.Symbol,
		Name:        issueMsg.Name,
		Description: issueMsg.Description,
		URI:         issueMsg.URI,
		URIHash:     issueMsg.URIHash,
		Data:        dataToCompare,
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_burning,
			assetnfttypes.ClassFeature_disable_sending,
		},
		RoyaltyRate: sdkmath.LegacyMustNewDecFromStr("0.1"),
	}
	requireT.Equal(expectedClass, assetNftClassRes.Class)

	var data2 assetnfttypes.DataBytes
	requireT.NoError(proto.Unmarshal(assetNftClassRes.Class.Data.Value, &data2))

	requireT.JSONEq(string(jsonData), string(data2.Data))

	assetNftClassesRes, err := assetNftClient.Classes(ctx, &assetnfttypes.QueryClassesRequest{
		Issuer: issuer.String(),
	})
	requireT.NoError(err)
	requireT.Len(assetNftClassesRes.Classes, 1)
	requireT.Equal(uint64(1), assetNftClassesRes.Pagination.Total)
	requireT.Equal(expectedClass, assetNftClassesRes.Classes[0])
}

// TestAssetNFTUpdate tests non-fungible token update.
func TestAssetNFTUpdate(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()
	owner := chain.GenAccount()

	nftClient := nft.NewQueryClient(chain.ClientContext)

	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "symbol",
		Name:   "name",
		Data:   nil,
	}

	chain.FundAccountWithOptions(ctx, t, issuer, integration.BalancesOptions{
		Messages: []sdk.Msg{
			issueMsg,
		},
	})

	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.NoError(err)

	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)

	jsonData := []byte(`{"name": "Name", "description": "Description"}`)
	dataDynamic := assetnfttypes.DataDynamic{
		Items: []assetnfttypes.DataDynamicItem{
			{
				Editors: []assetnfttypes.DataEditor{
					assetnfttypes.DataEditor_owner,
				},
				Data: jsonData,
			},
		},
	}
	data, err := codectypes.NewAnyWithValue(&dataDynamic)
	requireT.NoError(err)

	mintMsg := &assetnfttypes.MsgMint{
		Sender:    issuer.String(),
		Recipient: owner.String(),
		ID:        "id-1",
		ClassID:   classID,
		Data:      data,
	}

	chain.FundAccountWithOptions(ctx, t, issuer, integration.BalancesOptions{
		Messages: []sdk.Msg{
			mintMsg,
		},
	})

	txRes, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)
	requireT.EqualValues(txRes.GasUsed, chain.GasLimitByMsgs(mintMsg))

	storedNFT, err := nftClient.NFT(ctx, &nft.QueryNFTRequest{
		ClassId: mintMsg.ClassID,
		Id:      mintMsg.ID,
	})
	requireT.NoError(err)

	var storedDataDynamic assetnfttypes.DataDynamic
	requireT.NoError(storedDataDynamic.Unmarshal(storedNFT.Nft.Data.Value))
	requireT.Equal(dataDynamic, storedDataDynamic)

	// update stored NFT
	msgUpdateData := &assetnfttypes.MsgUpdateData{
		Sender:  owner.String(),
		ClassID: mintMsg.ClassID,
		ID:      mintMsg.ID,
		Items: []assetnfttypes.DataDynamicIndexedItem{
			{
				Index: 0,
				Data:  []byte("new-data"),
			},
		},
	}
	chain.FundAccountWithOptions(ctx, t, owner, integration.BalancesOptions{
		Messages: []sdk.Msg{
			msgUpdateData,
		},
	})

	txRes, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(owner),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgUpdateData)),
		msgUpdateData,
	)
	requireT.NoError(err)
	requireT.EqualValues(txRes.GasUsed, chain.GasLimitByMsgs(msgUpdateData))

	storedNFT, err = nftClient.NFT(ctx, &nft.QueryNFTRequest{
		ClassId: mintMsg.ClassID,
		Id:      mintMsg.ID,
	})
	requireT.NoError(err)

	var updatedDataDynamic assetnfttypes.DataDynamic
	requireT.NoError(updatedDataDynamic.Unmarshal(storedNFT.Nft.Data.Value))
	requireT.Equal(string(msgUpdateData.Items[0].Data), string(updatedDataDynamic.Items[0].Data))

	// try to update from issuer
	msgUpdateData.Sender = issuer.String()
	chain.FundAccountWithOptions(ctx, t, issuer, integration.BalancesOptions{
		Messages: []sdk.Msg{
			msgUpdateData,
		},
	})
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgUpdateData)),
		msgUpdateData,
	)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)
}

// TestAssetNFTIssueClassInvalidFeatures tests non-fungible token class creation with invalid features.
func TestAssetNFTIssueClassInvalidFeatures(t *testing.T) {
	requireT := require.New(t)

	ctx, chain := integrationtests.NewCoreumTestingContext(t)
	issuer := chain.GenAccount()

	chain.FundAccountWithOptions(ctx, t, issuer, integration.BalancesOptions{
		Messages: []sdk.Msg{
			&assetnfttypes.MsgIssueClass{},
			&assetnfttypes.MsgIssueClass{},
		},
	})

	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer:      issuer.String(),
		Symbol:      "symbol",
		Name:        "name",
		Description: "description",
		URI:         "https://my-class-meta.invalid/1",
		URIHash:     "content-hash",
		RoyaltyRate: sdkmath.LegacyZeroDec(),
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_burning,
			assetnfttypes.ClassFeature_freezing,
			assetnfttypes.ClassFeature_whitelisting,
			assetnfttypes.ClassFeature_disable_sending,
			assetnfttypes.ClassFeature_burning,
		},
	}
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.ErrorContains(err, "duplicated features in the class features list")

	issueMsg = &assetnfttypes.MsgIssueClass{
		Issuer:      issuer.String(),
		Symbol:      "symbol",
		Name:        "name",
		Description: "description",
		URI:         "https://my-class-meta.invalid/1",
		URIHash:     "content-hash",
		RoyaltyRate: sdkmath.LegacyZeroDec(),
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_burning,
			100,
			assetnfttypes.ClassFeature_freezing,
			assetnfttypes.ClassFeature_whitelisting,
			assetnfttypes.ClassFeature_disable_sending,
		},
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.ErrorContains(err, "non-existing class feature provided")
}

// TestAssetNFTMintAndWhitelisting tests non-fungible token minting when whitelisting is required.
func TestAssetNFTMintAndWhitelisting(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()
	recipient := chain.GenAccount()

	nftClient := nft.NewQueryClient(chain.ClientContext)
	chain.FundAccountWithOptions(ctx, t, issuer, integration.BalancesOptions{
		Messages: []sdk.Msg{
			&assetnfttypes.MsgIssueClass{},
			&assetnfttypes.MsgMint{},
			&assetnfttypes.MsgAddToClassWhitelist{},
			&assetnfttypes.MsgMint{},
		},
		Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount,
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "NFTClassSymbol",
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_whitelisting,
		},
	}
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.NoError(err)

	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)

	// mint new token in that class - should fail
	mintMsg := &assetnfttypes.MsgMint{
		Sender:    issuer.String(),
		Recipient: recipient.String(),
		ID:        "id-1",
		ClassID:   classID,
		URI:       "https://my-class-meta.invalid/1",
		URIHash:   "content-hash",
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// whitelist class
	msgAddToWhitelist := &assetnfttypes.MsgAddToClassWhitelist{
		Sender:  issuer.String(),
		ClassID: classID,
		Account: recipient.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgAddToWhitelist)),
		msgAddToWhitelist,
	)
	requireT.NoError(err)

	// mint again - should succeed
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)

	// check the owner
	ownerRes, err := nftClient.Owner(ctx, &nft.QueryOwnerRequest{
		ClassId: classID,
		Id:      mintMsg.ID,
	})
	requireT.NoError(err)
	requireT.Equal(recipient.String(), ownerRes.Owner)
}

// TestAssetNFTMint tests non-fungible token minting.
func TestAssetNFTMint(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()
	recipient := chain.GenAccount()

	// prepare mint message
	jsonData := []byte(`{"name": "Name", "description": "Description"}`)
	data, err := codectypes.NewAnyWithValue(&assetnfttypes.DataBytes{Data: jsonData})
	requireT.NoError(err)

	mintMsg := &assetnfttypes.MsgMint{
		Sender:  issuer.String(),
		ID:      "id-1",
		URI:     "https://my-class-meta.invalid/1",
		URIHash: "content-hash",
		Data:    data,
	}

	nftClient := nft.NewQueryClient(chain.ClientContext)
	chain.FundAccountWithOptions(ctx, t, issuer, integration.BalancesOptions{
		Messages: []sdk.Msg{
			&assetnfttypes.MsgIssueClass{},
			mintMsg,
			&nft.MsgSend{},
			&assetnfttypes.MsgMint{},
		},
		Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount,
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "NFTClassSymbol",
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.NoError(err)

	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)

	// mint with invalid data type

	invalidData, err := codectypes.NewAnyWithValue(&assetnfttypes.MsgMint{})
	requireT.NoError(err)

	invalidMintMsg := &assetnfttypes.MsgMint{
		Sender:  issuer.String(),
		ID:      "id-1",
		ClassID: classID,
		URI:     "https://my-class-meta.invalid/1",
		URIHash: "content-hash",
		Data:    invalidData,
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(invalidMintMsg)),
		invalidMintMsg,
	)
	requireT.True(assetnfttypes.ErrInvalidInput.Is(err))

	// mint new token in that class

	// we need to do this, otherwise assertion fails because some private fields are set differently
	dataToCompare := &codectypes.Any{
		TypeUrl: data.TypeUrl,
		Value:   data.Value,
	}

	mintMsg.ClassID = classID
	res, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)
	requireT.Equal(chain.GasLimitByMsgs(mintMsg), uint64(res.GasUsed))

	nftMintedEvents, err := event.FindTypedEvents[*nft.EventMint](res.Events)
	requireT.NoError(err)
	nftMintedEvent := nftMintedEvents[0]
	requireT.Equal(&nft.EventMint{
		ClassId: classID,
		Id:      mintMsg.ID,
		Owner:   issuer.String(),
	}, nftMintedEvent)

	// check that token is present in the nft module
	nftRes, err := nftClient.NFT(ctx, &nft.QueryNFTRequest{
		ClassId: classID,
		Id:      nftMintedEvent.Id,
	})
	requireT.NoError(err)
	requireT.Equal(&nft.NFT{
		ClassId: classID,
		Id:      mintMsg.ID,
		Uri:     mintMsg.URI,
		UriHash: mintMsg.URIHash,
		Data:    dataToCompare,
	}, nftRes.Nft)

	var data2 assetnfttypes.DataBytes
	requireT.NoError(proto.Unmarshal(nftRes.Nft.Data.Value, &data2))

	requireT.JSONEq(string(jsonData), string(data2.Data))

	// check the owner
	ownerRes, err := nftClient.Owner(ctx, &nft.QueryOwnerRequest{
		ClassId: classID,
		Id:      nftMintedEvent.Id,
	})
	requireT.NoError(err)
	requireT.Equal(issuer.String(), ownerRes.Owner)

	// change the owner
	sendMsg := &nft.MsgSend{
		Sender:   issuer.String(),
		Receiver: recipient.String(),
		Id:       mintMsg.ID,
		ClassId:  classID,
	}
	res, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.NoError(err)
	requireT.Equal(chain.GasLimitByMsgs(sendMsg), uint64(res.GasUsed))
	nftSentEvents, err := event.FindTypedEvents[*nft.EventSend](res.Events)
	requireT.NoError(err)
	nftSentEvent := nftSentEvents[0]
	requireT.Equal(&nft.EventSend{
		Sender:   sendMsg.Sender,
		Receiver: sendMsg.Receiver,
		ClassId:  sendMsg.ClassId,
		Id:       sendMsg.Id,
	}, nftSentEvent)

	// check new owner
	ownerRes, err = nftClient.Owner(ctx, &nft.QueryOwnerRequest{
		ClassId: classID,
		Id:      nftMintedEvent.Id,
	})
	requireT.NoError(err)
	requireT.Equal(recipient.String(), ownerRes.Owner)

	// mint to recipient

	mintMsg = &assetnfttypes.MsgMint{
		Sender:    issuer.String(),
		Recipient: recipient.String(),
		ID:        "id-2",
		ClassID:   classID,
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)

	ownerRes, err = nftClient.Owner(ctx, &nft.QueryOwnerRequest{
		ClassId: classID,
		Id:      mintMsg.ID,
	})
	requireT.NoError(err)
	requireT.Equal(recipient.String(), ownerRes.Owner)

	// check that balance is 0 meaning mint fee was taken

	bankClient := banktypes.NewQueryClient(chain.ClientContext)
	resp, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: issuer.String(),
		Denom:   chain.ChainSettings.Denom,
	})
	requireT.NoError(err)
	requireT.Equal(chain.NewCoin(sdkmath.ZeroInt()).String(), resp.Balance.String())
}

// TestAssetNFTWithLongData tests that class and NFT with long data works.
func TestAssetNFTWithLongData(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()

	// func account initially to create it on chain
	chain.FundAccountWithOptions(ctx, t, issuer, integration.BalancesOptions{
		Messages: []sdk.Msg{
			&assetnfttypes.MsgIssueClass{},
			&assetnfttypes.MsgMint{},
		},
		// minting fee and some tokens on top to cover the extra bytes
		Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount.AddRaw(100_000),
	})

	authParams, err := authtypes.NewQueryClient(chain.ClientContext).Params(ctx, &authtypes.QueryParamsRequest{})
	require.NoError(t, err)

	data, err := codectypes.NewAnyWithValue(
		&assetnfttypes.DataBytes{Data: bytes.Repeat([]byte{0x01}, 6000)},
	) // 3 bytes added by Any
	requireT.NoError(err)

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "NFTClassSymbol",
		Data:   data,
	}

	txBytes, err := chain.BuildSignedTx(ctx, chain.TxFactoryAuto(), issuer, issueMsg)
	requireT.NoError(err)
	extraBytes := uint64(len(txBytes)) - chain.DeterministicGasConfig.FreeBytes

	resp, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)+extraBytes*authParams.Params.TxSizeCostPerByte),
		issueMsg,
	)
	requireT.NoError(err)
	requireT.EqualValues(len(resp.Tx.GetValue()), chain.DeterministicGasConfig.FreeBytes+extraBytes)

	mintMsg := &assetnfttypes.MsgMint{
		Sender:  issuer.String(),
		ClassID: assetnfttypes.BuildClassID(issueMsg.Symbol, issuer),
		ID:      "id-1",
		Data:    data,
	}

	txBytes, err = chain.BuildSignedTx(ctx, chain.TxFactoryAuto(), issuer, mintMsg)
	requireT.NoError(err)
	extraBytes = uint64(len(txBytes)) - chain.DeterministicGasConfig.FreeBytes

	resp, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)+extraBytes*authParams.Params.TxSizeCostPerByte),
		mintMsg,
	)
	requireT.NoError(err)
	requireT.EqualValues(len(resp.Tx.GetValue()), chain.DeterministicGasConfig.FreeBytes+extraBytes)
}

// TestAssetNFTMintFeeProposal tests proposal upgrading mint fee.
func TestAssetNFTMintFeeProposal(t *testing.T) {
	// This test can't be run together with other tests because it affects balances due to unexpected issue fee.
	// That's why t.Parallel() is not here.

	ctx, chain := integrationtests.NewCoreumTestingContext(t)
	requireT := require.New(t)
	origParams := chain.QueryAssetNFTParams(ctx, t)
	newParams := origParams
	newParams.MintFee.Amount = sdkmath.OneInt()
	chain.Governance.ProposalFromMsgAndVote(
		ctx, t, nil,
		"-", "-", "-", govtypesv1.OptionYes,
		&assetnfttypes.MsgUpdateParams{
			Params:    newParams,
			Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		},
	)

	issuer := chain.GenAccount()
	chain.FundAccountWithOptions(ctx, t, issuer, integration.BalancesOptions{
		Messages: []sdk.Msg{
			&assetnfttypes.MsgIssueClass{},
			&assetnfttypes.MsgMint{},
		},
		Amount: sdkmath.OneInt(),
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "NFTClassSymbol",
	}
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.NoError(err)

	// mint new token in that class
	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)
	mintMsg := &assetnfttypes.MsgMint{
		Sender:  issuer.String(),
		ID:      "id-1",
		ClassID: classID,
		URI:     "https://my-class-meta.invalid/1",
		URIHash: "content-hash",
	}
	res, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)

	// verify issue fee was burnt

	burntStr, err := event.FindStringEventAttribute(res.Events, banktypes.EventTypeCoinBurn, sdk.AttributeKeyAmount)
	requireT.NoError(err)
	requireT.Equal(sdk.NewCoin(chain.ChainSettings.Denom, sdkmath.OneInt()).String(), burntStr)

	// check that balance is 0 meaning mint fee was taken

	bankClient := banktypes.NewQueryClient(chain.ClientContext)
	resp, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: issuer.String(),
		Denom:   chain.ChainSettings.Denom,
	})
	requireT.NoError(err)
	requireT.Equal(chain.NewCoin(sdkmath.ZeroInt()).String(), resp.Balance.String())

	// Revert to original mint fee
	chain.Governance.ProposalFromMsgAndVote(
		ctx, t, nil,
		"-", "-", "-", govtypesv1.OptionYes,
		&assetnfttypes.MsgUpdateParams{
			Params:    origParams,
			Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		},
	)
}

// TestAssetNFTBurn tests non-fungible token burning.
func TestAssetNFTBurn(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()

	nftClient := nft.NewQueryClient(chain.ClientContext)
	assetnftClient := assetnfttypes.NewQueryClient(chain.ClientContext)
	chain.FundAccountWithOptions(ctx, t, issuer, integration.BalancesOptions{
		Messages: []sdk.Msg{
			&assetnfttypes.MsgIssueClass{},
			&assetnfttypes.MsgMint{},
			&assetnfttypes.MsgBurn{},
			&assetnfttypes.MsgBurn{},
			&assetnfttypes.MsgMint{},
			&assetnfttypes.MsgMint{},
		},
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "NFTClassSymbol",
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_burning,
		},
	}
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.NoError(err)

	// mint new token in that class
	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)
	mintMsg := &assetnfttypes.MsgMint{
		Sender:  issuer.String(),
		ID:      "id-1",
		ClassID: classID,
	}
	res, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)
	requireT.Equal(chain.GasLimitByMsgs(mintMsg), uint64(res.GasUsed))

	// check that token is present in the nft module
	nftRes, err := nftClient.NFT(ctx, &nft.QueryNFTRequest{
		ClassId: classID,
		Id:      mintMsg.ID,
	})
	requireT.NoError(err)
	requireT.Equal(&nft.NFT{
		ClassId: classID,
		Id:      mintMsg.ID,
		Uri:     mintMsg.URI,
		UriHash: mintMsg.URIHash,
	}, nftRes.Nft)

	// burn the NFT
	msgBurn := &assetnfttypes.MsgBurn{
		Sender:  issuer.String(),
		ClassID: classID,
		ID:      "id-1",
	}
	res, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgBurn)),
		msgBurn,
	)
	requireT.NoError(err)
	requireT.Equal(chain.GasLimitByMsgs(msgBurn), uint64(res.GasUsed))

	// assert the burning event
	burnEvents, err := event.FindTypedEvents[*nft.EventBurn](res.Events)
	requireT.NoError(err)
	burnEvent := burnEvents[0]
	requireT.Equal(&nft.EventBurn{
		ClassId: classID,
		Id:      msgBurn.ID,
		Owner:   issuer.String(),
	}, burnEvent)

	// check that token isn't presented in the nft module anymore
	_, err = nftClient.NFT(ctx, &nft.QueryNFTRequest{
		ClassId: classID,
		Id:      mintMsg.ID,
	})
	requireT.Error(err)
	// the nft wraps the errors with the `errors` so the client doesn't decode them as sdk errors
	requireT.Contains(err.Error(), nft.ErrNFTNotExists.Error())

	// try to mint token with the same ID, should fail
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.Error(err)
	requireT.ErrorIs(err, assetnfttypes.ErrInvalidInput)

	// mint token with different ID, should succeed
	mintMsg.ID += "-2"
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)

	// burn the second NFT
	msgBurn = &assetnfttypes.MsgBurn{
		Sender:  issuer.String(),
		ClassID: classID,
		ID:      "id-1-2",
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgBurn)),
		msgBurn,
	)
	requireT.NoError(err)

	// query burnt NFTs
	burntListRes, err := assetnftClient.BurntNFTsInClass(ctx, &assetnfttypes.QueryBurntNFTsInClassRequest{
		Pagination: &query.PageRequest{},
		ClassId:    mintMsg.ClassID,
	})
	requireT.NoError(err)
	requireT.Len(burntListRes.NftIds, 2)

	// test pagination works
	burntListRes, err = assetnftClient.BurntNFTsInClass(ctx, &assetnfttypes.QueryBurntNFTsInClassRequest{
		Pagination: &query.PageRequest{
			Offset: 1,
		},
		ClassId: mintMsg.ClassID,
	})
	requireT.NoError(err)
	requireT.Len(burntListRes.NftIds, 1)

	// query is nft burnt
	burntNft, err := assetnftClient.BurntNFT(ctx, &assetnfttypes.QueryBurntNFTRequest{
		ClassId: mintMsg.ClassID,
		NftId:   "id-1",
	})
	requireT.NoError(err)
	requireT.True(burntNft.Burnt)
}

// TestAssetNFTBurnFrozen tests that frozen NFT cannot be burnt.
func TestAssetNFTBurnFrozen(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()
	recipient1 := chain.GenAccount()
	assetNFTClient := assetnfttypes.NewQueryClient(chain.ClientContext)

	chain.FundAccountsWithOptions(ctx, t, []integration.AccWithBalancesOptions{
		{
			Acc: issuer,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&assetnfttypes.MsgIssueClass{},
					&assetnfttypes.MsgMint{},
					&nft.MsgSend{},
					&assetnfttypes.MsgFreeze{},
					&assetnfttypes.MsgUnfreeze{},
				},
				Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount,
			},
		}, {
			Acc: recipient1,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&assetnfttypes.MsgBurn{},
					&assetnfttypes.MsgBurn{},
				},
			},
		},
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "NFTClassSymbol",
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_freezing,
			assetnfttypes.ClassFeature_burning,
		},
	}
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.NoError(err)

	// mint new token in that class
	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)
	nftID := "id-1" //nolint:goconst // repeating values in tests are ok
	mintMsg := &assetnfttypes.MsgMint{
		Sender:  issuer.String(),
		ID:      nftID,
		ClassID: classID,
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)

	// freeze the NFT
	msgFreeze := &assetnfttypes.MsgFreeze{
		Sender:  issuer.String(),
		ClassID: classID,
		ID:      nftID,
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgFreeze)),
		msgFreeze,
	)
	requireT.NoError(err)

	queryRes, err := assetNFTClient.Frozen(ctx, &assetnfttypes.QueryFrozenRequest{
		ClassId: classID,
		Id:      nftID,
	})
	requireT.NoError(err)
	requireT.True(queryRes.Frozen)

	// send from issuer to recipient1 (send is allowed)
	sendMsg := &nft.MsgSend{
		Sender:   issuer.String(),
		ClassId:  classID,
		Id:       nftID,
		Receiver: recipient1.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.NoError(err)

	// burn from recipient1 (burn is not allowed since it is frozen)
	burnMsg := &assetnfttypes.MsgBurn{
		Sender:  recipient1.String(),
		ClassID: classID,
		ID:      nftID,
	}

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient1),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(burnMsg)),
		burnMsg,
	)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// unfreeze the nft
	msgUnFreeze := &assetnfttypes.MsgUnfreeze{
		Sender:  issuer.String(),
		ClassID: classID,
		ID:      nftID,
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgUnFreeze)),
		msgUnFreeze,
	)
	requireT.NoError(err)

	// burn from recipient1 (it is now allowed)
	burnMsg = &assetnfttypes.MsgBurn{
		Sender:  recipient1.String(),
		ClassID: classID,
		ID:      nftID,
	}

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient1),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(burnMsg)),
		burnMsg,
	)
	requireT.NoError(err)
}

// TestAssetNFTBurnFrozen_Issuer tests that frozen NFT can be burnt by the issuer.
func TestAssetNFTBurnFrozen_Issuer(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()
	assetNFTClient := assetnfttypes.NewQueryClient(chain.ClientContext)
	nftClient := nft.NewQueryClient(chain.ClientContext)

	chain.FundAccountWithOptions(ctx, t, issuer, integration.BalancesOptions{
		Messages: []sdk.Msg{
			&assetnfttypes.MsgIssueClass{},
			&assetnfttypes.MsgMint{},
			&assetnfttypes.MsgFreeze{},
			&assetnfttypes.MsgBurn{},
		},
		Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount,
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "NFTClassSymbol",
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_freezing,
		},
	}
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.NoError(err)

	// mint new token in that class
	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)
	nftID := "id-1"
	mintMsg := &assetnfttypes.MsgMint{
		Sender:  issuer.String(),
		ID:      nftID,
		ClassID: classID,
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)

	// freeze the NFT
	msgFreeze := &assetnfttypes.MsgFreeze{
		Sender:  issuer.String(),
		ClassID: classID,
		ID:      nftID,
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgFreeze)),
		msgFreeze,
	)
	requireT.NoError(err)

	queryRes, err := assetNFTClient.Frozen(ctx, &assetnfttypes.QueryFrozenRequest{
		ClassId: classID,
		Id:      nftID,
	})
	requireT.NoError(err)
	requireT.True(queryRes.Frozen)

	_, err = nftClient.NFT(ctx, &nft.QueryNFTRequest{
		ClassId: classID,
		Id:      nftID,
	})
	requireT.NoError(err)

	// burn from issuer (burn is allowed even though nft is frozen)
	burnMsg := &assetnfttypes.MsgBurn{
		Sender:  issuer.String(),
		ClassID: classID,
		ID:      nftID,
	}

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(burnMsg)),
		burnMsg,
	)
	requireT.NoError(err)

	_, err = nftClient.NFT(ctx, &nft.QueryNFTRequest{
		ClassId: classID,
		Id:      nftID,
	})
	requireT.Error(err)
	requireT.Contains(err.Error(), "not found nft")
}

// TestAssetNFTClassFreeze tests non-fungible token class freezing.
func TestAssetNFTClassFreeze(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()
	recipient1 := chain.GenAccount()
	nftClient := assetnfttypes.NewQueryClient(chain.ClientContext)

	chain.FundAccountsWithOptions(ctx, t, []integration.AccWithBalancesOptions{
		{
			Acc: issuer,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&assetnfttypes.MsgIssueClass{},
					&assetnfttypes.MsgMint{},
					&nft.MsgSend{},
					&assetnfttypes.MsgClassFreeze{},
					&assetnfttypes.MsgClassUnfreeze{},
				},
				Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount,
			},
		}, {
			Acc: recipient1,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&nft.MsgSend{},
					&nft.MsgSend{},
					&nft.MsgSend{},
				},
			},
		},
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "NFTClassSymbol",
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_freezing,
		},
	}
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.NoError(err)

	// mint new token in that class
	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)
	nftID := "id-1"
	mintMsg := &assetnfttypes.MsgMint{
		Sender:    issuer.String(),
		ID:        nftID,
		ClassID:   classID,
		Recipient: recipient1.String(),
	}
	res, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)
	requireT.Equal(chain.GasLimitByMsgs(mintMsg), uint64(res.GasUsed))

	// class freeze the NFT
	msgFreeze := &assetnfttypes.MsgClassFreeze{
		Sender:  issuer.String(),
		ClassID: classID,
		Account: recipient1.String(),
	}
	res, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgFreeze)),
		msgFreeze,
	)
	requireT.NoError(err)
	requireT.Equal(int64(chain.GasLimitByMsgs(msgFreeze)), res.GasUsed)

	queryRes, err := nftClient.Frozen(ctx, &assetnfttypes.QueryFrozenRequest{
		ClassId: classID,
		Id:      nftID,
	})
	requireT.NoError(err)
	requireT.True(queryRes.Frozen)

	// assert the freezing event
	frozenEvents, err := event.FindTypedEvents[*assetnfttypes.EventClassFrozen](res.Events)
	requireT.NoError(err)
	frozenEvent := frozenEvents[0]
	requireT.Equal(&assetnfttypes.EventClassFrozen{
		ClassId: classID,
		Account: recipient1.String(),
	}, frozenEvent)

	// send from recipient1 to recipient2 (send is not allowed since it is frozen)
	recipient2 := chain.GenAccount()
	sendMsg := &nft.MsgSend{
		Sender:   recipient1.String(),
		ClassId:  classID,
		Id:       nftID,
		Receiver: recipient2.String(),
	}

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient1),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// send from recipient1 to issuer (send is not allowed since it is frozen)
	sendMsg = &nft.MsgSend{
		Sender:   recipient1.String(),
		ClassId:  classID,
		Id:       nftID,
		Receiver: issuer.String(),
	}

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient1),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.Error(err)
	requireT.True(cosmoserrors.ErrUnauthorized.Is(err))

	// unfreeze the NFT
	msgUnfreeze := &assetnfttypes.MsgClassUnfreeze{
		Sender:  issuer.String(),
		ClassID: classID,
		Account: recipient1.String(),
	}
	res, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgUnfreeze)),
		msgUnfreeze,
	)
	requireT.NoError(err)
	requireT.Equal(int64(chain.GasLimitByMsgs(msgUnfreeze)), res.GasUsed)

	queryRes, err = nftClient.Frozen(ctx, &assetnfttypes.QueryFrozenRequest{
		ClassId: classID,
		Id:      nftID,
	})
	requireT.NoError(err)
	requireT.False(queryRes.Frozen)

	// assert the unfreezing event
	unFrozenEvents, err := event.FindTypedEvents[*assetnfttypes.EventClassUnfrozen](res.Events)
	requireT.NoError(err)
	unfrozenEvent := unFrozenEvents[0]
	requireT.Equal(&assetnfttypes.EventClassUnfrozen{
		ClassId: classID,
		Account: recipient1.String(),
	}, unfrozenEvent)

	// send from recipient1 to recipient2 (send is allowed since it is not frozen)
	sendMsg = &nft.MsgSend{
		Sender:   recipient1.String(),
		ClassId:  classID,
		Id:       nftID,
		Receiver: recipient2.String(),
	}

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient1),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.NoError(err)
}

// TestAssetNFTFreeze tests non-fungible token freezing.
func TestAssetNFTFreeze(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()
	recipient1 := chain.GenAccount()
	nftClient := assetnfttypes.NewQueryClient(chain.ClientContext)

	chain.FundAccountsWithOptions(ctx, t, []integration.AccWithBalancesOptions{
		{
			Acc: issuer,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&assetnfttypes.MsgIssueClass{},
					&assetnfttypes.MsgMint{},
					&nft.MsgSend{},
					&assetnfttypes.MsgFreeze{},
					&assetnfttypes.MsgUnfreeze{},
				},
				Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount,
			},
		}, {
			Acc: recipient1,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&nft.MsgSend{},
					&nft.MsgSend{},
					&nft.MsgSend{},
				},
			},
		},
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "NFTClassSymbol",
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_freezing,
		},
	}
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.NoError(err)

	// mint new token in that class
	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)
	nftID := "id-1"
	mintMsg := &assetnfttypes.MsgMint{
		Sender:  issuer.String(),
		ID:      nftID,
		ClassID: classID,
	}
	res, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)
	requireT.Equal(chain.GasLimitByMsgs(mintMsg), uint64(res.GasUsed))

	// freeze the NFT
	msgFreeze := &assetnfttypes.MsgFreeze{
		Sender:  issuer.String(),
		ClassID: classID,
		ID:      nftID,
	}
	res, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgFreeze)),
		msgFreeze,
	)
	requireT.NoError(err)
	// requireT.Equal(chain.GasLimitByMsgs(msgFreeze), uint64(res.GasUsed))

	queryRes, err := nftClient.Frozen(ctx, &assetnfttypes.QueryFrozenRequest{
		ClassId: classID,
		Id:      nftID,
	})
	requireT.NoError(err)
	requireT.True(queryRes.Frozen)

	// assert the freezing event
	frozenEvents, err := event.FindTypedEvents[*assetnfttypes.EventFrozen](res.Events)
	requireT.NoError(err)
	frozenEvent := frozenEvents[0]
	requireT.Equal(&assetnfttypes.EventFrozen{
		ClassId: classID,
		Id:      msgFreeze.ID,
		Owner:   issuer.String(),
	}, frozenEvent)

	// send from issuer to recipient1 (send is allowed)
	sendMsg := &nft.MsgSend{
		Sender:   issuer.String(),
		ClassId:  classID,
		Id:       nftID,
		Receiver: recipient1.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.NoError(err)

	// send from recipient1 to recipient2 (send is not allowed since it is frozen)
	recipient2 := chain.GenAccount()
	sendMsg = &nft.MsgSend{
		Sender:   recipient1.String(),
		ClassId:  classID,
		Id:       nftID,
		Receiver: recipient2.String(),
	}

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient1),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.Error(err)
	requireT.True(cosmoserrors.ErrUnauthorized.Is(err))

	// send from recipient1 to issuer (send is not allowed since it is frozen)
	sendMsg = &nft.MsgSend{
		Sender:   recipient1.String(),
		ClassId:  classID,
		Id:       nftID,
		Receiver: issuer.String(),
	}

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient1),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.Error(err)
	requireT.True(cosmoserrors.ErrUnauthorized.Is(err))

	// unfreeze the NFT
	msgUnfreeze := &assetnfttypes.MsgUnfreeze{
		Sender:  issuer.String(),
		ClassID: classID,
		ID:      nftID,
	}
	res, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgUnfreeze)),
		msgUnfreeze,
	)
	requireT.NoError(err)
	requireT.EqualValues(chain.GasLimitByMsgs(msgUnfreeze), res.GasUsed)

	queryRes, err = nftClient.Frozen(ctx, &assetnfttypes.QueryFrozenRequest{
		ClassId: classID,
		Id:      nftID,
	})
	requireT.NoError(err)
	requireT.False(queryRes.Frozen)

	// assert the unfreezing event
	unFrozenEvents, err := event.FindTypedEvents[*assetnfttypes.EventUnfrozen](res.Events)
	requireT.NoError(err)
	unfrozenEvent := unFrozenEvents[0]
	requireT.Equal(&assetnfttypes.EventUnfrozen{
		ClassId: classID,
		Id:      msgFreeze.ID,
		Owner:   recipient1.String(),
	}, unfrozenEvent)

	// send from recipient1 to recipient2 (send is allowed since it is not frozen)
	sendMsg = &nft.MsgSend{
		Sender:   recipient1.String(),
		ClassId:  classID,
		Id:       nftID,
		Receiver: recipient2.String(),
	}

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient1),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.NoError(err)
}

// TestAssetNFTWhitelist tests non-fungible token whitelisting.
func TestAssetNFTWhitelist(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()
	recipient := chain.GenAccount()
	recipient2 := chain.GenAccount()
	nftClient := assetnfttypes.NewQueryClient(chain.ClientContext)

	chain.FundAccountsWithOptions(ctx, t, []integration.AccWithBalancesOptions{
		{
			Acc: issuer,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&assetnfttypes.MsgIssueClass{},
					&assetnfttypes.MsgMint{},
					&nft.MsgSend{},
					&assetnfttypes.MsgAddToWhitelist{},
					&nft.MsgSend{},
					&assetnfttypes.MsgAddToWhitelist{},
					&assetnfttypes.MsgRemoveFromWhitelist{},
					&assetnfttypes.MsgAddToWhitelist{},
				},
				Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount,
			},
		}, {
			Acc: recipient,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&nft.MsgSend{},
					&nft.MsgSend{},
				},
			},
		}, {
			Acc: recipient2,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&nft.MsgSend{},
					&nft.MsgSend{},
				},
			},
		},
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "NFTClassSymbol",
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_whitelisting,
		},
	}
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.NoError(err)

	// mint new token in that class
	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)
	nftID := "id-1"
	mintMsg := &assetnfttypes.MsgMint{
		Sender:  issuer.String(),
		ID:      nftID,
		ClassID: classID,
	}
	res, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)
	requireT.Equal(chain.GasLimitByMsgs(mintMsg), uint64(res.GasUsed))

	// send to non-whitelisted recipient (send must fail)
	sendMsg := &nft.MsgSend{
		Sender:   issuer.String(),
		ClassId:  classID,
		Id:       nftID,
		Receiver: recipient.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// whitelist recipient for the NFT
	msgAddToWhitelist := &assetnfttypes.MsgAddToWhitelist{
		Sender:  issuer.String(),
		ClassID: classID,
		ID:      nftID,
		Account: recipient.String(),
	}
	res, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgAddToWhitelist)),
		msgAddToWhitelist,
	)
	requireT.NoError(err)
	requireT.EqualValues(chain.GasLimitByMsgs(msgAddToWhitelist), res.GasUsed)

	// assert the query
	queryRes, err := nftClient.Whitelisted(ctx, &assetnfttypes.QueryWhitelistedRequest{
		ClassId: classID,
		Id:      nftID,
		Account: recipient.String(),
	})
	requireT.NoError(err)
	requireT.True(queryRes.Whitelisted)

	// assert the whitelisting event
	whitelistEvents, err := event.FindTypedEvents[*assetnfttypes.EventAddedToWhitelist](res.Events)
	requireT.NoError(err)
	whitelistEvent := whitelistEvents[0]
	requireT.Equal(&assetnfttypes.EventAddedToWhitelist{
		ClassId: classID,
		Id:      msgAddToWhitelist.ID,
		Account: recipient.String(),
	}, whitelistEvent)

	// try to send again and it should succeed now.
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.NoError(err)

	// send from whitelisted recipient to non-whitelisted recipient2 (send must fail)
	sendMsg = &nft.MsgSend{
		Sender:   recipient.String(),
		ClassId:  classID,
		Id:       nftID,
		Receiver: recipient2.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// whitelist recipient2 for the NFT
	msgAddToWhitelist = &assetnfttypes.MsgAddToWhitelist{
		Sender:  issuer.String(),
		ClassID: classID,
		ID:      nftID,
		Account: recipient2.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgAddToWhitelist)),
		msgAddToWhitelist,
	)
	requireT.NoError(err)

	// try to send again from recipient to recipient2 and it should succeed now.
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.NoError(err)

	// unwhitelist the account
	msgRemoveFromWhitelist := &assetnfttypes.MsgRemoveFromWhitelist{
		Sender:  issuer.String(),
		ClassID: classID,
		ID:      nftID,
		Account: recipient.String(),
	}
	res, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgRemoveFromWhitelist)),
		msgRemoveFromWhitelist,
	)
	requireT.NoError(err)
	requireT.EqualValues(chain.GasLimitByMsgs(msgRemoveFromWhitelist), res.GasUsed)

	queryRes, err = nftClient.Whitelisted(ctx, &assetnfttypes.QueryWhitelistedRequest{
		ClassId: classID,
		Id:      nftID,
		Account: recipient.String(),
	})
	requireT.NoError(err)
	requireT.False(queryRes.Whitelisted)

	// assert the unwhitelisting event
	unwhitelistedEvents, err := event.FindTypedEvents[*assetnfttypes.EventRemovedFromWhitelist](res.Events)
	requireT.NoError(err)
	unwhitelistedEvent := unwhitelistedEvents[0]
	requireT.Equal(&assetnfttypes.EventRemovedFromWhitelist{
		ClassId: classID,
		Id:      msgAddToWhitelist.ID,
		Account: recipient.String(),
	}, unwhitelistedEvent)

	// try to send back from recipient2 to non-whitelisted recipient (send should fail)
	sendMsg = &nft.MsgSend{
		Sender:   recipient2.String(),
		ClassId:  classID,
		Id:       nftID,
		Receiver: recipient.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient2),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// whitelisting issuer should fail
	msgAddToWhitelist = &assetnfttypes.MsgAddToWhitelist{
		Sender:  issuer.String(),
		ClassID: classID,
		ID:      nftID,
		Account: issuer.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgAddToWhitelist)),
		msgAddToWhitelist,
	)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// sending to issuer should succeed
	sendMsg = &nft.MsgSend{
		Sender:   recipient2.String(),
		ClassId:  classID,
		Id:       nftID,
		Receiver: issuer.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient2),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.NoError(err)
}

// TestAssetNFTAuthZ tests that assetnft module works seamlessly with authz module.
func TestAssetNFTAuthZ(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	granter := chain.GenAccount()
	grantee := chain.GenAccount()
	nftClient := assetnfttypes.NewQueryClient(chain.ClientContext)

	chain.FundAccountsWithOptions(ctx, t, []integration.AccWithBalancesOptions{
		{
			Acc: granter,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&assetnfttypes.MsgIssueClass{},
					&assetnfttypes.MsgMint{},
					&authztypes.MsgGrant{},
				},
				Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount,
			},
		}, {
			Acc: grantee,
			Options: integration.BalancesOptions{
				Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount.
					Add(sdkmath.NewInt(20_000)),
			},
		},
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: granter.String(),
		Symbol: "NFTClassSymbol",
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_freezing, //nolint:nosnakecase // generated variable
		},
	}

	// mint new token in that class
	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, granter)
	nftID := "id-1"
	mintMsg := &assetnfttypes.MsgMint{
		Sender:  granter.String(),
		ID:      nftID,
		ClassID: classID,
	}

	// grant authorization to freeze nft
	grantMsg, err := authztypes.NewMsgGrant(
		granter,
		grantee,
		authztypes.NewGenericAuthorization(sdk.MsgTypeURL(&assetnfttypes.MsgFreeze{})),
		lo.ToPtr(time.Now().Add(time.Minute)),
	)
	requireT.NoError(err)

	msgList := []sdk.Msg{issueMsg, mintMsg, grantMsg}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(granter),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgList...)),
		msgList...,
	)
	requireT.NoError(err)

	// whitelist recipient for the NFT
	freezeMsg := &assetnfttypes.MsgFreeze{
		Sender:  granter.String(),
		ClassID: classID,
		ID:      nftID,
	}
	execMsg := authztypes.NewMsgExec(grantee, []sdk.Msg{freezeMsg})

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(grantee),
		chain.TxFactoryAuto(),
		&execMsg,
	)
	requireT.NoError(err)

	// assert the query
	queryRes, err := nftClient.Frozen(ctx, &assetnfttypes.QueryFrozenRequest{
		ClassId: classID,
		Id:      nftID,
	})
	requireT.NoError(err)
	requireT.True(queryRes.Frozen)
}

// TestAssetNFTClassWhitelist tests nft class whitelisting.
func TestAssetNFTClassWhitelist(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()
	recipient := chain.GenAccount()
	recipient2 := chain.GenAccount()
	nftClient := assetnfttypes.NewQueryClient(chain.ClientContext)

	chain.FundAccountsWithOptions(ctx, t, []integration.AccWithBalancesOptions{
		{
			Acc: issuer,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&assetnfttypes.MsgIssueClass{},
					&assetnfttypes.MsgMint{},
					&assetnfttypes.MsgMint{},
					&assetnfttypes.MsgAddToClassWhitelist{},
					&assetnfttypes.MsgAddToClassWhitelist{},
					&assetnfttypes.MsgAddToClassWhitelist{},
					&assetnfttypes.MsgAddToClassWhitelist{},
					&assetnfttypes.MsgRemoveFromClassWhitelist{},
					&nft.MsgSend{},
					&nft.MsgSend{},
					&nft.MsgSend{},
				},
				Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount,
			},
		}, {
			Acc: recipient,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&nft.MsgSend{},
					&nft.MsgSend{},
				},
			},
		}, {
			Acc: recipient2,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&nft.MsgSend{},
					&nft.MsgSend{},
				},
			},
		},
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "NFTClassSymbol",
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_whitelisting,
		},
	}
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg)),
		issueMsg,
	)
	requireT.NoError(err)

	// mint new token in that class
	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)
	nftID1 := "id-1"
	mintMsg := &assetnfttypes.MsgMint{
		Sender:  issuer.String(),
		ID:      nftID1,
		ClassID: classID,
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)

	// send to non-whitelisted recipient (send must fail)
	sendMsg := &nft.MsgSend{
		Sender:   issuer.String(),
		ClassId:  classID,
		Id:       nftID1,
		Receiver: recipient.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// whitelist recipient for the NFT Class
	msgAddToWhitelist := &assetnfttypes.MsgAddToClassWhitelist{
		Sender:  issuer.String(),
		ClassID: classID,
		Account: recipient.String(),
	}
	res, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgAddToWhitelist)),
		msgAddToWhitelist,
	)
	requireT.NoError(err)
	requireT.EqualValues(chain.GasLimitByMsgs(msgAddToWhitelist), res.GasUsed)

	// assert the query
	queryRes, err := nftClient.Whitelisted(ctx, &assetnfttypes.QueryWhitelistedRequest{
		ClassId: classID,
		Id:      nftID1,
		Account: recipient.String(),
	})
	requireT.NoError(err)
	requireT.True(queryRes.Whitelisted)

	// assert the whitelisting event
	whitelistEvents, err := event.FindTypedEvents[*assetnfttypes.EventAddedToClassWhitelist](res.Events)
	requireT.NoError(err)
	whitelistEvent := whitelistEvents[0]
	requireT.Equal(&assetnfttypes.EventAddedToClassWhitelist{
		ClassId: classID,
		Account: recipient.String(),
	}, whitelistEvent)

	// try to send again and it should succeed now.
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.NoError(err)

	// Mint new nft in class and it should now be whitelisted
	nftID2 := "id-2"
	mintMsg = &assetnfttypes.MsgMint{
		Sender:  issuer.String(),
		ID:      nftID2,
		ClassID: classID,
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(mintMsg)),
		mintMsg,
	)
	requireT.NoError(err)
	// already whitelisted for the class-whitelisted account
	queryRes, err = nftClient.Whitelisted(ctx, &assetnfttypes.QueryWhitelistedRequest{
		ClassId: classID,
		Id:      nftID2,
		Account: recipient.String(),
	})
	requireT.NoError(err)
	requireT.True(queryRes.Whitelisted)

	// not whitelisted for a random account
	queryRes, err = nftClient.Whitelisted(ctx, &assetnfttypes.QueryWhitelistedRequest{
		ClassId: classID,
		Id:      nftID2,
		Account: chain.GenAccount().String(),
	})
	requireT.NoError(err)
	requireT.False(queryRes.Whitelisted)

	// send is allowed
	sendMsg.Id = nftID2
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.NoError(err)

	// send from whitelisted recipient to non-whitelisted recipient2 (send must fail)
	sendMsg = &nft.MsgSend{
		Sender:   recipient.String(),
		ClassId:  classID,
		Id:       nftID1,
		Receiver: recipient2.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// whitelist recipient2 for the NFT
	msgAddToWhitelist = &assetnfttypes.MsgAddToClassWhitelist{
		Sender:  issuer.String(),
		ClassID: classID,
		Account: recipient2.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgAddToWhitelist)),
		msgAddToWhitelist,
	)
	requireT.NoError(err)

	// try to send again from recipient to recipient2 and it should succeed now.
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.NoError(err)

	// unwhitelist the account
	msgRemoveFromWhitelist := &assetnfttypes.MsgRemoveFromClassWhitelist{
		Sender:  issuer.String(),
		ClassID: classID,
		Account: recipient.String(),
	}
	res, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgRemoveFromWhitelist)),
		msgRemoveFromWhitelist,
	)
	requireT.NoError(err)
	requireT.EqualValues(chain.GasLimitByMsgs(msgRemoveFromWhitelist), res.GasUsed)

	queryRes, err = nftClient.Whitelisted(ctx, &assetnfttypes.QueryWhitelistedRequest{
		ClassId: classID,
		Id:      nftID1,
		Account: recipient.String(),
	})
	requireT.NoError(err)
	requireT.False(queryRes.Whitelisted)

	// assert the unwhitelisting event
	unwhitelistedEvents, err := event.FindTypedEvents[*assetnfttypes.EventRemovedFromClassWhitelist](res.Events)
	requireT.NoError(err)
	unwhitelistedEvent := unwhitelistedEvents[0]
	requireT.Equal(&assetnfttypes.EventRemovedFromClassWhitelist{
		ClassId: classID,
		Account: recipient.String(),
	}, unwhitelistedEvent)

	// try to send back from recipient2 to non-whitelisted recipient (send should fail)
	sendMsg = &nft.MsgSend{
		Sender:   recipient2.String(),
		ClassId:  classID,
		Id:       nftID1,
		Receiver: recipient.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient2),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// whitelisting issuer should fail
	msgAddToWhitelist = &assetnfttypes.MsgAddToClassWhitelist{
		Sender:  issuer.String(),
		ClassID: classID,
		Account: issuer.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgAddToWhitelist)),
		msgAddToWhitelist,
	)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// sending to issuer should succeed
	sendMsg = &nft.MsgSend{
		Sender:   recipient2.String(),
		ClassId:  classID,
		Id:       nftID1,
		Receiver: issuer.String(),
	}
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient2),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.NoError(err)
}

func TestAssetNFTSoulbound(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()
	recipient := chain.GenAccount()

	chain.FundAccountsWithOptions(ctx, t, []integration.AccWithBalancesOptions{
		{
			Acc: issuer,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&assetnfttypes.MsgIssueClass{},
					&assetnfttypes.MsgMint{},
					&nft.MsgSend{},
				},
				Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount,
			},
		}, {
			Acc: recipient,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&nft.MsgSend{},
				},
			},
		},
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "NFTClassSymbol",
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_soulbound,
		},
	}

	// mint new token in that class
	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)
	nftID := "id-1"
	mintMsg1 := &assetnfttypes.MsgMint{
		Sender:  issuer.String(),
		ID:      nftID,
		ClassID: classID,
	}

	msgList := []sdk.Msg{issueMsg, mintMsg1}
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgList...)),
		msgList...,
	)
	requireT.NoError(err)

	// try to send from issuer to recipient (it is allowed)
	sendMsg := &nft.MsgSend{
		ClassId:  classID,
		Id:       nftID,
		Sender:   issuer.String(),
		Receiver: recipient.String(),
	}

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.NoError(err)

	// try to send from recipient to issuer (it is not allowed)
	sendMsg = &nft.MsgSend{
		ClassId:  classID,
		Id:       nftID,
		Sender:   recipient.String(),
		Receiver: issuer.String(),
	}

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)
}

func TestAssetNFTSoulbound_SendInsideMint(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	issuer := chain.GenAccount()
	recipient := chain.GenAccount()

	chain.FundAccountsWithOptions(ctx, t, []integration.AccWithBalancesOptions{
		{
			Acc: issuer,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&assetnfttypes.MsgIssueClass{},
					&assetnfttypes.MsgMint{},
					&nft.MsgSend{},
				},
				Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount,
			},
		}, {
			Acc: recipient,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&nft.MsgSend{},
				},
			},
		},
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer: issuer.String(),
		Symbol: "NFTClassSymbol",
		Features: []assetnfttypes.ClassFeature{
			assetnfttypes.ClassFeature_soulbound,
		},
	}

	// mint new token in that class
	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, issuer)
	nftID := "id-1"
	mintMsg1 := &assetnfttypes.MsgMint{
		Recipient: recipient.String(),
		Sender:    issuer.String(),
		ID:        nftID,
		ClassID:   classID,
	}

	msgList := []sdk.Msg{issueMsg, mintMsg1}
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(issuer),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgList...)),
		msgList...,
	)
	requireT.NoError(err)

	// try to send from recipient to issuer (it is not allowed)
	sendMsg := &nft.MsgSend{
		ClassId:  classID,
		Id:       nftID,
		Sender:   recipient.String(),
		Receiver: issuer.String(),
	}

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(recipient),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(sendMsg)),
		sendMsg,
	)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)
}

// TestAssetNFTSendAuthorization tests that assetnft SendAuthorization works as expected.
func TestAssetNFTSendAuthorization(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)
	granter := chain.GenAccount()
	grantee := chain.GenAccount()
	recipient := chain.GenAccount()
	nftClient := nft.NewQueryClient(chain.ClientContext)
	authzClient := authztypes.NewQueryClient(chain.ClientContext)

	chain.FundAccountsWithOptions(ctx, t, []integration.AccWithBalancesOptions{
		{
			Acc: granter,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					&assetnfttypes.MsgIssueClass{},
					&assetnfttypes.MsgMint{},
				},
				Amount: chain.QueryAssetNFTParams(ctx, t).MintFee.Amount.
					Add(sdkmath.NewInt(20_000)), // added 20k for the grant msg
			},
		}, {
			Acc: grantee,
			Options: integration.BalancesOptions{
				Amount: sdkmath.NewInt(1).Add(sdkmath.NewInt(40_000)),
			},
		},
	})

	// issue new NFT class
	issueMsg := &assetnfttypes.MsgIssueClass{
		Issuer:   granter.String(),
		Symbol:   "NFTClassSymbol",
		Features: []assetnfttypes.ClassFeature{},
	}

	// mint new token in that class
	classID := assetnfttypes.BuildClassID(issueMsg.Symbol, granter)
	nftID := "id-1"
	mintMsg1 := &assetnfttypes.MsgMint{
		Sender:  granter.String(),
		ID:      nftID,
		ClassID: classID,
	}

	msgList := []sdk.Msg{issueMsg, mintMsg1}
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(granter),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msgList...)),
		msgList...,
	)
	requireT.NoError(err)

	// try to send before grant
	sendMsg := &nft.MsgSend{
		ClassId:  classID,
		Id:       nftID,
		Sender:   granter.String(),
		Receiver: recipient.String(),
	}
	execMsg := authztypes.NewMsgExec(grantee, []sdk.Msg{sendMsg})

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(grantee),
		chain.TxFactory().WithGas(200_000),
		&execMsg,
	)
	requireT.Error(err)
	requireT.ErrorIs(err, authztypes.ErrNoAuthorizationFound)

	// grant authorization to send nft
	grantMsg, err := authztypes.NewMsgGrant(
		granter,
		grantee,
		assetnfttypes.NewSendAuthorization([]assetnfttypes.NFTIdentifier{
			{ClassId: classID, Id: nftID},
			{ClassId: classID, Id: "not-minted-yet"},
		}),
		lo.ToPtr(time.Now().Add(time.Minute)),
	)
	requireT.NoError(err)
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(granter),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(grantMsg)),
		grantMsg,
	)
	requireT.NoError(err)

	// assert granted
	gransRes, err := authzClient.Grants(ctx, &authztypes.QueryGrantsRequest{
		Granter: granter.String(),
		Grantee: grantee.String(),
	})
	requireT.NoError(err)
	requireT.Len(gransRes.Grants, 1)
	updatedGrant := assetnfttypes.SendAuthorization{}
	chain.ClientContext.Codec().MustUnmarshal(gransRes.Grants[0].Authorization.Value, &updatedGrant)
	requireT.ElementsMatch([]assetnfttypes.NFTIdentifier{
		{ClassId: classID, Id: nftID},
		{ClassId: classID, Id: "not-minted-yet"},
	}, updatedGrant.Nfts)

	// try to send after grant
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(grantee),
		chain.TxFactoryAuto(),
		&execMsg,
	)
	requireT.NoError(err)

	// assert transfer of ownership
	ownerResp, err := nftClient.Owner(ctx, &nft.QueryOwnerRequest{
		ClassId: classID,
		Id:      nftID,
	})
	requireT.NoError(err)
	requireT.Equal(ownerResp.Owner, recipient.String())

	// assert granted
	gransRes, err = authzClient.Grants(ctx, &authztypes.QueryGrantsRequest{
		Granter: granter.String(),
		Grantee: grantee.String(),
	})
	requireT.NoError(err)
	requireT.Len(gransRes.Grants, 1)
	updatedGrant = assetnfttypes.SendAuthorization{}
	chain.ClientContext.Codec().MustUnmarshal(gransRes.Grants[0].Authorization.Value, &updatedGrant)
	requireT.ElementsMatch([]assetnfttypes.NFTIdentifier{
		{ClassId: classID, Id: "not-minted-yet"},
	}, updatedGrant.Nfts)
}
