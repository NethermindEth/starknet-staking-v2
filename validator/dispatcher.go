package validator

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	junoUtils "github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
	"github.com/sourcegraph/conc"
)

// Created a function variable for mocking purposes in tests
var Sleep = time.Sleep

var ErrTxnHashNotFound = rpc.RPCError{Code: 29, Message: "Transaction hash not found"}

type AttestStatus uint8

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
		return err
	}

	_, err = signer.SignTransaction(&t.txn)
	if err != nil {
		return err
	}

	t.valid = err == nil
	return err
}

func (t *AttestTransaction) Invoke(signer signerP.Signer) (
	*rpc.AddInvokeTransactionResponse, error,
) {
	if !t.valid {
		return nil, errors.New("invoking attest transaction before building it")
	}
	t.valid = false

	// todo(rdr): make sure to estimate fee with query bit with Braavos Account
	estimate, err := signer.EstimateFee(&t.txn)
	if err != nil {
		return nil, err
	}
	t.txn.ResourceBounds = utils.FeeEstToResBoundsMap(estimate, 1.5)

	// patch for making sure txn.Version is correct
	t.txn.Version = rpc.TransactionV3

	_, err = signer.SignTransaction(&t.txn)
	if err != nil {
		return nil, err
	}
	return signer.InvokeTransaction(&t.txn)
}

func (t *AttestTransaction) UpdateNonce(signer signerP.Signer) error {
	if !t.valid {
		return errors.New("updating transaction nonce before building it")
	}
	newNonce, err := signer.Nonce()
	if err != nil {
		return err
	}
	if !t.txn.Nonce.Equal(newNonce) {
		t.txn.Nonce = newNonce
		_, err := signer.SignTransaction(&t.txn)
		if err != nil {
			return err
		}
	}
	return nil
}

// I want to name this built or smth like that
func (t *AttestTransaction) Valid() bool {
	return t.valid
}

type AttestTracker struct {
	Transaction AttestTransaction
	Hash        felt.Felt
	Status      AttestStatus
}

func NewAttestTracker() AttestTracker {
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
	a.setStatus(status)
}

func (a *AttestTracker) setStatus(status AttestStatus) {
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

type EventDispatcher[S signerP.Signer] struct {
	// Current epoch attest-related fields
	CurrentAttest AttestTracker
	// Event channels
	DoAttest      chan types.DoAttest
	PrepareAttest chan types.PrepareAttest
	EndOfWindow   chan struct{}
}

func NewEventDispatcher[S signerP.Signer]() EventDispatcher[S] {
	return EventDispatcher[S]{
		CurrentAttest: NewAttestTracker(),
		DoAttest:      make(chan types.DoAttest),
		PrepareAttest: make(chan types.PrepareAttest),
		EndOfWindow:   make(chan struct{}),
	}
}

func (d *EventDispatcher[S]) Dispatch(signer S, logger *junoUtils.ZapLogger, tracer metrics.Tracer) {
	wg := conc.NewWaitGroup()
	defer wg.Wait()

	// Block hash to attest to
	var targetBlockHash types.BlockHash

	for {
		select {
		case attest, ok := <-d.PrepareAttest:
			if !ok {
				return
			}
			if d.CurrentAttest.Status != Iddle {
				logger.Error("receiveing prepare attest info while doing attest")
			}
			if d.CurrentAttest.Transaction.Valid() {
				continue
			}

			targetBlockHash = attest.BlockHash
			logger.Debugf("preparing attest transaction for blockhash: %s", targetBlockHash.String())
			err := d.CurrentAttest.Transaction.Build(signer, &targetBlockHash)
			if err != nil {
				logger.Errorf("failed to build attest transaction: %s", err.Error())
				continue
			}
			logger.Debug("built attest transaction successfully")

		case attest, ok := <-d.DoAttest:
			if !ok {
				return
			}

			// if the attest event is already being tracked by the tool
			if d.CurrentAttest.Status != Iddle && d.CurrentAttest.Status != Failed {
				// If  status is still not successful, check for it
				if d.CurrentAttest.Status != Successful {
					d.CurrentAttest.UpdateStatus(signer, logger)
				}
				// If status is status is already successful or ongoing, do nothing.
				if d.CurrentAttest.Status == Successful || d.CurrentAttest.Status == Ongoing {
					continue
				}
			}
			d.CurrentAttest.setStatus(Ongoing)

			// Case when the validator is initiated mid window and didn't have time to prepare
			// or the transaction invoke failed.
			if !d.CurrentAttest.Transaction.Valid() {
				targetBlockHash = attest.BlockHash
				logger.Debugf("building attest transaction (in `do` stage) for blockhash: %s", &targetBlockHash)
				err := d.CurrentAttest.Transaction.Build(signer, &targetBlockHash)
				if err != nil {
					logger.Errorf("failed to build attest transaction: %s", err.Error())
					continue
				}
				logger.Debug("built attest transaction successfully")
			} else {
				// Otherwise, the tx was prepared in advance. Update the transaction nonce
				// since it was set some blocks ago
				logger.Debug("updating attest transaction nonce")
				err := d.CurrentAttest.Transaction.UpdateNonce(signer)
				if err != nil {
					logger.Errorf("failed to update transaction nonce: %s", err.Error())
					continue
				}
			}

			logger.Infow("Invoking attest", "block hash", targetBlockHash.String())
			resp, err := d.CurrentAttest.Transaction.Invoke(signer)
			if err != nil {
				if strings.Contains(err.Error(), "Attestation is done for this epoch") {
					logger.Infow(
						"Attestation is already done for this epoch",
						"block hash", targetBlockHash.String(),
					)
					d.CurrentAttest.setStatus(Successful)
					continue
				}

				logger.Errorw(
					"Failed to attest",
					"block hash", targetBlockHash.String(),
					"error", err,
				)
				d.CurrentAttest.setStatus(Failed)

				continue
			}
			logger.Debugw("Attest transaction sent", "hash", resp.Hash)
			d.CurrentAttest.Hash = *resp.Hash
			// Record attestation submission in metrics
			tracer.RecordAttestationSubmitted()

		case <-d.EndOfWindow:
			logger.Info("End of window reached")
			if d.CurrentAttest.Status != Successful {
				d.CurrentAttest.UpdateStatus(signer, logger)
			}
			if d.CurrentAttest.Status == Successful {
				logger.Infow(
					"Successfully attested to target block",
					"target block hash", targetBlockHash.String(),
				)
				tracer.RecordAttestationConfirmed()
			} else {
				logger.Warnw(
					"Failed to attest to target block",
					"target block hash", targetBlockHash.String(),
					"latest attest status", d.CurrentAttest.Status,
				)
				tracer.RecordAttestationFailure()
			}
			// clean slate for the next window
			d.CurrentAttest = NewAttestTracker()
		}
	}
}

func TrackAttest[S signerP.Signer](
	signer S,
	logger *junoUtils.ZapLogger,
	txHash *felt.Felt,
) AttestStatus {
	txStatus, err := signer.GetTransactionStatus(txHash)
	if err != nil {
		if err.Error() == ErrTxnHashNotFound.Error() {
			logger.Infow(
				"Attest transaction status was not found. Will wait.",
				"hash", txHash,
			)
			return Ongoing
		} else {
			logger.Errorw(
				"Attest transaction FAILED. Will retry.",
				"transaction hash", txHash,
				"error", err,
			)
			return Failed
		}
	}

	if txStatus.FinalityStatus == rpc.TxnStatus_Received {
		logger.Infow(
			"Attest transaction RECEIVED.",
			"hash", txHash,
		)
		return Ongoing
	}

	if txStatus.FinalityStatus == rpc.TxnStatus_Rejected {
		// TODO: are we guaranteed err is nil if tx got rejected ?
		logger.Errorw(
			"Attest transaction REJECTED",
			"transaction hash", txHash,
		)
		return Failed
	}

	if txStatus.ExecutionStatus == rpc.TxnExecutionStatusREVERTED {
		logger.Errorw(
			"Attest transaction REVERTED",
			"transaction hash", txHash,
			"failure reason", txStatus.FailureReason,
		)
		return Failed
	}

	logger.Infow(
		"Attest transaction SUCCESSFUL",
		"transaction hash", txHash,
		"finality status", txStatus.FinalityStatus,
		"execution status", txStatus.ExecutionStatus,
	)
	return Successful
}
