package validator

import (
	"testing"

	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/stretchr/testify/assert"
	"lukechampine.com/uint128"
)

func TestBackupAttestTracker_Sync(t *testing.T) {
	// backupAttest := NewBackupAttestTracker(context.Background(), nil, nil, nil)
	// backupAttest.Refresh(4112044)
}

func TestCalculateCurrentAttestInfo(t *testing.T) {
	type testCase struct {
		description                 string
		epochInfo                   types.EpochInfo
		attestationWindow           uint64
		currentBlockNumber          uint64
		expectedAttestInfoWithEpoch AttestAndEpochInfo
	}

	epochInfo := types.EpochInfo{
		StakerAddress: types.AddressFromString(
			"0x11efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e",
		),
		Stake:         uint128.New(659767778871346152, 3),
		EpochLen:      231,
		EpochID:       20318,
		StartingBlock: types.BlockNumber(4118792),
	}

	testCases := []testCase{
		{
			description:        "original data from sepolia, block in the middle of the epoch",
			epochInfo:          epochInfo,
			attestationWindow:  40,
			currentBlockNumber: 4118800,
			expectedAttestInfoWithEpoch: AttestAndEpochInfo{
				AttestInfo: types.AttestInfo{
					WindowLength: 40,
					TargetBlock:  types.BlockNumber(4118838),
					WindowStart:  types.BlockNumber(4118849),
					WindowEnd:    types.BlockNumber(4118878),
				},
				EpochInfo: types.EpochInfo{
					StakerAddress: epochInfo.StakerAddress,
					Stake:         epochInfo.Stake,
					EpochLen:      epochInfo.EpochLen,
					EpochID:       20318,
					StartingBlock: types.BlockNumber(4118792),
				},
				CurrentEndingBlock: 4119023,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			attestInfoWithEpoch := calculateCurrentAttestInfo(
				testCase.epochInfo,
				testCase.attestationWindow,
				testCase.currentBlockNumber,
			)
			assert.Equal(t, testCase.expectedAttestInfoWithEpoch, attestInfoWithEpoch)
		})
	}
}
