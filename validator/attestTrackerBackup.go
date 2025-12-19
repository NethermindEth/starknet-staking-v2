package validator

import (
	"context"

	junoUtils "github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
)

type BackupAttestTracker struct {
	ctx               context.Context
	logger            *junoUtils.ZapLogger
	provider          *rpc.Provider
	currentAttestInfo AttestAndEpochInfo
	contracts         *types.ValidationContracts

	nodeBlockNumber   uint64
	feederBlockNumber uint64
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
	//nolint:exhaustruct // Using default values for `currentAttestInfo`
	return &BackupAttestTracker{
		ctx:       ctx,
		logger:    logger,
		provider:  provider,
		contracts: contracts,
	}
}

// @todo : both methods are placeholders for now. They need to be implemented.
// I'm thinkin about, in the ProcessBlockHeaders, use these methods like:
//
//	if NodeIsTooFarBehind() {
//		Attest()
//	}
func (a *BackupAttestTracker) NodeIsTooFarBehind() bool {
	return a.nodeBlockNumber-10 < uint64(a.currentAttestInfo.WindowEnd)
}

func (a *BackupAttestTracker) Attest() {
	// let's try to use starkent.go acc.BuildAndInvokeTransaction method
	// with huge multiplier for the fee and tip estimation (like 3x) since the
	// estimation will be based on outdated blocks as the node is behind.
}

// RefreshData refreshes the data (epoch and attest info, block numbers...) for
// the backup attest tracker.
func (a *BackupAttestTracker) RefreshData(
	nodeBlockNumber uint64,
	feederBlockNumber uint64,
	epochInfo types.EpochInfo,
	attestInfo types.AttestInfo,
) error {
	epochEndingBlock := epochInfo.StartingBlock.Uint64() + epochInfo.EpochLen
	if feederBlockNumber < epochEndingBlock {
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
		feederBlockNumber,
	)
	a.nodeBlockNumber = nodeBlockNumber
	a.feederBlockNumber = feederBlockNumber
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
