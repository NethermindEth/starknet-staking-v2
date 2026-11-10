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
)

func TestCapTip(t *testing.T) {
	testCases := []struct {
		name           string
		tip            rpc.U64
		maxTip         uint64
		expectedTip    rpc.U64
		expectedCapped bool
	}{
		{name: "Tip below max tip", tip: "0x64", maxTip: 1000, expectedTip: "0x64"},
		{name: "Tip equal to max tip", tip: "0x3e8", maxTip: 1000, expectedTip: "0x3e8"},
		{
			name:           "Tip above max tip",
			tip:            "0x3e9",
			maxTip:         1000,
			expectedTip:    "0x3e8",
			expectedCapped: true,
		},
		{name: "Zero tip", tip: "0x0", maxTip: 1000, expectedTip: "0x0"},
		{
			name:           "Zero max tip removes the tip",
			tip:            "0x3e8",
			maxTip:         0,
			expectedTip:    "0x0",
			expectedCapped: true,
		},
		{name: "Zero tip and zero max tip", tip: "0x0", maxTip: 0, expectedTip: "0x0"},
		{
			name:        "Unlimited max tip",
			tip:         "0x6579c17031f",
			maxTip:      math.MaxUint64,
			expectedTip: "0x6579c17031f",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tip, capped, err := capTip(tc.tip, tc.maxTip)
			require.NoError(t, err)
			require.Equal(t, tc.expectedTip, tip)
			require.Equal(t, tc.expectedCapped, capped)
		})
	}

	t.Run("Error: invalid tip", func(t *testing.T) {
		_, _, err := capTip("not a number", 1000)
		require.ErrorContains(t, err, "invalid tip")
	})
}

func TestBuildAttestTransactionTip(t *testing.T) {
	// A latest block with a single transaction tipping 1000, so the estimated tip
	// is 1500 (1000 * TipMultiplier)
	latestBlock := &rpc.Block{
		Transactions: []rpc.BlockTransaction{{
			Hash: felt.NewFromUint64[felt.Felt](1),
			Transaction: rpc.InvokeTxnV3{
				Type:          rpc.TransactionTypeInvoke,
				Tip:           "0x3e8",
				NonceDataMode: rpc.DAModeL1,
				FeeMode:       rpc.DAModeL1,
			},
		}},
	}

	newExternalSigner := func(
		t *testing.T, provider *mocks.MockRPCProvider, maxTip uint64,
	) *ExternalSigner {
		t.Helper()

		return &ExternalSigner{
			ctx:                 t.Context(),
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
			expectedTip: "0x5dc",
		},
		{name: "Max tip above the estimated tip", maxTip: 2000, expectedTip: "0x5dc"},
		{name: "Max tip below the estimated tip caps it", maxTip: 100, expectedTip: "0x64"},
		{name: "Zero max tip never pays a tip", maxTip: 0, expectedTip: "0x0"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			provider := mocks.NewMockRPCProvider(gomock.NewController(t))
			provider.EXPECT().
				Nonce(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(felt.NewFromUint64[felt.Felt](1), nil)
			provider.EXPECT().
				BlockWithTxs(gomock.Any(), rpc.WithBlockTag(rpc.BlockTagLatest), gomock.Nil()).
				Return(latestBlock, nil)

			s := newExternalSigner(t, provider, tc.maxTip)
			txn, err := s.BuildAttestTransaction(new(types.BlockHash))
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
		_, err := s.BuildAttestTransaction(new(types.BlockHash))
		require.ErrorIs(t, err, blockErr)
	})
}
