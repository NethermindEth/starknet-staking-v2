package validator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/sourcegraph/conc"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type dispatchContextKey struct{}

func TestDispatchUsesItsContext(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	signerAddress, err := types.AddressFromString("0x123")
	require.NoError(t, err)
	rewardAddress, err := types.AddressFromString("0x456")
	require.NoError(t, err)

	const expectedCalls = 4
	seen := make(chan context.Context, expectedCalls)

	mockSigner := mocks.NewMockSigner(mockCtrl)
	mockSigner.EXPECT().Address().Return(&signerAddress).AnyTimes()
	mockSigner.EXPECT().
		ValidationContracts().
		Return(validator.SepoliaValidationContracts(t)).
		AnyTimes()
	mockSigner.EXPECT().BuildAttestTransaction(gomock.Any(), gomock.Any()).Return(attestTxn(), nil)
	mockSigner.EXPECT().SignTransaction(gomock.Any(), gomock.Any()).Return(nil, nil).Times(2)
	mockSigner.EXPECT().EstimateFee(gomock.Any(), gomock.Any()).Return(feeEstimate(), nil)
	mockSigner.EXPECT().
		InvokeTransaction(gomock.Any(), gomock.Any()).
		Return(rpc.AddInvokeTransactionResponse{Hash: new(felt.Felt).SetUint64(1)}, nil)
	mockSigner.EXPECT().
		TransactionStatus(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ *felt.Felt) (*rpc.TxnStatusResult, error) {
			seen <- ctx

			return nil, validator.ErrTxnHashNotFound
		}).
		Times(1)
	mockSigner.EXPECT().
		Call(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			ctx context.Context, _ rpc.FunctionCall, _ rpc.BlockID,
		) ([]*felt.Felt, error) {
			seen <- ctx

			return nil, errors.New("balance unavailable")
		}).
		Times(2)

	mockRewardSigner := mocks.NewMockRewardSigner(mockCtrl)
	mockRewardSigner.EXPECT().Address().Return(&rewardAddress).AnyTimes()
	mockRewardSigner.EXPECT().
		ClaimRewards(gomock.Any(), gomock.Any()).
		Do(func(ctx context.Context, _ metrics.Tracer) { seen <- ctx }).
		Times(1)

	ctx := context.WithValue(t.Context(), dispatchContextKey{}, "dispatch")

	dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
	wg := conc.NewWaitGroup()
	wg.Go(func() {
		dispatcher.Dispatch(
			ctx, mockSigner, mockRewardSigner, 100, log.NewNopZapLogger(), metrics.NewNoOpMetrics(),
		)
	})

	sendEvent(t, dispatcher.DoAttest, types.DoAttest{})
	sendEvent(t, dispatcher.EndOfWindow, struct{}{})
	close(dispatcher.DoAttest)
	require.Nil(t, wg.WaitAndRecover())

	timeout := time.After(5 * time.Second)
	for range expectedCalls {
		select {
		case got := <-seen:
			require.Equal(t, ctx, got)
		case <-timeout:
			require.Fail(t, "not every call was made at the end of the window")
		}
	}
}
