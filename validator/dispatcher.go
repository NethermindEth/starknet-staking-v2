package validator

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
	"go.uber.org/zap"
)

// Created a function variable for mocking purposes in tests
var Sleep = time.Sleep

var ErrTxnHashNotFound = rpc.ErrHashNotFound

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
	logger log.Logger,
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
	// Event channels
	DoAttest      chan types.DoAttest
	PrepareAttest chan types.PrepareAttest
	EndOfWindow   chan struct{}
	// Current epoch attest-related fields
	CurrentAttest AttestTracker
}

func NewEventDispatcher[S signerP.Signer]() EventDispatcher[S] {
	return EventDispatcher[S]{
		CurrentAttest: NewAttestTracker(),
		DoAttest:      make(chan types.DoAttest),
		PrepareAttest: make(chan types.PrepareAttest),
		EndOfWindow:   make(chan struct{}),
	}
}

//nolint:gocyclo // Refactor in another time
func (d *EventDispatcher[S]) Dispatch(
	signer S, balanceThreshold float64, logger log.Logger, tracer metrics.Tracer,
) {
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
			logger.Debug(
				"building attest transaction for blockhash",
				zap.String("blockhash", targetBlockHash.String()),
			)
			err := d.CurrentAttest.Transaction.Build(signer, &targetBlockHash)
			if err != nil {
				logger.Error("failed to build attest transaction", zap.Error(err))

				continue
			}
			logger.Debug("attest transaction built successfully")

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
				logger.Debug(
					"building attest transaction (in `do` stage) for blockhash",
					zap.String("blockhash", targetBlockHash.String()),
				)
				err := d.CurrentAttest.Transaction.Build(signer, &targetBlockHash)
				if err != nil {
					logger.Error("failed to build attest transaction", zap.Error(err))

					continue
				}
				logger.Debug("attest transaction built successfully")
			} else {
				// Otherwise, the tx was prepared in advance. Update the transaction nonce
				// since it was set some blocks ago
				logger.Debug("updating attest transaction nonce")
				err := d.CurrentAttest.Transaction.UpdateNonce(signer)
				if err != nil {
					logger.Error("failed to update transaction nonce", zap.Error(err))

					continue
				}
			}

			logger.Info("invoking attest", zap.String("target block hash", targetBlockHash.String()))
			resp, err := d.CurrentAttest.Transaction.Invoke(signer)
			if err != nil {
				if strings.Contains(err.Error(), "Attestation is done for this epoch") {
					logger.Info(
						"attestation is already done for this epoch",
					)
					d.CurrentAttest.setStatus(Successful)

					continue
				}

				logger.Error(
					"failed to attest",
					zap.Error(err),
				)
				d.CurrentAttest.setStatus(Failed)

				continue
			}

			logger.Debug("attest transaction sent", zap.String("hash", resp.Hash.String()))
			d.CurrentAttest.Hash = *resp.Hash
			// Record attestation submission in metrics
			tracer.RecordAttestationSubmitted()

		case <-d.EndOfWindow:
			logger.Info("end of window reached")
			if d.CurrentAttest.Status != Successful {
				d.CurrentAttest.UpdateStatus(signer, logger)
			}
			if d.CurrentAttest.Status == Successful {
				logger.Info(
					"successfully attested to target block",
					zap.String("targetBlockHash", targetBlockHash.String()),
				)
				tracer.RecordAttestationConfirmed()
			} else {
				logger.Warn(
					"failed to attest to target block",
					zap.String("targetBlockHash", targetBlockHash.String()),
					zap.Uint8("latestAttestStatus", uint8(d.CurrentAttest.Status)),
				)
				tracer.RecordAttestationFailure()
			}
			// clean slate for the next window
			d.CurrentAttest = NewAttestTracker()
			// check the account balance
			go CheckBalance(signer, balanceThreshold, logger, tracer)
		}
	}
}

func TrackAttest[S signerP.Signer](
	signer S,
	logger log.Logger,
	txHash *felt.Felt,
) AttestStatus {
	txStatus, err := signer.TransactionStatus(txHash)
	if err != nil {
		if err.Error() == ErrTxnHashNotFound.Error() {
			logger.Info(
				"attest transaction status was not found. Will wait.",
				zap.String("transactionHash", txHash.String()),
			)

			return Ongoing
		} else {
			logger.Error(
				"attest transaction FAILED. Will retry.",
				zap.String("transactionHash", txHash.String()),
				zap.Error(err),
			)

			return Failed
		}
	}

	if txStatus.ExecutionStatus == rpc.TxnExecutionStatusREVERTED {
		logger.Error(
			"attest transaction REVERTED. Will retry.",
			zap.String("transactionHash", txHash.String()),
			zap.String("failureReason", txStatus.FailureReason),
		)

		return Failed
	}

	switch txStatus.FinalityStatus {
	case rpc.TxnStatusReceived, rpc.TxnStatusCandidate, rpc.TxnStatusPreConfirmed:
		logger.Info(
			"attest transaction in processing. Will wait",
			zap.String("transactionHash", txHash.String()),
			zap.String("finalityStatus", string(txStatus.FinalityStatus)),
		)

		return Ongoing
	default:
		logger.Info(
			"attest transaction SUCCESSFUL.",
			zap.String("transactionHash", txHash.String()),
			zap.String("finalityStatus", string(txStatus.FinalityStatus)),
			zap.String("executionStatus", string(txStatus.ExecutionStatus)),
		)

		return Successful
	}
}
