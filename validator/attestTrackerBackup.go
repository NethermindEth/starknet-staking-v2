package validator

import (
	"context"

	"github.com/NethermindEth/juno/core/felt"
	junoUtils "github.com/NethermindEth/juno/utils"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
)

var _ AttestTracker = (*BackupAttestTracker)(nil)

type BackupAttestTracker struct {
	Transaction AttestTransaction
	hash        felt.Felt
	status      AttestStatus

	ctx               context.Context
	logger            *junoUtils.ZapLogger
	provider          *rpc.Provider
	originalEpochInfo epochInfo
	attestationWindow uint64
	currentAttestInfo AttestAndEpochInfo
	contracts         *types.ValidationContracts
}

// epochInfo is a struct representing the epoch info configured in
// the staking contract, used to calculate the next epochs
// https://github.com/starkware-libs/starknet-staking/blob/bd3cec884b465c2edc8b43012135d01500c27e5b/src/staking/objects.cairo#L153
//
//nolint:lll // The link would be invalid if we break the line
type epochInfo struct {
	Length        uint64
	StartingBlock uint64
	StartingEpoch uint64
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
			CurrentEndingBlock: epochInfo.StartingBlock.Uint64() + epochInfo.EpochLen,
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

func calculateCurrentAttestInfo(
	epochInfo types.EpochInfo,
	attestationWindow uint64,
	currentBlockNumber uint64,
) AttestAndEpochInfo {
	epochsPassed := (currentBlockNumber - epochInfo.StartingBlock.Uint64()) / epochInfo.EpochLen
	currentEpoch := epochInfo.EpochID + epochsPassed
	currentStartingBlock := epochInfo.StartingBlock.Uint64() + (epochsPassed * epochInfo.EpochLen)
	currentEndingBlock := currentStartingBlock + epochInfo.EpochLen

	// @todo finish this
	return AttestAndEpochInfo{
		AttestInfo: types.AttestInfo{
			WindowLength:    attestationWindow,
			TargetBlock:     types.BlockNumber(123),
			TargetBlockHash: types.BlockHash{},
			WindowStart:     types.BlockNumber(123),
			WindowEnd:       types.BlockNumber(123),
		},
		EpochInfo: types.EpochInfo{
			StakerAddress: epochInfo.StakerAddress,
			Stake:         epochInfo.Stake,
			EpochLen:      epochInfo.EpochLen,
			EpochID:       currentEpoch,
			StartingBlock: types.BlockNumber(currentStartingBlock),
		},
		CurrentEndingBlock: currentEndingBlock,
	}
}

// //////////////////////////////////////////////////////////////
// Implement the AttestTracker interface
// //////////////////////////////////////////////////////////////
func (a *BackupAttestTracker) NewAttestTracker() AttestTracker {
	//nolint:exhaustruct // Using default values
	return &BackupAttestTracker{
		Transaction:       AttestTransaction{},
		hash:              felt.Zero,
		status:            Iddle,
		ctx:               a.ctx,
		provider:          a.provider,
		originalEpochInfo: a.originalEpochInfo,
		contracts:         a.contracts,
	}
}

func (a *BackupAttestTracker) Hash() felt.Felt {
	return a.hash
}

func (a *BackupAttestTracker) SetHash(hash felt.Felt) {
	a.hash = hash
}

func (a *BackupAttestTracker) Status() AttestStatus {
	return a.status
}

func (a *BackupAttestTracker) SetStatus(status AttestStatus) {
	a.status = status
}

func (a *BackupAttestTracker) UpdateStatus(signer signerP.Signer, logger *junoUtils.ZapLogger) {
	status := TrackAttest(signer, logger, &a.hash)
	a.SetStatus(status)
}

func (a *BackupAttestTracker) BuildTxn(signer signerP.Signer, blockHash *types.BlockHash) error {
	return a.Transaction.Build(signer, blockHash)
}

func (a *BackupAttestTracker) Attest(
	signer signerP.Signer,
) (rpc.AddInvokeTransactionResponse, error) {
	return a.Transaction.Invoke(signer)
}

func (a *BackupAttestTracker) UpdateNonce(signer signerP.Signer) error {
	return a.Transaction.UpdateNonce(signer)
}

func (a *BackupAttestTracker) IsTxnReady() bool {
	return a.Transaction.Valid()
}
