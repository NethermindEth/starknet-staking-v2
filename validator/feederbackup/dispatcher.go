package feederbackup

import (
	"github.com/NethermindEth/juno/core/felt"
	junoUtils "github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
)

var _ validator.AttestTracker = (*FeederAttestTracker)(nil)

type FeederAttestTracker struct {
	Transaction validator.AttestTransaction
	hash        felt.Felt
	status      validator.AttestStatus
}

func (a *FeederAttestTracker) NewAttestTracker() validator.AttestTracker {
	return &FeederAttestTracker{
		Transaction: validator.AttestTransaction{},
		hash:        felt.Zero,
		status:      validator.Iddle,
	}
}

func (a *FeederAttestTracker) Hash() felt.Felt {
	return a.hash
}

func (a *FeederAttestTracker) SetHash(hash felt.Felt) {
	a.hash = hash
}

func (a *FeederAttestTracker) Status() validator.AttestStatus {
	return a.status
}

func (a *FeederAttestTracker) SetStatus(status validator.AttestStatus) {
	a.status = status
}

func (a *FeederAttestTracker) UpdateStatus(signer signerP.Signer, logger *junoUtils.ZapLogger) {
	status := validator.TrackAttest(signer, logger, &a.hash)
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
