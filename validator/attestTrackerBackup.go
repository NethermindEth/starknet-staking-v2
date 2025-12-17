package validator

import (
	"context"

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
	contracts         *types.ValidationContracts
}

type epochInfo struct {
	length        uint64
	startingBlock uint64
	startingEpoch uint64
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

func (a *BackupAttestTracker) Sync() error {
	if a.originalEpochInfo.startingEpoch == 0 {
		a.logger.Debug("no epoch info found, fetching from contract")
		epochInfoResp, err := a.getEpochInfo()
		if err != nil {
			return err
		}
		a.originalEpochInfo = epochInfoResp
	}

	a.logger.Infow("epoch info synced", "epoch info", a.originalEpochInfo)

	return nil
}

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
		length:        resp[1].Uint64(),
		startingBlock: resp[2].Uint64(),
		startingEpoch: resp[3].Uint64(),
	}, nil
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
