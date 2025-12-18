package validator

import (
	"github.com/NethermindEth/juno/core/felt"
	junoUtils "github.com/NethermindEth/juno/utils"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
)

// AttestTrackerI is the interface that wraps the basic methods for an attest tracker.
type AttestTrackerI interface {
	Hash() felt.Felt
	NewAttestTracker() AttestTrackerI
	Status() AttestStatus
	SetHash(hash felt.Felt)
	SetStatus(status AttestStatus)
	UpdateStatus(signer signerP.Signer, logger *junoUtils.ZapLogger)

	// From the AttestTransaction type
	BuildTxn(signer signerP.Signer, blockHash *types.BlockHash) error
	Attest(signer signerP.Signer) (rpc.AddInvokeTransactionResponse, error)
	UpdateNonce(signer signerP.Signer) error
	IsTxnReady() bool
}

func (a *AttestTracker) Hash() felt.Felt {
	return a.hash
}

func (a *AttestTracker) SetHash(hash felt.Felt) {
	a.hash = hash
}

func (a *AttestTracker) Status() AttestStatus {
	return a.status
}

// From the AttestTransaction type, implementing the AttestTracker interface

func (a *AttestTracker) BuildTxn(signer signerP.Signer, blockHash *types.BlockHash) error {
	return a.Transaction.Build(signer, blockHash)
}

func (a *AttestTracker) Attest(
	signer signerP.Signer,
) (rpc.AddInvokeTransactionResponse, error) {
	return a.Transaction.Invoke(signer)
}

func (a *AttestTracker) UpdateNonce(signer signerP.Signer) error {
	return a.Transaction.UpdateNonce(signer)
}

func (a *AttestTracker) IsTxnReady() bool {
	return a.Transaction.Valid()
}
