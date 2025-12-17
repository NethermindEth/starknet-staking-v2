package validator

import (
	"testing"

	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/stretchr/testify/assert"
)

func TestBackupAttestTracker_Sync(t *testing.T) {
	// backupAttest := NewBackupAttestTracker(context.Background(), nil, nil, nil)
	// backupAttest.Refresh(4112044)
}

func TestCalculateCurrentAttestInfo(t *testing.T) {
	type testCase struct {
		description                 string
		epochInfo                   epochInfo
		attestationWindow           uint64
		currentBlockNumber          uint64
		expectedAttestInfoWithEpoch AttestAndEpochInfo
	}

	testCases := []testCase{
		{
			description: "original data from sepolia, block in the middle of the epoch",
			epochInfo: epochInfo{
				Length:        231,
				StartingBlock: 924293,
				StartingEpoch: 6489,
			},
			attestationWindow:  40,
			currentBlockNumber: 4114200,
			expectedAttestInfoWithEpoch: AttestAndEpochInfo{
				AttestInfo: types.AttestInfo{
					TargetBlock: types.BlockNumber(4114196),
					WindowStart: types.BlockNumber(4114207),
					WindowEnd:   types.BlockNumber(4114236),
				},
				CurrentBlockNumber: 4114200,
				EpochID:            20298,
				EpochStartingBlock: 4114172,
				EpochEndingBlock:   4114403,
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
