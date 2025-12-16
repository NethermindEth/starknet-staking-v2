package validator

import (
	"github.com/NethermindEth/juno/core/felt"
	junoUtils "github.com/NethermindEth/juno/utils"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
)

var _ AttestTracker = (*FeederAttestTracker)(nil)

type FeederAttestTracker struct {
	Transaction AttestTransaction
	hash        felt.Felt
	status      AttestStatus
}

func (a *FeederAttestTracker) NewAttestTracker() AttestTracker {
	//nolint:exhaustruct // Using default values
	return &FeederAttestTracker{
		Transaction: AttestTransaction{},
		hash:        felt.Zero,
		status:      Iddle,
	}
}

func (a *FeederAttestTracker) Hash() felt.Felt {
	return a.hash
}

func (a *FeederAttestTracker) SetHash(hash felt.Felt) {
	a.hash = hash
}

func (a *FeederAttestTracker) Status() AttestStatus {
	return a.status
}

func (a *FeederAttestTracker) SetStatus(status AttestStatus) {
	a.status = status
}

func (a *FeederAttestTracker) UpdateStatus(signer signerP.Signer, logger *junoUtils.ZapLogger) {
	status := TrackAttest(signer, logger, &a.hash)
	a.SetStatus(status)
}

func (a *FeederAttestTracker) BuildTxn(signer signerP.Signer, blockHash *types.BlockHash) error {
	return a.Transaction.Build(signer, blockHash)
}

func (a *FeederAttestTracker) Attest(
	signer signerP.Signer,
) (rpc.AddInvokeTransactionResponse, error) {
	return a.Transaction.Invoke(signer)
}

func (a *FeederAttestTracker) UpdateNonce(signer signerP.Signer) error {
	return a.Transaction.UpdateNonce(signer)
}

func (a *FeederAttestTracker) IsTxnReady() bool {
	return a.Transaction.Valid()
}
