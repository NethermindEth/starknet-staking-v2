package validator

import (
	"errors"
	"fmt"
	"strings"
	"sync"
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

// Consecutive blocks during which the node may report a sent attest transaction as not found,
// or fail to return its status, before the attestation is sent again. Sending it again cannot
// attest twice: once the first transaction is executed, the fee estimation of the new one fails
// with "Attestation is done for this epoch", which counts as a success.
const maxUnknownStatusPolls = 5

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
			// The signature no longer matches the nonce, so the transaction must be rebuilt
			t.valid = false

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
	// Consecutive status polls that could not tell what happened to the sent transaction
	unknownPolls int
}

func NewAttestTracker() AttestTracker {
	//nolint:exhaustruct_v5 // Using default values
	return AttestTracker{
		Transaction: AttestTransaction{},
		Status:      Iddle,
	}
}

func (a *AttestTracker) UpdateStatus(
	signer signerP.Signer,
	logger log.Logger,
) {
	status, known := trackAttest(signer, logger, &a.Hash)
	if known {
		a.unknownPolls = 0
	} else {
		a.unknownPolls++
	}
	a.setStatus(status)
}

// Whether a transaction was sent but the last status poll could not tell what happened to it
func (a *AttestTracker) statusUnknown() bool {
	return a.Status == Ongoing && a.unknownPolls > 0
}

func (a *AttestTracker) setStatus(status AttestStatus) {
	a.Status = status
	switch status {
	case Ongoing, Successful:
	case Failed:
		a.Hash = felt.Zero
		a.unknownPolls = 0
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
	// Blocks whose events were already sent, so they are never sent twice
	processedBlocks *processedBlocks
}

func NewEventDispatcher[S signerP.Signer]() EventDispatcher[S] {
	return EventDispatcher[S]{
		CurrentAttest:   NewAttestTracker(),
		DoAttest:        make(chan types.DoAttest),
		PrepareAttest:   make(chan types.PrepareAttest),
		EndOfWindow:     make(chan struct{}),
		processedBlocks: new(processedBlocks),
	}
}

// Highest block number processed. The node replays its latest block header on every new
// subscription, so a header received again after a resubscription is recognised as processed.
// It is shared by the `ProcessBlockHeaders` routines of successive subscriptions, which may
// briefly overlap.
type processedBlocks struct {
	mu        sync.Mutex
	processed bool
	highest   uint64
}

// Records the block number, returning false if it is not higher than every block recorded
// before
func (p *processedBlocks) add(blockNumber uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.processed && blockNumber <= p.highest {
		return false
	}
	p.processed = true
	p.highest = blockNumber

	return true
}

//nolint:gocyclo,funlen // Refactor in another time
func (d *EventDispatcher[S]) Dispatch(
	signer S,
	rewardSigner signerP.RewardSigner,
	balanceThreshold float64,
	logger log.Logger, tracer metrics.Tracer,
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

			if d.CurrentAttest.Status == Successful {
				continue
			}
			// A transaction was sent: check its status and keep waiting unless it failed
			if d.CurrentAttest.Status == Ongoing {
				d.CurrentAttest.UpdateStatus(signer, logger)
				if d.CurrentAttest.unknownPolls >= maxUnknownStatusPolls {
					logger.Warn(
						"attest transaction status still unknown. Sending the attestation again",
						zap.String("transactionHash", d.CurrentAttest.Hash.String()),
						zap.Int("blocksUnknown", d.CurrentAttest.unknownPolls),
					)
					d.CurrentAttest.setStatus(Failed)
				}
				if d.CurrentAttest.Status != Failed {
					continue
				}
			}

			// Nothing was sent yet or the last attempt failed: (re)send the attest transaction.
			// Any failure below marks the attempt as failed, so it is retried on the next block

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
					logger.Error(
						"failed to build attest transaction. Will retry on the next block",
						zap.Error(err),
					)
					d.CurrentAttest.setStatus(Failed)

					continue
				}
				logger.Debug("attest transaction built successfully")
			} else {
				// Otherwise, the tx was prepared in advance. Update the transaction nonce
				// since it was set some blocks ago
				logger.Debug("updating attest transaction nonce")
				err := d.CurrentAttest.Transaction.UpdateNonce(signer)
				if err != nil {
					logger.Error(
						"failed to update transaction nonce. Will retry on the next block",
						zap.Error(err),
					)
					d.CurrentAttest.setStatus(Failed)

					continue
				}
			}

			logger.Info(
				"invoking attest",
				zap.String("target block hash", targetBlockHash.String()),
			)
			resp, err := d.CurrentAttest.Transaction.Invoke(signer)
			if err != nil {
				if strings.Contains(err.Error(), "Attestation is done for this epoch") {
					logger.Info("attestation is already done for this epoch")
					d.CurrentAttest.setStatus(Successful)

					continue
				}

				logger.Error(
					"failed to attest. Will retry on the next block",
					zap.Error(err),
				)
				d.CurrentAttest.setStatus(Failed)

				continue
			}

			logger.Debug("attest transaction sent", zap.String("hash", resp.Hash.String()))
			// Only a sent transaction is tracked, so `Ongoing` always comes with its hash
			d.CurrentAttest.Hash = *resp.Hash
			d.CurrentAttest.setStatus(Ongoing)
			// Record attestation submission in metrics
			tracer.RecordAttestationSubmitted()

		case <-d.EndOfWindow:
			logger.Info("end of window reached")
			// Only a sent transaction has a status to check. When nothing was sent or the
			// last attempt failed there is no transaction hash to look for
			if d.CurrentAttest.Status == Ongoing {
				d.CurrentAttest.UpdateStatus(signer, logger)
			}
			switch {
			case d.CurrentAttest.Status == Successful:
				logger.Info(
					"successfully attested to target block",
					zap.String("targetBlockHash", targetBlockHash.String()),
				)
				tracer.RecordAttestationConfirmed()
			case d.CurrentAttest.statusUnknown():
				// The node cannot say what happened to the transaction, which may well have
				// succeeded: counting a failure would raise false alarms
				logger.Warn(
					"end of window reached without knowing if the attestation succeeded. "+
						"Not counted as a failure, check the transaction on a block explorer",
					zap.String("targetBlockHash", targetBlockHash.String()),
					zap.String("transactionHash", d.CurrentAttest.Hash.String()),
				)
			default:
				logger.Warn(
					"failed to attest to target block",
					zap.String("targetBlockHash", targetBlockHash.String()),
					zap.Uint8("latestAttestStatus", uint8(d.CurrentAttest.Status)),
				)
				tracer.RecordAttestationFailure()
			}
			// clean slate for the next window
			d.CurrentAttest = NewAttestTracker()
			// check the accounts balances
			go CheckBalance(signer, rewardSigner, balanceThreshold, logger, tracer)

			// Claim rewards if the feature is enabled
			if rewardSigner != nil {
				go rewardSigner.ClaimRewards(tracer)
			}
		}
	}
}

// Returns the status of the attest transaction. When the node reports it as not found or fails
// to return its status, the transaction is still considered `Ongoing`.
func TrackAttest[S signerP.Signer](
	signer S,
	logger log.Logger,
	txHash *felt.Felt,
) AttestStatus {
	status, _ := trackAttest(signer, logger, txHash)

	return status
}

// Same as `TrackAttest`, also returning false when the status poll could not tell what happened
// to the transaction (not found by the node, or the status request failed)
func trackAttest[S signerP.Signer](
	signer S,
	logger log.Logger,
	txHash *felt.Felt,
) (AttestStatus, bool) {
	// A zero hash means no transaction was sent, so there is nothing the node could find
	if txHash.IsZero() {
		logger.Warn("no attest transaction to track. Will retry")

		return Failed, true
	}

	txStatus, err := signer.TransactionStatus(txHash)
	if err != nil {
		if err.Error() == ErrTxnHashNotFound.Error() {
			logger.Info(
				"attest transaction status was not found. Will check again on the next block",
				zap.String("transactionHash", txHash.String()),
			)
		} else {
			logger.Warn(
				"failed to get the attest transaction status. Will check again on the next block",
				zap.String("transactionHash", txHash.String()),
				zap.Error(err),
			)
		}

		return Ongoing, false
	}

	if txStatus.ExecutionStatus == rpc.TxnExecutionStatusREVERTED {
		logger.Error(
			"attest transaction REVERTED. Will retry.",
			zap.String("transactionHash", txHash.String()),
			zap.String("failureReason", txStatus.FailureReason),
		)

		return Failed, true
	}

	switch txStatus.FinalityStatus {
	case rpc.TxnStatusReceived, rpc.TxnStatusCandidate, rpc.TxnStatusPreConfirmed:
		logger.Info(
			"attest transaction in processing. Will wait",
			zap.String("transactionHash", txHash.String()),
			zap.String("finalityStatus", string(txStatus.FinalityStatus)),
		)

		return Ongoing, true
	default:
		logger.Info(
			"attest transaction SUCCESSFUL.",
			zap.String("transactionHash", txHash.String()),
			zap.String("finalityStatus", string(txStatus.FinalityStatus)),
			zap.String("executionStatus", string(txStatus.ExecutionStatus)),
		)

		return Successful, true
	}
}
