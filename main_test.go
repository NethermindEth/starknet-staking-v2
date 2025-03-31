package main_test

import (
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	main "github.com/NethermindEth/starknet-staking-v2"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"lukechampine.com/uint128"
)

func TestComputeBlockNumberToAttestTo(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)
	t.Run("Correct target block number computation - example 1", func(t *testing.T) {
		mockAccount.EXPECT().Address().Return(utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e"))
		epochInfo := main.EpochInfo{
			Stake:                     uint128.From64(1000000000000000000),
			EpochLen:                  40,
			EpochId:                   1516,
			CurrentEpochStartingBlock: main.BlockNumber(639270),
		}
		attestationWindow := uint64(16)

		blockNumber := main.ComputeBlockNumberToAttestTo(mockAccount, epochInfo, attestationWindow)

		require.Equal(t, main.BlockNumber(639291), blockNumber)
	})

	t.Run("Correct target block number computation - example 2", func(t *testing.T) {
		test, err := new(felt.Felt).SetString("0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		require.NoError(t, err)
		mockAccount.EXPECT().Address().Return(test)

		epochInfo := main.EpochInfo{
			StakerAddress:             main.Address(*test),
			Stake:                     uint128.From64(1000000000000000000),
			EpochLen:                  40,
			EpochId:                   1517,
			CurrentEpochStartingBlock: main.BlockNumber(639310),
		}
		attestationWindow := uint64(16)

		blockNumber := main.ComputeBlockNumberToAttestTo(mockAccount, epochInfo, attestationWindow)
		require.Equal(t, main.BlockNumber(639316), blockNumber)
	})

	t.Run("Correct target block number computation - example 3", func(t *testing.T) {
		mockAccount.EXPECT().Address().Return(utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e"))
		epochInfo := main.EpochInfo{
			Stake:                     uint128.From64(1000000000000000000),
			EpochLen:                  40,
			EpochId:                   1518,
			CurrentEpochStartingBlock: main.BlockNumber(639350),
		}
		attestationWindow := uint64(16)

		blockNumber := main.ComputeBlockNumberToAttestTo(mockAccount, epochInfo, attestationWindow)

		require.Equal(t, main.BlockNumber(639369), blockNumber)
	})
}
