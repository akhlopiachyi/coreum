//go:build integrationtests

package modules

import (
	"fmt"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmoserrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integrationtests "github.com/CoreumFoundation/coreum/v6/integration-tests"
	"github.com/CoreumFoundation/coreum/v6/pkg/client"
	"github.com/CoreumFoundation/coreum/v6/testutil/event"
	"github.com/CoreumFoundation/coreum/v6/testutil/integration"
	assetfttypes "github.com/CoreumFoundation/coreum/v6/x/asset/ft/types"
	deterministicgastypes "github.com/CoreumFoundation/coreum/v6/x/deterministicgas/types"
)

var maxMemo = strings.Repeat("-", 256) // cosmos sdk is configured to accept maximum memo of 256 characters by default

// TestBankSendDeterministicGas checks that transfer takes the deterministic amount of gas.
func TestBankSendDeterministicGas(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	sender := chain.GenAccount()
	recipient := chain.GenAccount()

	amountToSend := sdkmath.NewInt(1000)
	chain.FundAccountWithOptions(ctx, t, sender, integration.BalancesOptions{
		Messages: []sdk.Msg{&banktypes.MsgSend{}},
		Amount:   amountToSend,
	})

	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(chain.NewCoin(amountToSend)),
	}

	clientCtx := chain.ClientContext.WithFromAddress(sender)
	bankSendGas := chain.GasLimitByMsgs(&banktypes.MsgSend{})
	res, err := client.BroadcastTx(
		ctx,
		clientCtx,
		chain.TxFactory().
			WithMemo(maxMemo). // memo is set to max length here to charge as much gas as possible
			WithGas(bankSendGas),
		msg)
	require.NoError(t, err)
	require.Equal(t, bankSendGas, uint64(res.GasUsed))

	gasEvents, err := event.FindTypedEvents[*deterministicgastypes.EventGas](res.Events)
	require.NoError(t, err)
	require.Len(t, gasEvents, 1)

	msgGas, ok := chain.DeterministicGasConfig.GasRequiredByMessage(&banktypes.MsgSend{})
	require.True(t, ok)

	require.Equal(t, "cosmos.bank.v1beta1.MsgSend", gasEvents[0].MsgURL)
	require.Equal(t, msgGas, gasEvents[0].DeterministicGas)
	require.Positive(t, gasEvents[0].RealGas)
}

// TestBankSendDeterministicGasTwoBankSends checks that transfer takes the deterministic amount of gas.
func TestBankSendDeterministicGasTwoBankSends(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	sender := chain.GenAccount()
	recipient1 := chain.GenAccount()
	recipient2 := chain.GenAccount()

	bankSend1 := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient1.String(),
		Amount:      sdk.NewCoins(chain.NewCoin(sdkmath.NewInt(1000))),
	}
	bankSend2 := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient2.String(),
		Amount:      sdk.NewCoins(chain.NewCoin(sdkmath.NewInt(1000))),
	}

	chain.FundAccountWithOptions(ctx, t, sender, integration.BalancesOptions{
		Messages: []sdk.Msg{bankSend1, bankSend2},
		Amount:   sdkmath.NewInt(2000),
	})

	gasExpected := chain.GasLimitForMultiMsgTx(&banktypes.MsgSend{}, &banktypes.MsgSend{})
	clientCtx := chain.ClientContext.WithFromAddress(sender)
	txf := chain.ChainContext.TxFactory().WithGas(gasExpected)
	result, err := client.BroadcastTx(ctx, clientCtx, txf, bankSend1, bankSend2)
	require.NoError(t, err)
	require.Equal(t, gasExpected, uint64(result.GasUsed))
}

// TestBankSendDeterministicGasManyCoins checks that transfer takes the higher deterministic amount of gas when more
// coins are transferred.
func TestBankSendDeterministicGasManyCoins(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	const numOfTokens = 3

	sender := chain.GenAccount()
	recipient := chain.GenAccount()

	amountToSend := sdkmath.NewInt(1000)

	issueMsgs := make([]sdk.Msg, 0, numOfTokens)
	for i := range numOfTokens {
		issueMsgs = append(issueMsgs, &assetfttypes.MsgIssue{
			Issuer:        sender.String(),
			Symbol:        fmt.Sprintf("TOK%d", i),
			Subunit:       fmt.Sprintf("tok%d", i),
			Precision:     1,
			Description:   fmt.Sprintf("TOK%d Description", i),
			InitialAmount: amountToSend,
		})
	}

	chain.FundAccountWithOptions(ctx, t, sender, integration.BalancesOptions{
		Messages: append([]sdk.Msg{&banktypes.MsgSend{
			Amount: make(sdk.Coins, numOfTokens),
		}}, issueMsgs...),
		Amount: chain.QueryAssetFTParams(ctx, t).IssueFee.Amount.MulRaw(numOfTokens),
	})

	// Issue fungible tokens
	res, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(sender),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsgs...)),
		issueMsgs...,
	)
	require.NoError(t, err)

	coinsToSend := sdk.NewCoins()

	tokenIssuedEvts, err := event.FindTypedEvents[*assetfttypes.EventIssued](res.Events)
	require.NoError(t, err)
	require.Len(t, tokenIssuedEvts, numOfTokens)

	for _, e := range tokenIssuedEvts {
		coinsToSend = coinsToSend.Add(sdk.NewCoin(e.Denom, amountToSend))
	}

	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		Amount:      coinsToSend,
	}

	clientCtx := chain.ClientContext.WithFromAddress(sender)

	bankSendGas := chain.GasLimitByMsgs(msg)
	msgGas, ok := chain.DeterministicGasConfig.GasRequiredByMessage(msg)
	require.True(t, ok)
	require.Equal(t, chain.DeterministicGasConfig.FixedGas+msgGas, bankSendGas)

	res, err = client.BroadcastTx(
		ctx,
		clientCtx,
		chain.TxFactory().
			WithMemo(maxMemo). // memo is set to max length here to charge as much gas as possible
			WithGas(bankSendGas),
		msg)
	require.NoError(t, err)
	require.Equal(t, bankSendGas, uint64(res.GasUsed))
}

// TestBankSendFailsIfNotEnoughGasIsProvided checks that transfer fails if not enough gas is provided.
func TestBankSendFailsIfNotEnoughGasIsProvided(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	sender := chain.GenAccount()

	amountToSend := sdkmath.NewInt(1000)
	chain.FundAccountWithOptions(ctx, t, sender, integration.BalancesOptions{
		Messages: []sdk.Msg{&banktypes.MsgSend{}},
		Amount:   amountToSend,
	})

	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   sender.String(),
		Amount:      sdk.NewCoins(chain.NewCoin(amountToSend)),
	}

	clientCtx := chain.ClientContext.WithFromAddress(sender)
	bankSendGas := chain.GasLimitByMsgs(&banktypes.MsgSend{})
	_, err := client.BroadcastTx(
		ctx,
		clientCtx,
		chain.TxFactory().WithGas(bankSendGas-1), // gas less than expected
		msg)

	require.True(t, cosmoserrors.ErrOutOfGas.Is(err))
}

// TestBankSendGasEstimation checks that gas is correctly estimated for send message.
func TestBankSendGasEstimation(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	sender := chain.GenAccount()

	amountToSend := sdkmath.NewInt(1000)
	chain.FundAccountWithOptions(ctx, t, sender, integration.BalancesOptions{
		Messages: []sdk.Msg{&banktypes.MsgSend{}},
		Amount:   amountToSend,
	})

	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   sender.String(),
		Amount:      sdk.NewCoins(chain.NewCoin(amountToSend)),
	}

	bankSendGas := chain.GasLimitByMsgs(&banktypes.MsgSend{})
	_, estimatedGas, err := client.CalculateGas(
		ctx,
		chain.ClientContext.WithFromAddress(sender),
		chain.TxFactory().
			WithGas(bankSendGas),
		msg)
	require.NoError(t, err)
	assert.Equal(t, bankSendGas, estimatedGas)
}

// TestBankMultiSendDeterministicGasManyCoins checks that transfer takes the higher deterministic amount of
// gas when more coins are transferred.
func TestBankMultiSendDeterministicGasManyCoins(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	const numOfTokens = 3

	sender := chain.GenAccount()
	recipient := chain.GenAccount()

	amountToSend := sdkmath.NewInt(1000)

	issueMsgs := make([]sdk.Msg, 0, numOfTokens)
	for i := range numOfTokens {
		issueMsgs = append(issueMsgs, &assetfttypes.MsgIssue{
			Issuer:        sender.String(),
			Symbol:        fmt.Sprintf("TOK%d", i),
			Subunit:       fmt.Sprintf("tok%d", i),
			Description:   fmt.Sprintf("TOK%d Description", i),
			Precision:     1,
			InitialAmount: amountToSend,
		})
	}

	chain.FundAccountWithOptions(ctx, t, sender, integration.BalancesOptions{
		Messages: append([]sdk.Msg{&banktypes.MsgMultiSend{
			Inputs: []banktypes.Input{
				{
					Coins: make(sdk.Coins, numOfTokens),
				},
			},
			Outputs: []banktypes.Output{
				{
					Coins: make(sdk.Coins, numOfTokens),
				},
			},
		}}, issueMsgs...),
		Amount: chain.QueryAssetFTParams(ctx, t).IssueFee.Amount.MulRaw(numOfTokens),
	})

	// Issue fungible tokens
	res, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(sender),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsgs...)),
		issueMsgs...,
	)
	require.NoError(t, err)

	coinsToSend := sdk.NewCoins()

	tokenIssuedEvts, err := event.FindTypedEvents[*assetfttypes.EventIssued](res.Events)
	require.NoError(t, err)
	require.Len(t, tokenIssuedEvts, numOfTokens)

	for _, e := range tokenIssuedEvts {
		coinsToSend = coinsToSend.Add(sdk.NewCoin(e.Denom, amountToSend))
	}

	msg := &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{
			{
				Address: sender.String(),
				Coins:   coinsToSend,
			},
		},
		Outputs: []banktypes.Output{
			{
				Address: recipient.String(),
				Coins:   coinsToSend,
			},
		},
	}

	clientCtx := chain.ClientContext.WithFromAddress(sender)
	bankMultiSendGas := chain.GasLimitByMsgs(msg)

	res, err = client.BroadcastTx(
		ctx,
		clientCtx,
		chain.TxFactory().
			WithMemo(maxMemo). // memo is set to max length here to charge as much gas as possible
			WithGas(bankMultiSendGas),
		msg)
	require.NoError(t, err)
	require.Equal(t, bankMultiSendGas, uint64(res.GasUsed))
}

// TestBankMultiSend tests MultiSend message.
func TestBankMultiSend(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	sender := chain.GenAccount()
	recipient1 := chain.GenAccount()
	recipient2 := chain.GenAccount()

	amount := sdkmath.NewInt(1000)

	issueMsg1 := &assetfttypes.MsgIssue{
		Issuer:        sender.String(),
		Symbol:        "TOK1",
		Subunit:       "tok1",
		Precision:     1,
		Description:   "TOK1 Description",
		InitialAmount: amount,
	}
	issueMsg2 := &assetfttypes.MsgIssue{
		Issuer:        sender.String(),
		Symbol:        "TOK2",
		Subunit:       "tok2",
		Precision:     1,
		Description:   "TOK2 Description",
		InitialAmount: amount,
	}

	chain.FundAccountWithOptions(ctx, t, sender, integration.BalancesOptions{
		Messages: append([]sdk.Msg{&banktypes.MsgMultiSend{
			Inputs: []banktypes.Input{
				{Coins: make(sdk.Coins, 2)},
			},
			Outputs: []banktypes.Output{
				{Coins: make(sdk.Coins, 2)},
				{Coins: make(sdk.Coins, 2)},
			},
		}}, issueMsg1, issueMsg2),
		Amount: chain.QueryAssetFTParams(ctx, t).IssueFee.Amount.MulRaw(2),
	})

	// Issue fungible tokens
	res, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(sender),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg1)),
		issueMsg1,
	)
	require.NoError(t, err)

	tokenIssuedEvts, err := event.FindTypedEvents[*assetfttypes.EventIssued](res.Events)
	require.NoError(t, err)

	denom1 := tokenIssuedEvts[0].Denom

	res, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(sender),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issueMsg2)),
		issueMsg2,
	)
	require.NoError(t, err)

	tokenIssuedEvts, err = event.FindTypedEvents[*assetfttypes.EventIssued](res.Events)
	require.NoError(t, err)

	denom2 := tokenIssuedEvts[0].Denom

	msg := &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{
			{
				Address: sender.String(),
				Coins: sdk.NewCoins(
					sdk.NewInt64Coin(denom1, 1000),
					sdk.NewInt64Coin(denom2, 1000),
				),
			},
		},
		Outputs: []banktypes.Output{
			{
				Address: recipient1.String(),
				Coins: sdk.NewCoins(
					sdk.NewInt64Coin(denom1, 600),
					sdk.NewInt64Coin(denom2, 400),
				),
			},
			{
				Address: recipient2.String(),
				Coins: sdk.NewCoins(
					sdk.NewInt64Coin(denom1, 400),
					sdk.NewInt64Coin(denom2, 600),
				),
			},
		},
	}

	clientCtx := chain.ClientContext.WithFromAddress(sender)
	bankMultiSendGas := chain.GasLimitByMsgs(msg)
	res, err = client.BroadcastTx(
		ctx,
		clientCtx,
		chain.TxFactory().
			WithMemo(maxMemo). // memo is set to max length here to charge as much gas as possible
			WithGas(bankMultiSendGas),
		msg)
	require.NoError(t, err)
	require.Equal(t, bankMultiSendGas, uint64(res.GasUsed))

	// =============================

	bankClient := banktypes.NewQueryClient(chain.ClientContext)

	qres, err := bankClient.AllBalances(ctx, &banktypes.QueryAllBalancesRequest{Address: sender.String()})
	require.NoError(t, err)
	require.Empty(t, qres.Balances)

	recipient1AllBalancesRes, err := bankClient.AllBalances(
		ctx, &banktypes.QueryAllBalancesRequest{Address: recipient1.String()},
	)
	require.NoError(t, err)
	require.Equal(t,
		sdk.NewCoins(sdk.NewInt64Coin(denom1, 600), sdk.NewInt64Coin(denom2, 400)),
		recipient1AllBalancesRes.Balances,
	)

	recipient2AllBalancesRes, err := bankClient.AllBalances(
		ctx, &banktypes.QueryAllBalancesRequest{Address: recipient2.String()},
	)
	require.NoError(t, err)
	require.Equal(
		t,
		sdk.NewCoins(sdk.NewInt64Coin(denom1, 400), sdk.NewInt64Coin(denom2, 600)),
		recipient2AllBalancesRes.Balances,
	)
}

// TestTryBankMultiSendFromMultipleAccounts tests MultiSend message is prohibited form multiple accounts.
func TestTryBankMultiSendFromMultipleAccounts(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	requireT := require.New(t)

	sender1 := chain.GenAccount()
	sender2 := chain.GenAccount()

	recipient1 := chain.GenAccount()
	recipient2 := chain.GenAccount()
	recipient3 := chain.GenAccount()

	assetAmount := sdkmath.NewInt(1000)
	issue1Msg := &assetfttypes.MsgIssue{
		Issuer:        sender1.String(),
		Symbol:        "TOK1",
		Subunit:       "tok1",
		Precision:     1,
		Description:   "TOK1 Description",
		InitialAmount: assetAmount,
	}
	issue2Msg := &assetfttypes.MsgIssue{
		Issuer:        sender2.String(),
		Symbol:        "TOK2",
		Subunit:       "tok2",
		Precision:     1,
		Description:   "TOK2 Description",
		InitialAmount: assetAmount,
	}

	denom1 := assetfttypes.BuildDenom(issue1Msg.Subunit, sender1)
	denom2 := assetfttypes.BuildDenom(issue2Msg.Subunit, sender2)

	nativeAmountToSend := chain.NewCoin(sdkmath.NewInt(100))

	// define the message to send from multiple accounts to multiple
	multiSendMsg := &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{
			{
				Address: sender1.String(),
				Coins: sdk.NewCoins(
					sdk.NewInt64Coin(denom1, 1000),
					chain.NewCoin(sdkmath.NewInt(100)),
				),
			},
			{
				Address: sender2.String(),
				Coins: sdk.NewCoins(
					sdk.NewInt64Coin(denom2, 1000),
				),
			},
		},
		Outputs: []banktypes.Output{
			{
				Address: recipient1.String(),
				Coins: sdk.NewCoins(
					chain.NewCoin(sdkmath.NewInt(30)),
					sdk.NewInt64Coin(denom1, 600),
					sdk.NewInt64Coin(denom2, 400),
				),
			},
			{
				Address: recipient2.String(),
				Coins: sdk.NewCoins(
					sdk.NewInt64Coin(denom1, 400),
					sdk.NewInt64Coin(denom2, 600),
				),
			},
			{
				Address: recipient3.String(),
				Coins: sdk.NewCoins(
					chain.NewCoin(sdkmath.NewInt(70)),
				),
			},
		},
	}

	issueFee := chain.QueryAssetFTParams(ctx, t).IssueFee.Amount

	// fund accounts
	chain.FundAccountsWithOptions(ctx, t, []integration.AccWithBalancesOptions{
		{
			Acc: sender1,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{
					multiSendMsg,
					issue1Msg,
				},
				Amount: issueFee.Add(nativeAmountToSend.Amount),
			},
		}, {
			Acc: sender2,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{issue2Msg},
				Amount:   issueFee,
			},
		},
	})

	// issue first fungible token
	_, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(sender1),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issue1Msg)),
		issue1Msg,
	)
	requireT.NoError(err)
	// issue second fungible token
	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(sender2),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(issue2Msg)),
		issue2Msg,
	)
	requireT.NoError(err)

	tx := signTxWithMultipleSignatures(ctx, t, chain, []sdk.Msg{multiSendMsg}, []sdk.AccAddress{sender1, sender2})

	// encode tx and broadcast
	encodedMultiSendTx, err := chain.ClientContext.TxConfig().TxEncoder()(tx)
	requireT.NoError(err)
	_, err = client.BroadcastRawTx(
		ctx,
		chain.ClientContext.WithFromAddress(sender1),
		encodedMultiSendTx)
	requireT.ErrorIs(err, banktypes.ErrMultipleSenders)
}

// TestBankCoreSend checks that core is transferred correctly between wallets.
func TestBankCoreSend(t *testing.T) {
	t.Parallel()

	ctx, chain := integrationtests.NewCoreumTestingContext(t)

	sender := chain.GenAccount()
	recipient := chain.GenAccount()

	senderInitialAmount := sdkmath.NewInt(100)
	recipientInitialAmount := sdkmath.NewInt(10)

	chain.FundAccountsWithOptions(ctx, t, []integration.AccWithBalancesOptions{
		{
			Acc: sender,
			Options: integration.BalancesOptions{
				Messages: []sdk.Msg{&banktypes.MsgSend{}},
				Amount:   senderInitialAmount,
			},
		}, {
			Acc: recipient,
			Options: integration.BalancesOptions{
				Amount: recipientInitialAmount,
			},
		},
	})

	// transfer tokens from sender to recipient
	amountToSend := sdkmath.NewInt(10)
	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(chain.NewCoin(amountToSend)),
	}

	result, err := client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(sender),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msg)),
		msg,
	)
	require.NoError(t, err)

	t.Logf("Transfer executed, txHash:%s", result.TxHash)

	// Query wallets for current balance
	bankClient := banktypes.NewQueryClient(chain.ClientContext)

	balancesSender, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: sender.String(),
		Denom:   chain.ChainSettings.Denom,
	})
	require.NoError(t, err)

	balancesRecipient, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: recipient.String(),
		Denom:   chain.ChainSettings.Denom,
	})
	require.NoError(t, err)

	assert.Equal(t, senderInitialAmount.Sub(amountToSend).String(), balancesSender.Balance.Amount.String())
	assert.Equal(t, recipientInitialAmount.Add(amountToSend).String(), balancesRecipient.Balance.Amount.String())

	// Try to send more than remaining balance
	msg = &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		// sender can't send whole balance because funds for paying fees are required.
		Amount: sdk.NewCoins(*balancesSender.Balance),
	}

	_, err = client.BroadcastTx(
		ctx,
		chain.ClientContext.WithFromAddress(sender),
		chain.TxFactory().WithGas(chain.GasLimitByMsgs(msg)),
		msg,
	)
	require.Error(t, err)
	require.ErrorContains(t, err, "insufficient funds")
}
