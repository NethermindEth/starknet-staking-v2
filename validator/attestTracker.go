package validator

import (
	"errors"
	"fmt"

	"github.com/NethermindEth/juno/core/felt"
	junoUtils "github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
)

type AttestTracker struct {
	Transaction AttestTransaction
	Hash        felt.Felt
	Status      AttestStatus
}

func NewAttestTracker() AttestTracker {
	//nolint:exhaustruct // Using default values
	return AttestTracker{
		Transaction: AttestTransaction{},
		Status:      Iddle,
	}
}

func (a *AttestTracker) UpdateStatus(
	signer signerP.Signer,
	logger *junoUtils.ZapLogger,
) {
	status := TrackAttest(signer, logger, &a.Hash)
	a.SetStatus(status)
}

func (a *AttestTracker) SetStatus(status AttestStatus) {
	a.Status = status
	switch status {
	case Ongoing, Successful:
	case Failed:
		a.Hash = felt.Zero
	case Iddle:
		panic("status cannot be change to iddle")
	default:
		panic(fmt.Sprintf("unknown tracker status %d", status))
	}
}

type AttestStatus uint8

var ErrTxnHashNotFound = rpc.ErrHashNotFound

const (
	Iddle AttestStatus = iota
	Ongoing
	Successful
	Failed
)

type AttestTransaction struct {
	txn   rpc.BroadcastInvokeTxnV3
	valid bool
}

func (t *AttestTransaction) Build(signer signerP.Signer, blockHash *types.BlockHash) error {
	t.valid = false

	var err error
	t.txn, err = signer.BuildAttestTransaction(blockHash)
	if err != nil {
		return fmt.Errorf("signer failed building the transaction: %w", err)
	}

	_, err = signer.SignTransaction(&t.txn)
	if err != nil {
		return fmt.Errorf("signer failed to sign the transaction: %w", err)
	}

	t.valid = true

	return nil
}

func (t *AttestTransaction) Invoke(signer signerP.Signer) (
	rpc.AddInvokeTransactionResponse, error,
) {
	var resp rpc.AddInvokeTransactionResponse
	if !t.valid {
		return resp, errors.New("invoking attest transaction before building it")
	}
	t.valid = false

	// todo(rdr): make sure to estimate fee with query bit with Braavos Account
	estimate, err := signer.EstimateFee(&t.txn)
	if err != nil {
		return resp, fmt.Errorf("signer failed to estimate fee: %w", err)
	}
	t.txn.ResourceBounds = utils.FeeEstToResBoundsMap(estimate, constants.FeeEstimationMultiplier)

	// patch for making sure txn.Version is correct
	t.txn.Version = rpc.TransactionV3

	_, err = signer.SignTransaction(&t.txn)
	if err != nil {
		return resp, fmt.Errorf("signer failed to sign the transaction: %w", err)
	}

	resp, err = signer.InvokeTransaction(&t.txn)
	if err != nil {
		return resp, fmt.Errorf("signer failed to invoke the transaction: %w", err)
	}

	return resp, nil
}

func (t *AttestTransaction) UpdateNonce(signer signerP.Signer) error {
	if !t.valid {
		return errors.New("updating the transaction nonce before building the transaction")
	}
	newNonce, err := signer.Nonce()
	if err != nil {
		return fmt.Errorf("signer failed to get the nonce: %w", err)
	}
	if !t.txn.Nonce.Equal(newNonce) {
		t.txn.Nonce = newNonce
		_, err := signer.SignTransaction(&t.txn)
		if err != nil {
			return fmt.Errorf("signer failed to sign the transaction: %w", err)
		}
	}

	return nil
}

// I want to name this built or smth like that
func (t *AttestTransaction) Valid() bool {
	return t.valid
}

func TrackAttest[S signerP.Signer](
	signer S,
	logger *junoUtils.ZapLogger,
	txHash *felt.Felt,
) AttestStatus {
	txStatus, err := signer.TransactionStatus(txHash)
	if err != nil {
		if err.Error() == ErrTxnHashNotFound.Error() {
			logger.Infow(
				"attest transaction status was not found. Will wait.",
				"transaction hash", txHash,
			)

			return Ongoing
		} else {
			logger.Errorw(
				"attest transaction FAILED. Will retry.",
				"transaction hash", txHash,
				"error", err,
			)

			return Failed
		}
	}

	if txStatus.ExecutionStatus == rpc.TxnExecutionStatusREVERTED {
		logger.Errorw(
			"attest transaction REVERTED. Will retry.",
			"transaction hash", txHash,
			"failure reason", txStatus.FailureReason,
		)

		return Failed
	}

	switch txStatus.FinalityStatus {
	case rpc.TxnStatusReceived, rpc.TxnStatusCandidate, rpc.TxnStatusPreConfirmed:
		logger.Infow(
			fmt.Sprintf("attest transaction %s. Will wait.", txStatus.FinalityStatus),
			"hash", txHash,
		)

		return Ongoing
	default:
		logger.Infow(
			"attest transaction SUCCESSFUL.",
			"transaction hash", txHash,
			"finality status", txStatus.FinalityStatus,
			"execution status", txStatus.ExecutionStatus,
		)

		return Successful
	}
}
