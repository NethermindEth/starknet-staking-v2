package validator

import (
	"context"
	"fmt"

	"github.com/NethermindEth/juno/core/felt"
	junoUtils "github.com/NethermindEth/juno/utils"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
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
	CurrentBlockNumber uint64
	EpochID            uint64
	EpochStartingBlock uint64
	EpochEndingBlock   uint64
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

func (a *BackupAttestTracker) Refresh(currentBlockNumber uint64) error {
	if a.originalEpochInfo.StartingEpoch == 0 {
		a.logger.Debug("no original epoch info found, fetching from contract")
		epochInfoResp, err := a.getEpochInfo()
		if err != nil {
			return fmt.Errorf("failed to get original epoch info: %w", err)
		}
		a.originalEpochInfo = epochInfoResp
	}

	a.logger.Debugw("epoch info synced", "epoch info", a.originalEpochInfo)

	if a.attestationWindow == 0 {
		a.logger.Debug("no attestation window found, fetching from contract")
		attestationWindow, err := a.getAttestationWindow()
		if err != nil {
			return fmt.Errorf("failed to get attestation window: %w", err)
		}
		a.attestationWindow = attestationWindow
	}

	a.logger.Debugw("attestation window synced", "attestation window", a.attestationWindow)

	a.currentAttestInfo = calculateCurrentAttestInfo(
		a.originalEpochInfo,
		a.attestationWindow,
		currentBlockNumber,
	)
	a.logger.Debugw("current attest info calculated", "current attest info", a.currentAttestInfo)

	return nil
}

// getEpochInfo fetches the epoch info from the staking contract.
// Since this value almost never changes, we should be able to fetch it
// even from a lagging node.
func (a *BackupAttestTracker) getEpochInfo() (epochInfo, error) {
	epochInfoReq := rpc.FunctionCall{
		ContractAddress:    a.contracts.Staking.Felt(),
		EntryPointSelector: utils.GetSelectorFromNameFelt("get_epoch_info"),
		Calldata:           []*felt.Felt{},
	}

	resp, err := a.provider.Call(
		a.ctx,
		epochInfoReq,
		rpc.WithBlockTag(rpc.BlockTagLatest),
	)
	if err != nil {
		return epochInfo{}, err
	}

	return epochInfo{
		Length:        resp[1].Uint64(),
		StartingBlock: resp[2].Uint64(),
		StartingEpoch: resp[3].Uint64(),
	}, nil
}

// getAttestationWindow fetches the attestation window from the attest contract.
// Since this value almost never changes, we should be able to fetch it
// even from a lagging node.
func (a *BackupAttestTracker) getAttestationWindow() (uint64, error) {
	attestationWindowReq := rpc.FunctionCall{
		ContractAddress:    a.contracts.Attest.Felt(),
		EntryPointSelector: utils.GetSelectorFromNameFelt("attestation_window"),
		Calldata:           []*felt.Felt{},
	}

	resp, err := a.provider.Call(
		a.ctx,
		attestationWindowReq,
		rpc.WithBlockTag(rpc.BlockTagLatest),
	)
	if err != nil {
		return 0, err
	}

	return resp[0].Uint64(), nil
}

func calculateCurrentAttestInfo(
	epochInfo epochInfo,
	attestationWindow uint64,
	currentBlockNumber uint64,
) AttestAndEpochInfo {
	epochsPassed := (currentBlockNumber - epochInfo.StartingBlock) / epochInfo.Length
	currentEpoch := epochInfo.StartingEpoch + epochsPassed
	epochStartingBlock := epochInfo.StartingBlock + (epochsPassed * epochInfo.Length)
	epochEndingBlock := epochStartingBlock + epochInfo.Length

	// @todo finish this
	return AttestAndEpochInfo{
		AttestInfo: types.AttestInfo{
			TargetBlock:     types.BlockNumber(epochInfo.StartingBlock),
			TargetBlockHash: types.BlockHash{},
			WindowStart:     types.BlockNumber(epochInfo.StartingBlock),
			WindowEnd:       types.BlockNumber(epochInfo.StartingBlock + epochInfo.Length),
		},
		CurrentBlockNumber: currentBlockNumber,
		EpochID:            currentEpoch,
		EpochStartingBlock: epochStartingBlock,
		EpochEndingBlock:   epochEndingBlock,
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
