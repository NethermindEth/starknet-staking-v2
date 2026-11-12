package signer_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const (
	secretPathSegment = "0123456789abcdef0123456789abcdef"
	secretAPIKey      = "topsecretapikey"
	signerUser        = "rewardsigneruser"
	signerPassword    = "rewardsignerpassword"
	signerPathSegment = "sign-path-0123456789abcdef"

	autoClaimPrefix    = "Auto-Claiming Rewards: "
	claimBuildFailed   = autoClaimPrefix + "failed to build claim reward transaction"
	claimBlockFailed   = autoClaimPrefix + "failed to get the latest block number"
	sepoliaChainID     = "SN_SEPOLIA"
	blockNumberMethod  = "starknet_blockNumber"
	claimStartBlockNum = uint64(1000)
)

type claimCounter struct {
	*metrics.NoOpMetrics
	failures  int
	confirmed int
}

func (c *claimCounter) RecordRewardClaimFailure() { c.failures++ }

func (c *claimCounter) RecordRewardClaimConfirmed() { c.confirmed++ }

func newClaimCounter() *claimCounter {
	return &claimCounter{NoOpMetrics: metrics.NewNoOpMetrics(), failures: 0, confirmed: 0}
}

func providerTransportError(t *testing.T) error {
	t.Helper()

	server := validator.MockRPCServer(t, validator.MockRPC{
		OnRequest: func(method string, _ []json.RawMessage) {
			if method == blockNumberMethod {
				panic(http.ErrAbortHandler)
			}
		},
	})
	provider, err := rpc.NewProvider(
		t.Context(), server.URL+"/rpc/v0_10/"+secretPathSegment+"?apikey="+secretAPIKey,
	)
	require.NoError(t, err)

	_, err = provider.BlockNumber(t.Context())
	require.ErrorContains(t, err, secretPathSegment)
	require.ErrorContains(t, err, secretAPIKey)

	return err
}

func claimableStakerInfo(t *testing.T) []*felt.Felt {
	t.Helper()

	values := []string{
		"0x0", rewardAccountAddress, stakerAccountAddress, "0x1", "0x1",
		"0x1000000000000000000", "0x1",
	}
	info := make([]*felt.Felt, len(values))
	for i, value := range values {
		info[i] = felt.NewUnsafeFromString[felt.Felt](value)
	}

	return info
}

func claimFeeEstimate() []rpc.FeeEstimation {
	fee := func() *felt.Felt { return felt.NewFromUint64[felt.Felt](0x10) }

	return []rpc.FeeEstimation{{
		FeeEstimationCommon: rpc.FeeEstimationCommon{
			L1GasConsumed:     fee(),
			L1GasPrice:        fee(),
			L2GasConsumed:     fee(),
			L2GasPrice:        fee(),
			L1DataGasConsumed: fee(),
			L1DataGasPrice:    fee(),
			OverallFee:        fee(),
		},
		Unit: rpc.FriUnit,
	}}
}

func expectClaimSucceeds(t *testing.T, provider *mocks.MockRPCProvider) {
	t.Helper()

	accepted := &rpc.TxnStatusResult{
		FinalityStatus:  rpc.TxnStatusAcceptedOnL2,
		ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
	}
	hash := felt.NewUnsafeFromString[felt.Felt]("0xabc")

	provider.EXPECT().
		Call(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(claimableStakerInfo(t), nil).
		AnyTimes()
	provider.EXPECT().BlockNumber(gomock.Any()).Return(claimStartBlockNum, nil).AnyTimes()
	provider.EXPECT().
		Nonce(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(new(felt.Felt).SetUint64(5), nil).
		AnyTimes()
	provider.EXPECT().
		BlockWithTxs(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(new(rpc.Block), nil).
		AnyTimes()
	provider.EXPECT().
		EstimateFee(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(claimFeeEstimate(), nil).
		AnyTimes()
	provider.EXPECT().
		AddInvokeTransaction(gomock.Any(), gomock.Any()).
		Return(rpc.AddInvokeTransactionResponse{Hash: hash}, nil).
		AnyTimes()
	provider.EXPECT().TransactionStatus(gomock.Any(), gomock.Any()).Return(accepted, nil).AnyTimes()
}

func newClaimRewardSigner(
	t *testing.T, provider rpc.RPCProvider, logger log.Logger, externalURL string,
) signer.RewardSigner {
	t.Helper()

	stakerAddress, err := types.AddressFromString(stakerAccountAddress)
	require.NoError(t, err)
	rewardConfig := config.Reward{
		Address:        rewardAccountAddress,
		ClaimThreshold: 1,
		ClaimMaxFee:    math.MaxUint64,
		PrivKey:        "0x1",
	}
	if externalURL != "" {
		rewardConfig.PrivKey = ""
		rewardConfig.ExternalURL = externalURL
	}

	rewardSigner, err := signer.NewRewardSigner(
		t.Context(),
		provider,
		logger,
		&rewardConfig,
		new(config.ContractAddresses),
		&stakerAddress,
		externalURL != "",
	)
	require.NoError(t, err)

	return rewardSigner
}

func requireRedactedError(t *testing.T, logs *observer.ObservedLogs, message string) {
	t.Helper()

	entries := logs.FilterMessage(message).AllUntimed()
	require.Len(t, entries, 1, message)
	logged := fmt.Sprint(entries[0].ContextMap()["error"])
	require.Contains(t, logged, "REDACTED")
	require.Contains(t, logged, "EOF")
}

func requireNoSecretLogged(t *testing.T, logs *observer.ObservedLogs, secrets ...string) {
	t.Helper()

	entries := logs.AllUntimed()
	for i := range entries {
		logged := entries[i].Message + " " + fmt.Sprint(entries[i].ContextMap())
		for _, secret := range secrets {
			require.NotContains(t, logged, secret)
		}
	}
}

func TestClaimRewardsRedactsProviderURLs(t *testing.T) {
	transportErr := providerTransportError(t)

	testCases := []struct {
		name      string
		fail      func(provider *mocks.MockRPCProvider)
		message   string
		failures  int
		confirmed int
	}{
		{
			name: "Unclaimed rewards amount",
			fail: func(provider *mocks.MockRPCProvider) {
				provider.EXPECT().
					Call(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, transportErr)
			},
			message:  autoClaimPrefix + "failed to get unclaimed rewards amount",
			failures: 1,
		},
		{
			name: "Block number at the claim start",
			fail: func(provider *mocks.MockRPCProvider) {
				provider.EXPECT().BlockNumber(gomock.Any()).Return(uint64(0), transportErr)
			},
			message:  claimBlockFailed,
			failures: 1,
		},
		{
			name: "Nonce",
			fail: func(provider *mocks.MockRPCProvider) {
				provider.EXPECT().
					Nonce(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, transportErr)
			},
			message:  claimBuildFailed,
			failures: 1,
		},
		{
			name: "Tip estimate",
			fail: func(provider *mocks.MockRPCProvider) {
				provider.EXPECT().
					BlockWithTxs(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, transportErr)
			},
			message:  claimBuildFailed,
			failures: 1,
		},
		{
			name: "Fee estimate",
			fail: func(provider *mocks.MockRPCProvider) {
				provider.EXPECT().
					EstimateFee(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, transportErr)
			},
			message:  autoClaimPrefix + "failed to populate fees",
			failures: 1,
		},
		{
			name: "Block number before sending",
			fail: func(provider *mocks.MockRPCProvider) {
				provider.EXPECT().BlockNumber(gomock.Any()).Return(claimStartBlockNum, nil)
				provider.EXPECT().BlockNumber(gomock.Any()).Return(uint64(0), transportErr)
			},
			message:  claimBlockFailed,
			failures: 1,
		},
		{
			name: "Sending the transaction",
			fail: func(provider *mocks.MockRPCProvider) {
				provider.EXPECT().
					AddInvokeTransaction(gomock.Any(), gomock.Any()).
					Return(rpc.AddInvokeTransactionResponse{}, transportErr)
			},
			message:  autoClaimPrefix + "failed to send reward claim transaction",
			failures: 1,
		},
		{
			name: "Transaction status",
			fail: func(provider *mocks.MockRPCProvider) {
				provider.EXPECT().
					TransactionStatus(gomock.Any(), gomock.Any()).
					Return(nil, transportErr)
			},
			message:   autoClaimPrefix + "reward claim transaction status not available yet",
			confirmed: 1,
		},
		{
			name: "Unclaimed rewards refresh after the claim",
			fail: func(provider *mocks.MockRPCProvider) {
				provider.EXPECT().
					Call(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(claimableStakerInfo(t), nil)
				provider.EXPECT().
					Call(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, transportErr)
			},
			message:   autoClaimPrefix + "failed to refresh the unclaimed rewards amount after the claim",
			confirmed: 1,
		},
		{
			name: "Panic carrying the error",
			fail: func(provider *mocks.MockRPCProvider) {
				provider.EXPECT().
					BlockNumber(gomock.Any()).
					DoAndReturn(func(context.Context) (uint64, error) { panic(transportErr) })
			},
			message:  autoClaimPrefix + "claim aborted by an unexpected error",
			failures: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				provider := mocks.NewMockRPCProvider(gomock.NewController(t))
				provider.EXPECT().ChainID(gomock.Any()).Return(sepoliaChainID, nil).AnyTimes()
				provider.EXPECT().
					Call(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(claimableStakerInfo(t), nil)
				core, logs := observer.New(zapcore.DebugLevel)
				rewardSigner := newClaimRewardSigner(
					t, provider, log.NewZapLoggerWithCore(core), "",
				)
				tc.fail(provider)
				expectClaimSucceeds(t, provider)
				tracer := newClaimCounter()

				rewardSigner.ClaimRewards(t.Context(), tracer)

				requireRedactedError(t, logs, tc.message)
				requireNoSecretLogged(t, logs, secretPathSegment, secretAPIKey)
				require.Equal(t, tc.failures, tracer.failures)
				require.Equal(t, tc.confirmed, tracer.confirmed)
			})
		})
	}
}

func TestClaimRewardsRedactsExternalSignerURL(t *testing.T) {
	var signRequests atomic.Int32
	signerServer := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			if signRequests.Add(1) > 1 {
				panic(http.ErrAbortHandler)
			}
			_, _ = w.Write([]byte(`{"signature": ["0x1", "0x2"]}`))
		},
	))
	t.Cleanup(signerServer.Close)
	signerURL := fmt.Sprintf(
		"http://%s:%s@%s/%s",
		signerUser, signerPassword, signerServer.Listener.Addr(), signerPathSegment,
	)

	provider := mocks.NewMockRPCProvider(gomock.NewController(t))
	provider.EXPECT().ChainID(gomock.Any()).Return(sepoliaChainID, nil).AnyTimes()
	expectClaimSucceeds(t, provider)
	core, logs := observer.New(zapcore.DebugLevel)
	rewardSigner := newClaimRewardSigner(t, provider, log.NewZapLoggerWithCore(core), signerURL)
	tracer := newClaimCounter()

	rewardSigner.ClaimRewards(t.Context(), tracer)

	require.EqualValues(t, 2, signRequests.Load())
	requireRedactedError(t, logs, claimBuildFailed)
	requireNoSecretLogged(t, logs, signerUser, signerPassword, signerPathSegment)
	require.Equal(t, 1, tracer.failures)
}
