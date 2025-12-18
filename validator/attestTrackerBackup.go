package validator

import (
	"context"

	"github.com/NethermindEth/juno/core/felt"
	junoUtils "github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
)

type BackupAttestTracker struct {
	Transaction AttestTransaction
	hash        felt.Felt
	status      AttestStatus

	ctx               context.Context
	logger            *junoUtils.ZapLogger
	provider          *rpc.Provider
	currentAttestInfo AttestAndEpochInfo
	contracts         *types.ValidationContracts
}

type AttestAndEpochInfo struct {
	types.AttestInfo
	types.EpochInfo
	CurrentEndingBlock uint64
}

// NewBackupAttestTracker creates a new BackupAttestTracker.
func NewBackupAttestTracker(
	ctx context.Context,
	provider *rpc.Provider,
	logger *junoUtils.ZapLogger,
	contracts *types.ValidationContracts,
) *BackupAttestTracker {
	return &BackupAttestTracker{
		ctx:       ctx,
		logger:    logger,
		provider:  provider,
		contracts: contracts,
	}
}

func (a *BackupAttestTracker) Refresh(
	currentBlockNumber uint64,
	epochInfo types.EpochInfo,
	attestInfo types.AttestInfo,
) error {
	epochEndingBlock := epochInfo.StartingBlock.Uint64() + epochInfo.EpochLen
	if currentBlockNumber < epochEndingBlock {
		a.logger.Debug(
			"current block number is within the current attest info. Existing attest info will be used.",
		)
		a.currentAttestInfo = AttestAndEpochInfo{
			AttestInfo:         attestInfo,
			EpochInfo:          epochInfo,
			CurrentEndingBlock: epochEndingBlock,
		}

		return nil
	}

	a.currentAttestInfo = calculateCurrentAttestInfo(
		epochInfo,
		attestInfo.WindowLength,
		currentBlockNumber,
	)
	a.logger.Debugw("resfresh done", "current attest info", a.currentAttestInfo)

	return nil
}

// calculateCurrentAttestInfo calculates the current attest and epoch info
// based on the current block number and the last epoch info.
func calculateCurrentAttestInfo(
	epochInfo types.EpochInfo,
	attestationWindow uint64,
	currentBlockNumber uint64,
) AttestAndEpochInfo {
	epochsPassed := (currentBlockNumber - epochInfo.StartingBlock.Uint64()) / epochInfo.EpochLen
	currentEpoch := epochInfo.EpochID + epochsPassed
	currentStartingBlock := epochInfo.StartingBlock.Uint64() + (epochsPassed * epochInfo.EpochLen)
	currentEndingBlock := currentStartingBlock + epochInfo.EpochLen

	newEpochInfo := types.EpochInfo{
		StakerAddress: epochInfo.StakerAddress,
		Stake:         epochInfo.Stake,
		EpochLen:      epochInfo.EpochLen,
		EpochID:       currentEpoch,
		StartingBlock: types.BlockNumber(currentStartingBlock),
	}

	targerBlockNumber := signerP.ComputeBlockNumberToAttestTo(&newEpochInfo, attestationWindow)

	//nolint:exhaustruct // Purposely not using the block hash
	return AttestAndEpochInfo{
		AttestInfo: types.AttestInfo{
			WindowLength: attestationWindow,
			TargetBlock:  targerBlockNumber,
			WindowStart:  targerBlockNumber + types.BlockNumber(constants.MinAttestationWindow),
			WindowEnd:    targerBlockNumber + types.BlockNumber(attestationWindow),
		},
		EpochInfo:          newEpochInfo,
		CurrentEndingBlock: currentEndingBlock,
	}
}
