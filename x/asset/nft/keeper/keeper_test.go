package keeper_test

import (
	"sort"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmoserrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum/v6/pkg/config/constant"
	"github.com/CoreumFoundation/coreum/v6/testutil/event"
	"github.com/CoreumFoundation/coreum/v6/testutil/simapp"
	"github.com/CoreumFoundation/coreum/v6/x/asset/nft/keeper"
	"github.com/CoreumFoundation/coreum/v6/x/asset/nft/types"
)

func TestKeeper_IssueClass(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	nftKeeper := testApp.AssetNFTKeeper

	addr := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())

	settings := types.IssueClassSettings{
		Issuer:      addr,
		Name:        "name",
		Symbol:      "Symbol",
		Description: "description",
		URI:         "https://my-class-meta.invalid/1",
		URIHash:     "content-hash",
		Data:        genNFTData(requireT),
		Features: []types.ClassFeature{
			types.ClassFeature_burning,
		},
	}

	classID, err := nftKeeper.IssueClass(ctx, settings)
	requireT.NoError(err)
	requireT.Equal(strings.ToLower(settings.Symbol)+"-"+addr.String(), classID)

	nativeNFTClass, found := testApp.NFTKeeper.GetClass(ctx, classID)
	requireT.True(found)
	// we check line by line because of the data field
	requireT.Equal(settings.Name, nativeNFTClass.Name)
	requireT.Equal(settings.Symbol, nativeNFTClass.Symbol)
	requireT.Equal(settings.Description, nativeNFTClass.Description)
	requireT.Equal(settings.URI, nativeNFTClass.Uri)
	requireT.Equal(settings.URIHash, nativeNFTClass.UriHash)
	requireT.Equal(string(settings.Data.Value), string(nativeNFTClass.Data.Value))

	nftClass, err := nftKeeper.GetClass(ctx, classID)
	requireT.NoError(err)

	// we check line by line because of the data field
	requireClassSettingsEqualClass(requireT, settings, nftClass)

	// try to duplicate
	settings.Symbol = "SYMBOL"
	_, err = nftKeeper.IssueClass(ctx, settings)
	requireT.ErrorIs(err, types.ErrInvalidInput)

	// try to get non-valid class
	_, err = nftKeeper.GetClass(ctx, "invalid")
	requireT.ErrorIs(err, types.ErrInvalidInput)

	// try to get nonexistent class
	_, err = nftKeeper.GetClass(ctx, types.BuildClassID("nonexistent", addr))
	requireT.ErrorIs(err, types.ErrClassNotFound)

	// try to create class containing non-existing feature
	settings.Symbol = "symbol2"
	settings.Features = append(settings.Features, 10000)
	_, err = nftKeeper.IssueClass(ctx, settings)
	requireT.ErrorIs(err, types.ErrInvalidInput)
}

func TestKeeper_GetClasses(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	nftKeeper := testApp.AssetNFTKeeper

	issuer1 := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	issuer2 := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())

	settings := types.IssueClassSettings{
		Issuer:      issuer1,
		Name:        "name",
		Symbol:      "Symbol1",
		Description: "description",
		URI:         "https://my-class-meta.invalid/1",
		URIHash:     "content-hash",
		Data:        genNFTData(requireT),
		Features: []types.ClassFeature{
			types.ClassFeature_burning,
		},
	}

	issuer1Settings1 := settings

	issuer2Settings2 := settings
	issuer2Settings2.Issuer = issuer2
	issuer2Settings2.Symbol = "Symbol2"

	issuer2Settings3 := settings
	issuer2Settings3.Issuer = issuer2
	issuer2Settings3.Symbol = "Symbol3"

	allSettings := []types.IssueClassSettings{
		issuer1Settings1, issuer2Settings2, issuer2Settings3,
	}

	for _, issueSettings := range allSettings {
		_, err := nftKeeper.IssueClass(ctx, issueSettings)
		requireT.NoError(err)
	}

	// get all classes without the issuer
	classes, _, err := nftKeeper.GetClasses(ctx, nil, &query.PageRequest{Limit: query.PaginationMaxLimit})
	requireT.NoError(err)
	requireT.Len(classes, len(allSettings))
	sort.Slice(classes, func(i, j int) bool {
		return classes[i].Symbol < classes[j].Symbol
	})

	for i := range classes {
		requireClassSettingsEqualClass(requireT, allSettings[i], classes[i])
	}

	// get issuer 2 classes
	classes, _, err = nftKeeper.GetClasses(ctx, &issuer2, &query.PageRequest{Limit: query.PaginationMaxLimit})
	requireT.NoError(err)
	requireT.Len(classes, 2)
	sort.Slice(classes, func(i, j int) bool {
		return classes[i].Symbol < classes[j].Symbol
	})

	issuer2Settings := []types.IssueClassSettings{
		issuer2Settings2, issuer2Settings3,
	}
	for i := range classes {
		requireClassSettingsEqualClass(requireT, issuer2Settings[i], classes[i])
	}
}

func TestKeeper_Mint(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	nftKeeper := testApp.AssetNFTKeeper
	bankKeeper := testApp.BankKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 10_000_000),
	}
	requireT.NoError(nftKeeper.SetParams(ctx, nftParams))

	addr := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	requireT.NoError(testApp.FundAccount(ctx, addr, sdk.NewCoins(nftParams.MintFee)))
	classSettings := types.IssueClassSettings{
		Issuer: addr,
		Symbol: "symbol",
	}

	classID, err := nftKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)
	requireT.Equal(classSettings.Symbol+"-"+addr.String(), classID)

	settings := types.MintSettings{
		Sender:    addr,
		Recipient: addr,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
		Data:      genNFTData(requireT),
	}

	// mint first NFT
	err = nftKeeper.Mint(ctx, settings)
	requireT.NoError(err)

	nft, found := testApp.NFTKeeper.GetNFT(ctx, classID, settings.ID)
	requireT.True(found)
	// we check line by line because of the data field
	requireT.Equal(settings.ClassID, nft.ClassId)
	requireT.Equal(settings.ID, nft.Id)
	requireT.Equal(settings.URI, nft.Uri)
	requireT.Equal(settings.URIHash, nft.UriHash)
	requireT.Equal(string(settings.Data.Value), string(nft.Data.Value))

	nftOwner := testApp.NFTKeeper.GetOwner(ctx, classID, settings.ID)
	requireT.Equal(addr, nftOwner)

	// verify issue fee was burnt

	burntStr, err := event.FindStringEventAttribute(
		ctx.EventManager().ABCIEvents(),
		banktypes.EventTypeCoinBurn,
		sdk.AttributeKeyAmount,
	)
	requireT.NoError(err)
	requireT.Equal(nftParams.MintFee.String(), burntStr)

	// check that balance is 0 meaning issue fee was taken

	balance := bankKeeper.GetBalance(ctx, addr, constant.DenomDev)
	requireT.Equal(sdkmath.ZeroInt().String(), balance.Amount.String())

	// mint second NFT with the same ID
	err = nftKeeper.Mint(ctx, settings)
	requireT.True(types.ErrInvalidInput.Is(err))

	// try to mint from non-issuer account
	settings.Sender = sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	err = nftKeeper.Mint(ctx, settings)
	requireT.True(cosmoserrors.ErrUnauthorized.Is(err))
}

func TestKeeper_UpdateData(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})

	nftKeeper := testApp.AssetNFTKeeper

	dataDynamic := types.DataDynamic{
		Items: []types.DataDynamicItem{
			{
				Editors: []types.DataEditor{
					types.DataEditor_admin,
				},
				Data: []byte(uuid.NewString()),
			},
			{
				Editors: []types.DataEditor{
					types.DataEditor_owner,
				},
				Data: []byte(uuid.NewString()),
			},
			{
				Editors: []types.DataEditor{
					types.DataEditor_admin,
					types.DataEditor_owner,
				},
				Data: []byte(uuid.NewString()),
			},
			{
				Editors: nil,
				Data:    []byte(uuid.NewString()),
			},
		},
	}

	tests := []struct {
		name          string
		initialData   *codectypes.Any
		itemsToUpdate []types.DataDynamicIndexedItem
		senderAddress func(issuer, admin sdk.AccAddress) sdk.AccAddress
		isClassFrozen bool
		isNFTFrozen   bool
		wantErr       error
		errorContains string
	}{
		{
			name:        "positive_admin_update",
			initialData: marshalDataToAny(requireT, &dataDynamic),
			itemsToUpdate: []types.DataDynamicIndexedItem{
				{
					Index: 0,
					Data:  []byte(uuid.NewString()),
				},
				{
					Index: 2,
					Data:  []byte(uuid.NewString()),
				},
			},
			senderAddress: func(admin, owner sdk.AccAddress) sdk.AccAddress {
				return admin
			},
		},
		{
			name:        "positive_owner_update",
			initialData: marshalDataToAny(requireT, &dataDynamic),
			itemsToUpdate: []types.DataDynamicIndexedItem{
				{
					Index: 1,
					Data:  []byte(uuid.NewString()),
				},
				{
					Index: 2,
					Data:  []byte(uuid.NewString()),
				},
			},
			senderAddress: func(admin, owner sdk.AccAddress) sdk.AccAddress {
				return owner
			},
		},
		{
			name:        "negative_initial_nil_data",
			initialData: nil,
			itemsToUpdate: []types.DataDynamicIndexedItem{
				{
					Index: 0,
					Data:  []byte(uuid.NewString()),
				},
			},
			senderAddress: func(admin, owner sdk.AccAddress) sdk.AccAddress {
				return owner
			},
			wantErr:       types.ErrInvalidInput,
			errorContains: "no data",
		},
		{
			name: "negative_not_dynamic_data",
			initialData: marshalDataToAny(requireT, &types.DataBytes{
				Data: []byte{},
			}),
			itemsToUpdate: []types.DataDynamicIndexedItem{
				{
					Index: 0,
					Data:  []byte(uuid.NewString()),
				},
			},
			senderAddress: func(admin, owner sdk.AccAddress) sdk.AccAddress {
				return owner
			},
			wantErr:       types.ErrInvalidInput,
			errorContains: "is not updatable",
		},
		{
			name:        "negative_frozen_nft",
			initialData: marshalDataToAny(requireT, &dataDynamic),
			itemsToUpdate: []types.DataDynamicIndexedItem{
				{
					Index: 0,
					Data:  []byte(uuid.NewString()),
				},
			},
			senderAddress: func(admin, owner sdk.AccAddress) sdk.AccAddress {
				return owner
			},
			isNFTFrozen:   true,
			wantErr:       cosmoserrors.ErrUnauthorized,
			errorContains: "frozen",
		},
		{
			name:        "negative_frozen_class",
			initialData: marshalDataToAny(requireT, &dataDynamic),
			itemsToUpdate: []types.DataDynamicIndexedItem{
				{
					Index: 0,
					Data:  []byte(uuid.NewString()),
				},
			},
			senderAddress: func(admin, owner sdk.AccAddress) sdk.AccAddress {
				return owner
			},
			isClassFrozen: true,
			wantErr:       cosmoserrors.ErrUnauthorized,
			errorContains: "frozen",
		},
		{
			name:        "negative_update_of_not_updatable_item",
			initialData: marshalDataToAny(requireT, &dataDynamic),
			itemsToUpdate: []types.DataDynamicIndexedItem{
				{
					Index: 3,
					Data:  []byte(uuid.NewString()),
				},
			},
			senderAddress: func(admin, owner sdk.AccAddress) sdk.AccAddress {
				return admin
			},
			wantErr:       types.ErrInvalidInput,
			errorContains: "not updatable",
		},
		{
			name:        "negative_owner_update_prohibited_item",
			initialData: marshalDataToAny(requireT, &dataDynamic),
			itemsToUpdate: []types.DataDynamicIndexedItem{
				{
					Index: 0,
					Data:  []byte(uuid.NewString()),
				},
			},
			senderAddress: func(admin, owner sdk.AccAddress) sdk.AccAddress {
				return owner
			},
			wantErr:       cosmoserrors.ErrUnauthorized,
			errorContains: "sender is not authorized to update the item",
		},
		{
			name:        "negative_admin_update_prohibited_item",
			initialData: marshalDataToAny(requireT, &dataDynamic),
			itemsToUpdate: []types.DataDynamicIndexedItem{
				{
					Index: 1,
					Data:  []byte(uuid.NewString()),
				},
			},
			senderAddress: func(admin, owner sdk.AccAddress) sdk.AccAddress {
				return admin
			},
			wantErr:       cosmoserrors.ErrUnauthorized,
			errorContains: "sender is not authorized to update the item",
		},
		{
			name:        "negative_random_acc_update_prohibited_item",
			initialData: marshalDataToAny(requireT, &dataDynamic),
			itemsToUpdate: []types.DataDynamicIndexedItem{
				{
					Index: 0,
					Data:  []byte(uuid.NewString()),
				},
			},
			senderAddress: func(admin, owner sdk.AccAddress) sdk.AccAddress {
				return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
			},
			wantErr:       cosmoserrors.ErrUnauthorized,
			errorContains: "sender is not authorized to update the item",
		},
		{
			name:        "negative_out_of_range",
			initialData: marshalDataToAny(requireT, &dataDynamic),
			itemsToUpdate: []types.DataDynamicIndexedItem{
				{
					Index: 4,
					Data:  []byte(uuid.NewString()),
				},
			},
			senderAddress: func(admin, owner sdk.AccAddress) sdk.AccAddress {
				return admin
			},
			wantErr:       types.ErrInvalidInput,
			errorContains: "out or range",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuer := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
			owner := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
			classSettings := types.IssueClassSettings{
				Issuer: issuer,
				Symbol: "symbol",
			}
			if tt.isClassFrozen || tt.isNFTFrozen {
				classSettings.Features = []types.ClassFeature{
					types.ClassFeature_freezing,
				}
			}

			classID, err := nftKeeper.IssueClass(ctx, classSettings)
			requireT.NoError(err)

			mintSettings := types.MintSettings{
				Sender:    issuer,
				Recipient: owner,
				ClassID:   classID,
				ID:        "my-id",
				URI:       "https://my-nft-meta.invalid/1",
				URIHash:   "content-hash",
				Data:      tt.initialData,
			}

			requireT.NoError(nftKeeper.Mint(ctx, mintSettings))
			nftBefore, found := testApp.NFTKeeper.GetNFT(ctx, classID, mintSettings.ID)
			requireT.True(found)

			sender := tt.senderAddress(issuer, owner)

			if tt.isClassFrozen {
				requireT.NoError(testApp.AssetNFTKeeper.ClassFreeze(ctx, issuer, sender, classID))
			}

			if tt.isNFTFrozen {
				requireT.NoError(testApp.AssetNFTKeeper.Freeze(ctx, issuer, classID, nftBefore.Id))
			}

			err = testApp.AssetNFTKeeper.UpdateData(ctx, sender, nftBefore.ClassId, nftBefore.Id, tt.itemsToUpdate)
			if tt.wantErr != nil {
				requireT.ErrorIs(tt.wantErr, err)
				requireT.ErrorContains(err, tt.errorContains)
				return
			}
			requireT.NoError(err)

			expectedDataDynamic := cloneDataDynamic(requireT, dataDynamic)
			for i := range expectedDataDynamic.Items {
				for _, itemToUpdate := range tt.itemsToUpdate {
					if int(itemToUpdate.Index) == i {
						expectedDataDynamic.Items[i].Data = itemToUpdate.Data
					}
				}
			}
			nftAfter, found := testApp.NFTKeeper.GetNFT(ctx, classID, mintSettings.ID)
			requireT.True(found)
			gotNFTData := unmarshalDataDynamic(requireT, nftAfter.Data)
			requireT.Equal(expectedDataDynamic, gotNFTData)
			requireT.Equal(nftBefore.ClassId, nftAfter.ClassId)
			requireT.Equal(nftBefore.Id, nftAfter.Id)
			requireT.Equal(nftBefore.Uri, nftAfter.Uri)
			requireT.Equal(nftBefore.UriHash, nftAfter.UriHash)
		})
	}
}

func TestKeeper_MintWithRecipient(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	nftKeeper := testApp.AssetNFTKeeper
	bankKeeper := testApp.BankKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 10_000_000),
	}
	requireT.NoError(nftKeeper.SetParams(ctx, nftParams))

	addr := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	randomAddr := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	requireT.NoError(testApp.FundAccount(ctx, addr, sdk.NewCoins(nftParams.MintFee)))
	classSettings := types.IssueClassSettings{
		Issuer: addr,
		Symbol: "symbol",
	}

	classID, err := nftKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)
	requireT.Equal(classSettings.Symbol+"-"+addr.String(), classID)

	settings := types.MintSettings{
		Sender:    addr,
		Recipient: randomAddr,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	// mint NFT
	err = nftKeeper.Mint(ctx, settings)
	requireT.NoError(err)

	nft, found := testApp.NFTKeeper.GetNFT(ctx, classID, settings.ID)
	requireT.True(found)
	// we check line by line because of the data field
	requireT.Equal(settings.ClassID, nft.ClassId)
	requireT.Equal(settings.ID, nft.Id)
	requireT.Equal(settings.URI, nft.Uri)
	requireT.Equal(settings.URIHash, nft.UriHash)

	nftOwner := testApp.NFTKeeper.GetOwner(ctx, classID, settings.ID)
	requireT.Equal(randomAddr, nftOwner)

	// verify issue fee was burnt

	burntStr, err := event.FindStringEventAttribute(
		ctx.EventManager().ABCIEvents(),
		banktypes.EventTypeCoinBurn,
		sdk.AttributeKeyAmount,
	)
	requireT.NoError(err)
	requireT.Equal(nftParams.MintFee.String(), burntStr)

	// check that balance is 0 meaning issue fee was taken

	balance := bankKeeper.GetBalance(ctx, addr, constant.DenomDev)
	requireT.Equal(sdkmath.ZeroInt().String(), balance.Amount.String())
}

func TestKeeper_MintWithRecipientAndWhitelisting(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	nftKeeper := testApp.AssetNFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 10_000_000),
	}
	requireT.NoError(nftKeeper.SetParams(ctx, nftParams))

	addr := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	randomAddr := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	requireT.NoError(testApp.FundAccount(ctx, addr, sdk.NewCoins(nftParams.MintFee)))
	classSettings := types.IssueClassSettings{
		Issuer: addr,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_whitelisting,
		},
	}

	classID, err := nftKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)
	requireT.Equal(classSettings.Symbol+"-"+addr.String(), classID)

	settings := types.MintSettings{
		Sender:    addr,
		Recipient: randomAddr,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	// mint NFT - should fail because recipient is not whitelisted
	requireT.ErrorIs(nftKeeper.Mint(ctx, settings), cosmoserrors.ErrUnauthorized)

	// whitelist for class
	requireT.NoError(nftKeeper.AddToClassWhitelist(ctx, classID, addr, randomAddr))

	// now minting should work
	requireT.NoError(nftKeeper.Mint(ctx, settings))

	// verify ownership
	requireT.Equal(randomAddr, testApp.NFTKeeper.GetOwner(ctx, classID, settings.ID))
}

func TestKeeper_Burn(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper
	nftKeeper := testApp.NFTKeeper

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())

	classSettings := types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_burning,
			types.ClassFeature_disable_sending,
		},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	nftID := "my-id"
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: issuer,
		ClassID:   classID,
		ID:        nftID,
	}

	// mint NFT
	err = assetNFTKeeper.Mint(ctx, settings)
	requireT.NoError(err)

	// try to burn non-existing nft
	err = assetNFTKeeper.Burn(ctx, issuer, classID, "invalid")
	requireT.ErrorIs(err, types.ErrNFTNotFound)

	// try to burn from not owner account
	err = assetNFTKeeper.Burn(ctx, recipient, classID, nftID)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// burn the nft
	err = assetNFTKeeper.Burn(ctx, issuer, classID, nftID)
	requireT.NoError(err)

	// try to burn the nft one more time
	err = assetNFTKeeper.Burn(ctx, issuer, classID, nftID)
	requireT.ErrorIs(err, types.ErrNFTNotFound)

	// mint the nft with the same ID (must fail)
	err = assetNFTKeeper.Mint(ctx, settings)
	requireT.Error(err)
	requireT.ErrorIs(err, types.ErrInvalidInput)

	// mint new NFT
	settings.ID += "-2"
	err = assetNFTKeeper.Mint(ctx, settings)
	requireT.NoError(err)

	err = nftKeeper.Transfer(ctx, settings.ClassID, settings.ID, recipient)
	requireT.NoError(err)

	// try burn the nft with the enabled feature from the recipient account
	err = assetNFTKeeper.Burn(ctx, recipient, classID, settings.ID)
	requireT.NoError(err)

	// issue class without burning feature
	classSettings = types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol2",
	}

	classID, err = assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	settings = types.MintSettings{
		Sender:    issuer,
		Recipient: issuer,
		ClassID:   classID,
		ID:        nftID,
	}

	// mint NFT
	err = assetNFTKeeper.Mint(ctx, settings)
	requireT.NoError(err)

	// try burn the nft with the disabled feature from the issuer account
	err = assetNFTKeeper.Burn(ctx, issuer, classID, nftID)
	requireT.NoError(err)

	// mint new nft
	settings.ID += "-2"
	err = assetNFTKeeper.Mint(ctx, settings)
	requireT.NoError(err)

	err = nftKeeper.Transfer(ctx, settings.ClassID, settings.ID, recipient)
	requireT.NoError(err)

	// try burn the nft with the disabled feature from the recipient account
	err = assetNFTKeeper.Burn(ctx, recipient, classID, settings.ID)
	requireT.ErrorIs(err, types.ErrFeatureDisabled)
}

func TestKeeper_Burn_Frozen(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper
	nftKeeper := testApp.NFTKeeper

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())

	classSettings := types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_burning,
			types.ClassFeature_freezing,
		},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	nftID := "my-id"
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: issuer,
		ClassID:   classID,
		ID:        nftID,
	}

	// mint NFT
	err = assetNFTKeeper.Mint(ctx, settings)
	requireT.NoError(err)

	err = nftKeeper.Transfer(ctx, settings.ClassID, settings.ID, recipient)
	requireT.NoError(err)

	// freeze nft
	err = assetNFTKeeper.Freeze(ctx, issuer, settings.ClassID, settings.ID)
	requireT.NoError(err)

	// try burn the nft with the enabled feature from the recipient account
	err = assetNFTKeeper.Burn(ctx, recipient, classID, settings.ID)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)
}

func TestKeeper_Mint_WithZeroMintFee(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	nftKeeper := testApp.AssetNFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewCoin(constant.DenomDev, sdkmath.ZeroInt()),
	}
	requireT.NoError(nftKeeper.SetParams(ctx, nftParams))

	addr := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer: addr,
		Symbol: "symbol",
	}

	classID, err := nftKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)
	requireT.Equal(classSettings.Symbol+"-"+addr.String(), classID)

	requireT.NoError(err)
	settings := types.MintSettings{
		Sender:    addr,
		Recipient: addr,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	// mint NFT
	err = nftKeeper.Mint(ctx, settings)
	requireT.NoError(err)
}

func TestKeeper_Mint_WithNoFundsCoveringFee(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	nftKeeper := testApp.AssetNFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 10_000_000),
	}
	requireT.NoError(nftKeeper.SetParams(ctx, nftParams))

	addr := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer: addr,
		Symbol: "symbol",
	}

	classID, err := nftKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)
	requireT.Equal(classSettings.Symbol+"-"+addr.String(), classID)

	requireT.NoError(err)
	settings := types.MintSettings{
		Sender:    addr,
		Recipient: addr,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	// mint NFT
	requireT.ErrorIs(nftKeeper.Mint(ctx, settings), cosmoserrors.ErrInsufficientFunds)
}

func TestKeeper_DisableSending(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper
	nftKeeper := testApp.NFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 0),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_disable_sending,
		},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	requireT.NoError(err)
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: issuer,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	// mint NFT
	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))

	// try to send from issuer, it should succeed
	nftID := settings.ID
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	err = nftKeeper.Transfer(ctx, classID, nftID, recipient)
	requireT.NoError(err)

	// try to transfer from non-issuer, it should fail
	recipient2 := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	err = nftKeeper.Transfer(ctx, classID, nftID, recipient2)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)
}

func TestKeeper_Freeze(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper
	nftKeeper := testApp.NFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 0),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_freezing,
		},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	requireT.NoError(err)
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: issuer,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	// mint NFT
	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))

	// freeze NFT
	nftID := settings.ID
	requireT.NoError(assetNFTKeeper.Freeze(ctx, issuer, classID, nftID))
	isFrozen, err := assetNFTKeeper.IsFrozen(ctx, classID, nftID)
	requireT.NoError(err)
	requireT.True(isFrozen)

	// transfer from issuer (although it is frozen, the issuer can send)
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	err = nftKeeper.Transfer(ctx, classID, nftID, recipient)
	requireT.NoError(err)

	// transfer from non-issuer (must fail)
	recipient2 := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	err = nftKeeper.Transfer(ctx, classID, nftID, recipient2)
	requireT.Error(err)
	requireT.True(cosmoserrors.ErrUnauthorized.Is(err))

	// unfreeze
	requireT.NoError(assetNFTKeeper.Unfreeze(ctx, issuer, classID, nftID))
	isFrozen, err = assetNFTKeeper.IsFrozen(ctx, classID, nftID)
	requireT.NoError(err)
	requireT.False(isFrozen)

	// transfer from non-issuer (must succeed)
	err = nftKeeper.Transfer(ctx, classID, nftID, recipient2)
	requireT.NoError(err)
}

func TestKeeper_Freeze_Unfreezable(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 0),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer:   issuer,
		Symbol:   "symbol",
		Features: []types.ClassFeature{},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	requireT.NoError(err)
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: issuer,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	// mint NFT
	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))

	// freeze NFT
	nftID := settings.ID
	err = assetNFTKeeper.Freeze(ctx, issuer, classID, nftID)
	requireT.Error(err)
	requireT.True(types.ErrFeatureDisabled.Is(err))
}

func TestKeeper_Freeze_Nonexistent(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper
	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 0),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	// try to freeze NFT when Class does not exists
	err := assetNFTKeeper.Freeze(ctx, issuer, types.BuildClassID("symbol", issuer), "random-id")
	requireT.Error(err)
	requireT.True(types.ErrClassNotFound.Is(err))

	// issue class
	classSettings := types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_freezing,
		},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	// try to freeze when NFT does not exists
	err = assetNFTKeeper.Freeze(ctx, issuer, classID, "random-id")
	requireT.Error(err)
	requireT.True(types.ErrNFTNotFound.Is(err))
}

func TestKeeper_Whitelist(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper
	nftKeeper := testApp.NFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 1000_000),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	requireT.NoError(testApp.FundAccount(ctx, issuer, sdk.NewCoins(nftParams.MintFee)))
	classSettings := types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_whitelisting,
		},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	requireT.NoError(err)
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: issuer,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	// mint NFT
	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))
	nftID := settings.ID

	// transfer to non whitelisted account, it should fail.
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	err = nftKeeper.Transfer(ctx, classID, nftID, recipient)
	requireT.Error(err)
	requireT.True(cosmoserrors.ErrUnauthorized.Is(err))

	// whitelist the account
	requireT.NoError(assetNFTKeeper.AddToWhitelist(ctx, classID, nftID, issuer, recipient))
	isWhitelisted, err := assetNFTKeeper.IsWhitelisted(ctx, classID, nftID, recipient)
	requireT.NoError(err)
	requireT.True(isWhitelisted)

	// transfer again, it should now succeed.
	err = nftKeeper.Transfer(ctx, classID, nftID, recipient)
	requireT.NoError(err)

	// test query accounts
	recipient2 := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	requireT.NoError(assetNFTKeeper.AddToWhitelist(ctx, classID, nftID, issuer, recipient2))

	whitelistedNftAccounts, _, err := assetNFTKeeper.GetWhitelistedAccountsForNFT(
		ctx, classID, nftID, &query.PageRequest{Limit: query.PaginationMaxLimit},
	)
	requireT.NoError(err)
	requireT.Len(whitelistedNftAccounts, 2)
	requireT.ElementsMatch(whitelistedNftAccounts, []string{
		recipient.String(),
		recipient2.String(),
	})

	incrementallyQueriedAccounts := []string{}
	whitelistedNftAccounts, pageRes, err := assetNFTKeeper.GetWhitelistedAccountsForNFT(
		ctx, classID, nftID, &query.PageRequest{Limit: 1},
	)
	requireT.NoError(err)
	requireT.Len(whitelistedNftAccounts, 1)
	incrementallyQueriedAccounts = append(incrementallyQueriedAccounts, whitelistedNftAccounts...)

	whitelistedNftAccounts, pageRes, err = assetNFTKeeper.GetWhitelistedAccountsForNFT(
		ctx, classID, nftID, &query.PageRequest{Key: pageRes.GetNextKey()},
	)
	requireT.NoError(err)
	requireT.Len(whitelistedNftAccounts, 1)
	incrementallyQueriedAccounts = append(incrementallyQueriedAccounts, whitelistedNftAccounts...)
	requireT.Nil(pageRes.GetNextKey())

	requireT.ElementsMatch([]string{
		recipient.String(),
		recipient2.String(),
	}, incrementallyQueriedAccounts)
}

func TestKeeper_Whitelist_Unwhitelistable(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 0),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer:   issuer,
		Symbol:   "symbol",
		Features: []types.ClassFeature{},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	requireT.NoError(err)
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: issuer,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	// mint NFT
	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))

	// try to whitelist account, it should fail
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	nftID := settings.ID
	err = assetNFTKeeper.AddToWhitelist(ctx, classID, nftID, issuer, recipient)
	requireT.Error(err)
	requireT.True(types.ErrFeatureDisabled.Is(err))
}

func TestKeeper_Whitelist_NonExistent(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 0),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_whitelisting,
		},
	}
	classID := types.BuildClassID(classSettings.Symbol, issuer)
	settings := types.MintSettings{
		Sender:  issuer,
		ClassID: classID,
		ID:      "my-id",
		URI:     "https://my-nft-meta.invalid/1",
		URIHash: "content-hash",
	}

	// try whitelist account, it should fail because class is not present
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	nftID := settings.ID
	err := assetNFTKeeper.AddToWhitelist(ctx, classID, nftID, issuer, recipient)
	requireT.Error(err)
	requireT.True(types.ErrClassNotFound.Is(err))

	// create class
	mintedClassID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)
	requireT.Equal(classID, mintedClassID)

	// try whitelist account, it should fail because nft is not present
	err = assetNFTKeeper.AddToWhitelist(ctx, classID, nftID, issuer, recipient)
	requireT.Error(err)
	requireT.True(types.ErrNFTNotFound.Is(err))
}

func TestKeeper_ClassWhitelist(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper
	nftKeeper := testApp.NFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 1000_000),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	requireT.NoError(testApp.FundAccount(ctx, issuer, sdk.NewCoins(nftParams.MintFee)))
	classSettings := types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_whitelisting,
		},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	requireT.NoError(err)
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: issuer,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	// mint NFT
	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))
	nftID := settings.ID

	// transfer to non whitelisted account, it should fail.
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	err = nftKeeper.Transfer(ctx, classID, nftID, recipient)
	requireT.Error(err)
	requireT.True(cosmoserrors.ErrUnauthorized.Is(err))

	// whitelist the account
	requireT.NoError(assetNFTKeeper.AddToClassWhitelist(ctx, classID, issuer, recipient))

	// check whitelisting
	isWhitelisted, err := assetNFTKeeper.IsWhitelisted(ctx, classID, nftID, recipient)
	requireT.NoError(err)
	requireT.True(isWhitelisted)

	// transfer again, it should now succeed.
	err = nftKeeper.Transfer(ctx, classID, nftID, recipient)
	requireT.NoError(err)

	// test query accounts
	recipient2 := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	requireT.NoError(assetNFTKeeper.AddToClassWhitelist(ctx, classID, issuer, recipient2))

	isWhitelisted, err = assetNFTKeeper.IsWhitelisted(ctx, classID, nftID, recipient2)
	requireT.NoError(err)
	requireT.True(isWhitelisted)

	whitelistedNftAccounts, _, err := assetNFTKeeper.GetClassWhitelistedAccounts(
		ctx, classID, &query.PageRequest{Limit: query.PaginationMaxLimit},
	)
	requireT.NoError(err)
	requireT.Len(whitelistedNftAccounts, 2)
	requireT.ElementsMatch(whitelistedNftAccounts, []string{
		recipient.String(),
		recipient2.String(),
	})

	incrementallyQueriedAccounts := []string{}
	whitelistedNftAccounts, pageRes, err := assetNFTKeeper.GetClassWhitelistedAccounts(
		ctx, classID, &query.PageRequest{Limit: 1},
	)
	requireT.NoError(err)
	requireT.Len(whitelistedNftAccounts, 1)
	incrementallyQueriedAccounts = append(incrementallyQueriedAccounts, whitelistedNftAccounts...)

	whitelistedNftAccounts, pageRes, err = assetNFTKeeper.GetClassWhitelistedAccounts(
		ctx, classID, &query.PageRequest{Key: pageRes.GetNextKey()},
	)
	requireT.NoError(err)
	requireT.Len(whitelistedNftAccounts, 1)
	incrementallyQueriedAccounts = append(incrementallyQueriedAccounts, whitelistedNftAccounts...)
	requireT.Nil(pageRes.GetNextKey())

	requireT.ElementsMatch([]string{
		recipient.String(),
		recipient2.String(),
	}, incrementallyQueriedAccounts)
}

func TestKeeper_ClassWhitelist_And_NFTWhitelist(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_whitelisting,
		},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	// mint 2 NFTs
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: issuer,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}
	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))
	nftID1 := settings.ID

	settings.ID += "2"
	nftID2 := settings.ID
	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))

	// whitelist the recipient
	requireT.NoError(assetNFTKeeper.AddToWhitelist(ctx, classID, nftID1, issuer, recipient))
	assertWhitelisting(t, ctx, assetNFTKeeper, classID, nftID1, recipient, true)
	assertWhitelisting(t, ctx, assetNFTKeeper, classID, nftID2, recipient, false)

	// whitelist the whole class
	requireT.NoError(assetNFTKeeper.AddToClassWhitelist(ctx, classID, issuer, recipient))
	assertWhitelisting(t, ctx, assetNFTKeeper, classID, nftID1, recipient, true)
	assertWhitelisting(t, ctx, assetNFTKeeper, classID, nftID2, recipient, true)

	// remove the class whitelist, nft whitelist should still be effective
	requireT.NoError(assetNFTKeeper.RemoveFromClassWhitelist(ctx, classID, issuer, recipient))
	assertWhitelisting(t, ctx, assetNFTKeeper, classID, nftID1, recipient, true)
	assertWhitelisting(t, ctx, assetNFTKeeper, classID, nftID2, recipient, false)
}

func TestKeeper_ClassWhitelist_Unwhitelistable(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 0),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer:   issuer,
		Symbol:   "symbol",
		Features: []types.ClassFeature{},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	// try to whitelist account, it should fail
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	err = assetNFTKeeper.AddToClassWhitelist(ctx, classID, issuer, recipient)
	requireT.Error(err)
	requireT.ErrorIs(err, types.ErrFeatureDisabled)
}

func TestKeeper_ClassWhitelist_NonExistent(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper

	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classID := types.BuildClassID("symbol", issuer)

	// try whitelist account, it should fail because class is not present
	err := assetNFTKeeper.AddToClassWhitelist(ctx, classID, issuer, recipient)
	requireT.Error(err)
	requireT.ErrorIs(err, types.ErrClassNotFound)
}

func TestKeeper_ClassFreeze(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper
	nftKeeper := testApp.NFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 0),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_freezing,
		},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	// class-freezing issuer is not allowed
	err = assetNFTKeeper.ClassFreeze(ctx, issuer, issuer, classID)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// mint NFT
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: recipient,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))
	nftID := settings.ID

	// class-freeze recipient
	requireT.NoError(assetNFTKeeper.ClassFreeze(ctx, issuer, recipient, classID))

	isClassFrozen, err := assetNFTKeeper.IsClassFrozen(ctx, classID, recipient)
	requireT.NoError(err)
	requireT.True(isClassFrozen)

	frozenAccounts, _, err := assetNFTKeeper.GetClassFrozenAccounts(ctx, classID, &query.PageRequest{})
	requireT.NoError(err)
	requireT.ElementsMatch([]string{recipient.String()}, frozenAccounts)

	// transfer must be rejected
	recipient2 := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	err = nftKeeper.Transfer(ctx, classID, nftID, recipient2)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// class-unfreeze
	requireT.NoError(assetNFTKeeper.ClassUnfreeze(ctx, issuer, recipient, classID))

	// transfer again, must succeed
	err = nftKeeper.Transfer(ctx, classID, nftID, recipient2)
	requireT.NoError(err)
}

func TestKeeper_ClassFreeze_And_NFTFreeze(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_freezing,
		},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	// mint 2 NFTs
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: recipient,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}
	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))
	nftID1 := settings.ID

	settings.ID += "2"
	nftID2 := settings.ID
	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))

	// freeze the recipient for nft1
	requireT.NoError(assetNFTKeeper.Freeze(ctx, issuer, classID, nftID1))
	assertFrozen(t, ctx, assetNFTKeeper, classID, nftID1, true)
	assertFrozen(t, ctx, assetNFTKeeper, classID, nftID2, false)

	// freeze the whole class
	requireT.NoError(assetNFTKeeper.ClassFreeze(ctx, issuer, recipient, classID))
	assertFrozen(t, ctx, assetNFTKeeper, classID, nftID1, true)
	assertFrozen(t, ctx, assetNFTKeeper, classID, nftID2, true)

	// unfreeze the whole class
	requireT.NoError(assetNFTKeeper.ClassUnfreeze(ctx, issuer, recipient, classID))
	assertFrozen(t, ctx, assetNFTKeeper, classID, nftID1, true)
	assertFrozen(t, ctx, assetNFTKeeper, classID, nftID2, false)
}

func TestKeeper_ClassFreeze_Unfreezable(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 0),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	account := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer:   issuer,
		Symbol:   "symbol",
		Features: []types.ClassFeature{},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	requireT.NoError(err)
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: issuer,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	// mint NFT
	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))

	// class freeze NFT
	err = assetNFTKeeper.ClassFreeze(ctx, issuer, account, classID)
	requireT.Error(err)
	requireT.True(types.ErrFeatureDisabled.Is(err))
}

func TestKeeper_ClassFreeze_Nonexistent(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper
	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	account := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 0),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	// try to class freeze when NFT does not exists
	err := assetNFTKeeper.ClassFreeze(ctx, issuer, account, types.BuildClassID("symbol", issuer))
	requireT.Error(err)
	requireT.ErrorIs(err, types.ErrClassNotFound)
}

func TestKeeper_Soulbound(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper
	nftKeeper := testApp.NFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 0),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_soulbound,
		},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	// mint NFT
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: recipient,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))
	nftID := settings.ID

	// transfer must be rejected
	recipient2 := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	err = nftKeeper.Transfer(ctx, classID, nftID, recipient2)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// transfer to issuer must also be rejected
	err = nftKeeper.Transfer(ctx, classID, nftID, issuer)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)
}

func TestKeeper_Soulbound_Burning(t *testing.T) {
	requireT := require.New(t)
	testApp := simapp.New()
	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	assetNFTKeeper := testApp.AssetNFTKeeper
	nftKeeper := testApp.NFTKeeper

	nftParams := types.Params{
		MintFee: sdk.NewInt64Coin(constant.DenomDev, 0),
	}
	requireT.NoError(assetNFTKeeper.SetParams(ctx, nftParams))

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	classSettings := types.IssueClassSettings{
		Issuer: issuer,
		Symbol: "symbol",
		Features: []types.ClassFeature{
			types.ClassFeature_soulbound,
			types.ClassFeature_burning,
		},
	}

	classID, err := assetNFTKeeper.IssueClass(ctx, classSettings)
	requireT.NoError(err)

	// mint NFT
	recipient := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	settings := types.MintSettings{
		Sender:    issuer,
		Recipient: recipient,
		ClassID:   classID,
		ID:        "my-id",
		URI:       "https://my-nft-meta.invalid/1",
		URIHash:   "content-hash",
	}

	requireT.NoError(assetNFTKeeper.Mint(ctx, settings))
	nftID := settings.ID

	// transfer must be rejected
	recipient2 := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	err = nftKeeper.Transfer(ctx, classID, nftID, recipient2)
	requireT.Error(err)
	requireT.ErrorIs(err, cosmoserrors.ErrUnauthorized)

	// burning is allowed
	err = assetNFTKeeper.Burn(ctx, recipient, classID, nftID)
	requireT.NoError(err)
	requireT.False(nftKeeper.HasNFT(ctx, classID, nftID))
}

func genNFTData(requireT *require.Assertions) *codectypes.Any {
	dataString := "metadata"
	dataValue, err := codectypes.NewAnyWithValue(&types.DataBytes{Data: []byte(dataString)})
	requireT.NoError(err)
	return dataValue
}

func unmarshalDataDynamic(requireT *require.Assertions, data *codectypes.Any) types.DataDynamic {
	var dataDynamic types.DataDynamic
	requireT.NoError(dataDynamic.Unmarshal(data.Value))
	return dataDynamic
}

func marshalDataToAny(requireT *require.Assertions, data proto.Message) *codectypes.Any {
	dataValue, err := codectypes.NewAnyWithValue(data)
	requireT.NoError(err)
	return dataValue
}

func cloneDataDynamic(requireT *require.Assertions, data types.DataDynamic) types.DataDynamic {
	dataValue, err := codectypes.NewAnyWithValue(&data)
	requireT.NoError(err)
	var dataDynamic types.DataDynamic
	requireT.NoError(dataDynamic.Unmarshal(dataValue.Value))

	return dataDynamic
}

func requireClassSettingsEqualClass(
	requireT *require.Assertions, settings types.IssueClassSettings, class types.Class,
) {
	requireT.Equal(settings.Name, class.Name)
	requireT.Equal(settings.Symbol, class.Symbol)
	requireT.Equal(settings.Description, class.Description)
	requireT.Equal(settings.URI, class.URI)
	requireT.Equal(settings.URIHash, class.URIHash)
	requireT.Equal(string(settings.Data.Value), string(class.Data.Value))
	requireT.Equal(settings.Features, class.Features)
}

func assertWhitelisting(
	t *testing.T,
	ctx sdk.Context,
	k keeper.Keeper,
	classID, nftID string,
	account sdk.AccAddress,
	expectedWhitelisting bool,
) {
	isWhitelisted, err := k.IsWhitelisted(ctx, classID, nftID, account)
	require.NoError(t, err)
	require.Equal(t, expectedWhitelisting, isWhitelisted)
}

func assertFrozen(t *testing.T, ctx sdk.Context, k keeper.Keeper, classID, nftID string, expected bool) {
	frozen, err := k.IsFrozen(ctx, classID, nftID)
	require.NoError(t, err)
	require.Equal(t, expected, frozen)
}
