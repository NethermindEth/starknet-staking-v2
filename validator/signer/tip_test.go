package signer_test

import (
	"errors"
	"math"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func newInternalSigner(
	t *testing.T, provider rpc.RPCProvider, logger log.Logger,
) signer.Signer {
	t.Helper()

	internalSigner, err := signer.NewInternalSigner(
		t.Context(),
		provider,
		logger,
		&config.Signer{PrivKey: "0x123", OperationalAddress: "0x456"},
		new(config.ContractAddresses).SetDefaults("SN_SEPOLIA"),
	)
	require.NoError(t, err)

	return &internalSigner
}

func newExternalSigner(
	t *testing.T, provider rpc.RPCProvider, logger log.Logger,
) signer.Signer {
	t.Helper()

	externalSigner, err := signer.NewExternalSigner(
		t.Context(),
		provider,
		logger,
		// No external URL: building the transaction doesn't sign it
		&config.Signer{OperationalAddress: "0x456"},
		new(config.ContractAddresses).SetDefaults("SN_SEPOLIA"),
	)
	require.NoError(t, err)

	return &externalSigner
}

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

// Returns a mock provider that expects the calls made when creating a signer and
// building an attestation transaction. The latest block is only expected when
// latestBlock or blockErr is set, so a test fails if the tip estimation fetches
// a block it shouldn't
func newTipProvider(
	t *testing.T, latestBlock *rpc.Block, blockErr error,
) *mocks.MockRPCProvider {
	t.Helper()

	provider := mocks.NewMockRPCProvider(gomock.NewController(t))
	// The internal signer queries the chain ID twice on creation, the external once
	provider.EXPECT().ChainID(gomock.Any()).Return("SN_SEPOLIA", nil).MinTimes(1)
	provider.EXPECT().
		Nonce(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(felt.NewFromUint64[felt.Felt](1), nil)
	if latestBlock != nil || blockErr != nil {
		provider.EXPECT().
			BlockWithTxs(gomock.Any(), rpc.WithBlockTag(rpc.BlockTagLatest), []rpc.TxnResponseFlag{}).
			Return(latestBlock, blockErr)
	}

	return provider
}

func TestBuildAttestTransactionTip(t *testing.T) {
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

	// Both signers estimate the attestation tip the same way
	signers := []struct {
		name      string
		newSigner func(*testing.T, rpc.RPCProvider, log.Logger) signer.Signer
	}{
		{name: "Internal signer", newSigner: newInternalSigner},
		{name: "External signer", newSigner: newExternalSigner},
	}

	for _, signerType := range signers {
		t.Run(signerType.name, func(t *testing.T) {
			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					provider := newTipProvider(t, latestBlockWithTip(tc.blockTip), nil)
					s := signerType.newSigner(t, provider, log.NewNopZapLogger())

					txn, err := s.BuildAttestTransaction(
						t.Context(), new(types.BlockHash), tc.maxTip,
					)
					require.NoError(t, err)
					require.Equal(t, tc.expectedTip, txn.Tip)
				})
			}

			t.Run("Capped tip logs a warning", func(t *testing.T) {
				provider := newTipProvider(t, latestBlockWithTip("0x7d0"), nil) // 2000
				core, logs := observer.New(zapcore.DebugLevel)
				s := signerType.newSigner(t, provider, log.NewZapLoggerWithCore(core))

				txn, err := s.BuildAttestTransaction(t.Context(), new(types.BlockHash), 1000)
				require.NoError(t, err)
				require.Equal(t, rpc.U64("0x3e8"), txn.Tip) // 1000
				require.Equal(t, 1, logs.FilterLevelExact(zapcore.WarnLevel).Len())
			})

			t.Run("Zero max tip skips the estimation and logs no warning", func(t *testing.T) {
				// No latest block expectation: fetching it fails the test
				provider := newTipProvider(t, nil, nil)
				core, logs := observer.New(zapcore.DebugLevel)
				s := signerType.newSigner(t, provider, log.NewZapLoggerWithCore(core))

				txn, err := s.BuildAttestTransaction(t.Context(), new(types.BlockHash), 0)
				require.NoError(t, err)
				require.Equal(t, rpc.U64("0x0"), txn.Tip)
				require.Zero(t, logs.FilterLevelExact(zapcore.WarnLevel).Len())
			})

			t.Run("Error: estimate tip fails", func(t *testing.T) {
				blockErr := errors.New("block error")
				provider := newTipProvider(t, nil, blockErr)
				s := signerType.newSigner(t, provider, log.NewNopZapLogger())

				_, err := s.BuildAttestTransaction(
					t.Context(), new(types.BlockHash), math.MaxUint64,
				)
				require.ErrorIs(t, err, blockErr)
				require.ErrorContains(t, err, "estimate tip")
			})
		})
	}
}
