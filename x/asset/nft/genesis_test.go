package nft_test

import (
	"fmt"
	"sort"
	"testing"

	sdkmath "cosmossdk.io/math"
	rawnft "cosmossdk.io/x/nft"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/crypto/secp256k1"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum/v6/testutil/simapp"
	"github.com/CoreumFoundation/coreum/v6/x/asset/nft"
	"github.com/CoreumFoundation/coreum/v6/x/asset/nft/types"
)

func TestInitAndExportGenesis(t *testing.T) {
	assertT := assert.New(t)
	requireT := require.New(t)

	testApp := simapp.New()

	ctx := testApp.NewContextLegacy(false, tmproto.Header{})
	nftKeeper := testApp.AssetNFTKeeper

	// prepare the genesis data

	issuer := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	rawGenState := &rawnft.GenesisState{}

	// class definitions
	var classDefinitions []types.ClassDefinition
	for i := range 5 {
		classDefinition := types.ClassDefinition{
			ID: fmt.Sprintf("classid%d-%s", i, issuer),
			Features: []types.ClassFeature{
				types.ClassFeature_freezing,
				types.ClassFeature_whitelisting,
			},
			RoyaltyRate: sdkmath.LegacyMustNewDecFromStr(fmt.Sprintf("0.%d", (i+1)%10)),
		}

		rawGenState.Classes = append(rawGenState.Classes, &rawnft.Class{
			Id:     classDefinition.ID,
			Name:   fmt.Sprintf("name-%d", i),
			Symbol: fmt.Sprintf("symbol-%d", i),
		})
		classDefinitions = append(classDefinitions, classDefinition)
	}

	for i := range 5 {
		rawGenState.Entries = append(rawGenState.Entries, &rawnft.Entry{
			Owner: sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
			Nfts: []*rawnft.NFT{
				{
					ClassId: fmt.Sprintf("classid%d-%s", i, issuer),
					Id:      fmt.Sprintf("nft-id-1-%d", i),
				},
				{
					ClassId: fmt.Sprintf("classid%d-%s", i, issuer),
					Id:      fmt.Sprintf("nft-id-2-%d", i),
				},
			},
		})
	}

	// Frozen NFTs
	var frozen []types.FrozenNFT
	for i := range 5 {
		frozen = append(frozen, types.FrozenNFT{
			ClassID: fmt.Sprintf("classid%d-%s", i, issuer),
			NftIDs: []string{
				fmt.Sprintf("nft-id-1-%d", i),
				fmt.Sprintf("nft-id-2-%d", i),
			},
		})
	}

	// Whitelisting
	var whitelisted []types.WhitelistedNFTAccounts
	for i := range 5 {
		whitelisted = append(whitelisted,
			types.WhitelistedNFTAccounts{
				ClassID: fmt.Sprintf("classid%d-%s", i, issuer),
				NftID:   fmt.Sprintf("nft-id-1-%d", i),
				Accounts: []string{
					sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
					sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
					sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
				},
			},
			types.WhitelistedNFTAccounts{
				ClassID: fmt.Sprintf("classid%d-%s", i, issuer),
				NftID:   fmt.Sprintf("nft-id-2-%d", i),
				Accounts: []string{
					sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
					sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
					sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
				},
			})
	}

	// ClassWhitelisting
	var classWhitelisted []types.ClassWhitelistedAccounts
	for i := range 5 {
		classWhitelisted = append(classWhitelisted,
			types.ClassWhitelistedAccounts{
				ClassID: fmt.Sprintf("classid%d-%s", i, issuer),
				Accounts: []string{
					sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
					sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
					sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
				},
			},
		)
	}

	// ClassFrozen
	var classFrozen []types.ClassFrozenAccounts
	for i := range 5 {
		classFrozen = append(classFrozen,
			types.ClassFrozenAccounts{
				ClassID: fmt.Sprintf("classid%d-%s", i, issuer),
				Accounts: []string{
					sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
					sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
					sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
				},
			},
		)
	}

	// Burnt NFTs
	var burnt []types.BurntNFT
	for i := range 5 {
		burnt = append(burnt, types.BurntNFT{
			ClassID: fmt.Sprintf("classid%d-%s", i, issuer),
			NftIDs: []string{
				fmt.Sprintf("burnt-nft-id-1-%d", i),
				fmt.Sprintf("burnt-nft-id-2-%d", i),
			},
		})
	}

	genState := types.GenesisState{
		Params:                   types.DefaultParams(),
		ClassDefinitions:         classDefinitions,
		FrozenNFTs:               frozen,
		WhitelistedNFTAccounts:   whitelisted,
		ClassWhitelistedAccounts: classWhitelisted,
		ClassFrozenAccounts:      classFrozen,
		BurntNFTs:                burnt,
	}

	// init the keeper
	testApp.NFTKeeper.InitGenesis(ctx, rawGenState)
	nft.InitGenesis(ctx, nftKeeper, genState)

	// assert the keeper state

	// class definitions
	for _, definition := range classDefinitions {
		storedDefinition, err := nftKeeper.GetClassDefinition(ctx, definition.ID)
		requireT.NoError(err)
		assertT.Equal(definition, storedDefinition)
	}

	// params
	params, err := nftKeeper.GetParams(ctx)
	requireT.NoError(err)
	assertT.Equal(types.DefaultParams(), params)

	// check that export is equal import
	exportedGenState := nft.ExportGenesis(ctx, nftKeeper)
	assertT.ElementsMatch(genState.ClassDefinitions, exportedGenState.ClassDefinitions)
	assertT.ElementsMatch(genState.FrozenNFTs, exportedGenState.FrozenNFTs)

	for _, st := range genState.WhitelistedNFTAccounts {
		sort.Strings(st.Accounts)
	}
	for _, st := range exportedGenState.WhitelistedNFTAccounts {
		sort.Strings(st.Accounts)
	}

	// sort whitelisting accounts
	for _, st := range genState.ClassWhitelistedAccounts {
		sort.Strings(st.Accounts)
	}
	for _, st := range exportedGenState.ClassWhitelistedAccounts {
		sort.Strings(st.Accounts)
	}

	// sort frozen accounts
	for _, st := range genState.ClassFrozenAccounts {
		sort.Strings(st.Accounts)
	}
	for _, st := range exportedGenState.ClassFrozenAccounts {
		sort.Strings(st.Accounts)
	}

	assertT.ElementsMatch(genState.WhitelistedNFTAccounts, exportedGenState.WhitelistedNFTAccounts)
	assertT.ElementsMatch(genState.ClassWhitelistedAccounts, exportedGenState.ClassWhitelistedAccounts)
	assertT.ElementsMatch(genState.ClassFrozenAccounts, exportedGenState.ClassFrozenAccounts)
	assertT.ElementsMatch(genState.BurntNFTs, exportedGenState.BurntNFTs)
}
