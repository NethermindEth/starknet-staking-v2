package signer

import (
	"errors"
	"math"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// Returns a latest block with a single transaction paying the given tip
func latestBlockWithTip(tip rpc.U64) *rpc.Block {
	return &rpc.Block{
		Transactions: []rpc.BlockTransaction{{
			Hash: felt.NewFromUint64[felt.Felt](1),
			Transaction: rpc.InvokeTxnV3{
				Type:          rpc.TransactionTypeInvoke,
				Tip:           tip,
				NonceDataMode: rpc.DAModeL1,
				FeeMode:       rpc.DAModeL1,
			},
		}},
	}
}

func TestEstimateAttestTip(t *testing.T) {
	// The estimated tip is the block's tip multiplied by TipMultiplier (1.5)
	testCases := []struct {
		name        string
		blockTip    rpc.U64
		maxTip      uint64
		expectedTip rpc.U64
	}{
		{
			name:        "Estimated tip below max tip",
			blockTip:    "0x3e8", // 1000
			maxTip:      2000,
			expectedTip: "0x5dc", // 1500
		},
		{
			name:        "Estimated tip equal to max tip",
			blockTip:    "0x3e8", // 1000
			maxTip:      1500,
			expectedTip: "0x5dc", // 1500
		},
		{
			name:        "Estimated tip above max tip is capped",
			blockTip:    "0x7d0", // 2000
			maxTip:      1000,
			expectedTip: "0x3e8", // 1000
		},
		{
			name:        "Unlimited max tip keeps an outlier tip",
			blockTip:    "0xe8d4a51000", // 1000000000000
			maxTip:      math.MaxUint64,
			expectedTip: "0x15d3ef79800", // 1500000000000
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			provider := mocks.NewMockRPCProvider(gomock.NewController(t))
			provider.EXPECT().
				BlockWithTxs(gomock.Any(), rpc.WithBlockTag(rpc.BlockTagLatest), []rpc.TxnResponseFlag{}).
				Return(latestBlockWithTip(tc.blockTip), nil)

			tip, err := estimateAttestTip(t.Context(), provider, tc.maxTip, log.NewNopZapLogger())
			require.NoError(t, err)
			require.Equal(t, tc.expectedTip, tip)
		})
	}

	t.Run("Capped tip logs a warning", func(t *testing.T) {
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		provider.EXPECT().
			BlockWithTxs(gomock.Any(), rpc.WithBlockTag(rpc.BlockTagLatest), []rpc.TxnResponseFlag{}).
			Return(latestBlockWithTip("0x7d0"), nil) // 2000
		core, logs := observer.New(zapcore.DebugLevel)

		tip, err := estimateAttestTip(t.Context(), provider, 1000, log.NewZapLoggerWithCore(core))
		require.NoError(t, err)
		require.Equal(t, rpc.U64("0x3e8"), tip) // 1000
		require.Equal(t, 1, logs.FilterLevelExact(zapcore.WarnLevel).Len())
	})

	t.Run("Zero max tip skips the estimation and logs no warning", func(t *testing.T) {
		// No provider expectations: any RPC call fails the test
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		core, logs := observer.New(zapcore.DebugLevel)

		tip, err := estimateAttestTip(t.Context(), provider, 0, log.NewZapLoggerWithCore(core))
		require.NoError(t, err)
		require.Equal(t, rpc.U64("0x0"), tip)
		require.Zero(t, logs.Len())
	})

	t.Run("Error: estimate tip fails", func(t *testing.T) {
		blockErr := errors.New("block error")
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		provider.EXPECT().
			BlockWithTxs(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, blockErr)

		_, err := estimateAttestTip(t.Context(), provider, math.MaxUint64, log.NewNopZapLogger())
		require.ErrorIs(t, err, blockErr)
		require.ErrorContains(t, err, "estimate tip")
	})
}

func TestBuildAttestTransactionTip(t *testing.T) {
	// The estimated tip is 1500 (1000 * TipMultiplier)
	latestBlock := latestBlockWithTip("0x3e8") // 1000

	newExternalSigner := func(
		t *testing.T, provider *mocks.MockRPCProvider, maxTip uint64,
	) *ExternalSigner {
		t.Helper()

		return &ExternalSigner{
			Provider:            provider,
			logger:              log.NewNopZapLogger(),
			validationContracts: *sepoliaContracts(t),
			maxTip:              maxTip,
		}
	}

	testCases := []struct {
		name        string
		maxTip      uint64
		expectedTip rpc.U64
	}{
		{
			name:        "Unlimited max tip keeps the estimated tip",
			maxTip:      math.MaxUint64,
			expectedTip: "0x5dc", // 1500
		},
		{
			name:        "Max tip above the estimated tip",
			maxTip:      2000,
			expectedTip: "0x5dc", // 1500
		},
		{
			name:        "Max tip below the estimated tip caps it",
			maxTip:      100,
			expectedTip: "0x64", // 100
		},
		{
			name:        "Zero max tip never pays a tip",
			maxTip:      0,
			expectedTip: "0x0", // 0
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			provider := mocks.NewMockRPCProvider(gomock.NewController(t))
			provider.EXPECT().
				Nonce(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(felt.NewFromUint64[felt.Felt](1), nil)
			// A zero max tip skips the tip estimation, so the latest block is not fetched
			if tc.maxTip != 0 {
				provider.EXPECT().
					BlockWithTxs(gomock.Any(), rpc.WithBlockTag(rpc.BlockTagLatest), []rpc.TxnResponseFlag{}).
					Return(latestBlock, nil)
			}

			s := newExternalSigner(t, provider, tc.maxTip)
			txn, err := s.BuildAttestTransaction(t.Context(), new(types.BlockHash))
			require.NoError(t, err)
			require.Equal(t, tc.expectedTip, txn.Tip)
		})
	}

	t.Run("Error: estimate tip fails", func(t *testing.T) {
		blockErr := errors.New("block error")
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		provider.EXPECT().
			Nonce(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(felt.NewFromUint64[felt.Felt](1), nil)
		provider.EXPECT().
			BlockWithTxs(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, blockErr)

		s := newExternalSigner(t, provider, math.MaxUint64)
		_, err := s.BuildAttestTransaction(t.Context(), new(types.BlockHash))
		require.ErrorIs(t, err, blockErr)
	})
}
