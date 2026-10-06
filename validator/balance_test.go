package validator_test

import (
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Records the balance related calls, ignoring the rest
type balanceTracer struct {
	metrics.NoOpMetrics

	signerBalance *float64
	signerBelow   *bool
	rewardBalance *float64
	rewardBelow   *bool
}

func (b *balanceTracer) UpdateSignerBalance(balance float64) { b.signerBalance = &balance }

func (b *balanceTracer) RecordSignerBalanceAboveThreshold() { b.signerBelow = new(bool) }

func (b *balanceTracer) RecordSignerBalanceBelowThreshold() {
	below := true
	b.signerBelow = &below
}

func (b *balanceTracer) UpdateRewardBalance(balance float64) { b.rewardBalance = &balance }

func (b *balanceTracer) RecordRewardBalanceAboveThreshold() { b.rewardBelow = new(bool) }

func (b *balanceTracer) RecordRewardBalanceBelowThreshold() {
	below := true
	b.rewardBelow = &below
}

// Returns a `balance_of` call for the given address
func balanceOfCall(address *types.Address) (rpc.FunctionCall, rpc.BlockID) {
	return rpc.FunctionCall{
			ContractAddress:    felt.NewUnsafeFromString[felt.Felt](constants.StrkContractAddress),
			EntryPointSelector: utils.GetSelectorFromNameFelt("balance_of"),
			Calldata:           []*felt.Felt{address.Felt()},
		},
		rpc.WithBlockTag(rpc.BlockTagLatest)
}

// Returns a `balance_of` result for an amount of STRK
func strkBalance(strk uint64) []*felt.Felt {
	fri := felt.NewFromUint64[felt.Felt](strk)
	fri.Mul(fri, felt.NewFromUint64[felt.Felt](1e18))

	return []*felt.Felt{fri, new(felt.Felt)}
}

func TestCheckBalance(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	logger := log.NewNopZapLogger()
	signerAddress := types.AddressFromString("0x123")
	rewardAddress := types.AddressFromString("0x456")
	threshold := 100.0

	t.Run("Only signer balance is checked when reward address is not set", func(t *testing.T) {
		mockSigner := mocks.NewMockSigner(mockCtrl)
		mockSigner.EXPECT().Address().Return(&signerAddress).AnyTimes()
		mockSigner.EXPECT().
			Call(balanceOfCall(&signerAddress)).
			Return(strkBalance(150), nil)

		tracer := &balanceTracer{}
		validator.CheckBalance(mockSigner, nil, threshold, logger, tracer)

		require.Equal(t, 150.0, *tracer.signerBalance)
		require.False(t, *tracer.signerBelow)
		require.Nil(t, tracer.rewardBalance)
		require.Nil(t, tracer.rewardBelow)
	})

	testCases := []struct {
		name          string
		signerBalance uint64
		rewardBalance uint64
	}{
		{name: "Signer balance above, reward balance below threshold", signerBalance: 150, rewardBalance: 50},
		{name: "Signer balance below, reward balance above threshold", signerBalance: 50, rewardBalance: 150},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockSigner := mocks.NewMockSigner(mockCtrl)
			mockSigner.EXPECT().Address().Return(&signerAddress).AnyTimes()
			mockSigner.EXPECT().
				Call(balanceOfCall(&signerAddress)).
				Return(strkBalance(tc.signerBalance), nil)
			mockSigner.EXPECT().
				Call(balanceOfCall(&rewardAddress)).
				Return(strkBalance(tc.rewardBalance), nil)

			tracer := &balanceTracer{}
			validator.CheckBalance(mockSigner, &rewardAddress, threshold, logger, tracer)

			require.Equal(t, float64(tc.signerBalance), *tracer.signerBalance)
			require.Equal(t, float64(tc.signerBalance) <= threshold, *tracer.signerBelow)
			require.Equal(t, float64(tc.rewardBalance), *tracer.rewardBalance)
			require.Equal(t, float64(tc.rewardBalance) <= threshold, *tracer.rewardBelow)
		})
	}

	t.Run("Reward balance is checked even if signer balance fetch fails", func(t *testing.T) {
		mockSigner := mocks.NewMockSigner(mockCtrl)
		mockSigner.EXPECT().Address().Return(&signerAddress).AnyTimes()
		mockSigner.EXPECT().
			Call(balanceOfCall(&signerAddress)).
			Return(nil, errors.New("some error"))
		mockSigner.EXPECT().
			Call(balanceOfCall(&rewardAddress)).
			Return(strkBalance(150), nil)

		tracer := &balanceTracer{}
		validator.CheckBalance(mockSigner, &rewardAddress, threshold, logger, tracer)

		require.Nil(t, tracer.signerBalance)
		require.Nil(t, tracer.signerBelow)
		require.Equal(t, 150.0, *tracer.rewardBalance)
		require.False(t, *tracer.rewardBelow)
	})

	t.Run("Signer balance is recorded even if reward balance fetch fails", func(t *testing.T) {
		mockSigner := mocks.NewMockSigner(mockCtrl)
		mockSigner.EXPECT().Address().Return(&signerAddress).AnyTimes()
		mockSigner.EXPECT().
			Call(balanceOfCall(&signerAddress)).
			Return(strkBalance(150), nil)
		mockSigner.EXPECT().
			Call(balanceOfCall(&rewardAddress)).
			Return(nil, errors.New("some error"))

		tracer := &balanceTracer{}
		validator.CheckBalance(mockSigner, &rewardAddress, threshold, logger, tracer)

		require.Equal(t, 150.0, *tracer.signerBalance)
		require.False(t, *tracer.signerBelow)
		require.Nil(t, tracer.rewardBalance)
		require.Nil(t, tracer.rewardBelow)
	})
}
