package signer_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet.go/rpc"
	snUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type callContextKey struct{}

func TestSignersUseTheCallContext(t *testing.T) {
	newProvider := func(t *testing.T) *mocks.MockRPCProvider {
		t.Helper()

		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		provider.EXPECT().ChainID(gomock.Any()).Return("SN_SEPOLIA", nil).AnyTimes()

		return provider
	}
	signers := map[string]func(*testing.T, rpc.RPCProvider) signer.Signer{
		"internal": func(t *testing.T, provider rpc.RPCProvider) signer.Signer {
			t.Helper()

			internalSigner, err := signer.NewInternalSigner(
				t.Context(),
				provider,
				log.NewNopZapLogger(),
				&config.Signer{PrivKey: "0x123", OperationalAddress: "0x456"},
				new(config.ContractAddresses),
			)
			require.NoError(t, err)

			return &internalSigner
		},
		"external": func(t *testing.T, provider rpc.RPCProvider) signer.Signer {
			t.Helper()

			externalSigner, err := signer.NewExternalSigner(
				t.Context(),
				provider,
				log.NewNopZapLogger(),
				&config.Signer{
					ExternalURL:        "http://127.0.0.1:1234",
					OperationalAddress: "0x456",
				},
				new(config.ContractAddresses),
			)
			require.NoError(t, err)

			return &externalSigner
		},
	}

	hash := felt.NewUnsafeFromString[felt.Felt]("0xabc")
	blockID := rpc.WithBlockTag(rpc.BlockTagLatest)
	txn := new(rpc.BroadcastInvokeTxnV3)

	for name, newSigner := range signers {
		t.Run(name, func(t *testing.T) {
			provider := newProvider(t)
			testSigner := newSigner(t, provider)
			ctx := context.WithValue(t.Context(), callContextKey{}, name)

			provider.EXPECT().Call(ctx, gomock.Any(), blockID).Return(nil, nil)
			_, err := testSigner.Call(ctx, rpc.FunctionCall{}, blockID)
			require.NoError(t, err)

			provider.EXPECT().Nonce(ctx, gomock.Any(), gomock.Any()).Return(hash, nil)
			_, err = testSigner.Nonce(ctx)
			require.NoError(t, err)

			provider.EXPECT().TransactionStatus(ctx, hash).Return(nil, nil)
			_, err = testSigner.TransactionStatus(ctx, hash)
			require.NoError(t, err)

			provider.EXPECT().BlockWithTxHashes(ctx, blockID).Return(nil, nil)
			_, err = testSigner.BlockWithTxHashes(ctx, blockID)
			require.NoError(t, err)

			provider.EXPECT().
				AddInvokeTransaction(ctx, txn).
				Return(rpc.AddInvokeTransactionResponse{}, nil)
			_, err = testSigner.InvokeTransaction(ctx, txn)
			require.NoError(t, err)

			fee := new(felt.Felt).SetUint64(1)
			estimate := rpc.FeeEstimation{
				FeeEstimationCommon: rpc.FeeEstimationCommon{
					L1GasConsumed:     fee,
					L1GasPrice:        fee,
					L2GasConsumed:     fee,
					L2GasPrice:        fee,
					L1DataGasConsumed: fee,
					L1DataGasPrice:    fee,
					OverallFee:        fee,
				},
				Unit: rpc.FriUnit,
			}
			provider.EXPECT().
				EstimateFee(ctx, gomock.Any(), gomock.Any(), gomock.Any()).
				Return([]rpc.FeeEstimation{estimate}, nil)
			_, err = testSigner.EstimateFee(
				ctx, &rpc.BroadcastInvokeTxnV3{Version: rpc.TransactionV3WithQueryBit},
			)
			require.NoError(t, err)
		})
	}
}

func TestHashAndSignTxCancelledContext(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	invokeTxnV3 := snUtils.BuildInvokeTxn(
		felt.NewUnsafeFromString[felt.Felt]("0x123"),
		new(felt.Felt).SetUint64(1),
		[]*felt.Felt{},
		new(rpc.ResourceBoundsMapping),
		nil,
	)
	res, err := signer.HashAndSignTx(ctx, invokeTxnV3, new(felt.Felt).SetUint64(1), server.URL)

	require.Zero(t, res)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, requests.Load())
}
