package validator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/sourcegraph/conc"
)

// Created a function variable for mocking purposes in tests
var Sleep = time.Sleep

var ErrTxnHashNotFound = rpc.RPCError{Code: 29, Message: "Transaction hash not found"}

type AttestStatus uint8

const (
	Ongoing AttestStatus = iota + 1
	Successful
	Failed
)

type AttestTracker struct {
	Event           AttestRequired
	TransactionHash felt.Felt
	Status          AttestStatus
}

func NewAttestTracker() AttestTracker {
	return AttestTracker{
		Event:           AttestRequired{},
		TransactionHash: felt.Zero,
		Status:          Failed,
	}
}

func (a *AttestTracker) setOngoing() {
	a.Status = Ongoing
}

func (a *AttestTracker) setSuccessful() {
	a.Status = Successful
}

func (a *AttestTracker) setFailed() {
	a.Status = Failed
}

func (a *AttestTracker) setEvent(event *AttestRequired) {
	a.Event = *event
}

func (a *AttestTracker) setTransactionHash(txHash *felt.Felt) {
	a.TransactionHash = *txHash
}

func (a *AttestTracker) resetTransactionHash() {
	a.TransactionHash = felt.Zero
}

type EventDispatcher[S signerP.Signer] struct {
	// Current epoch attest-related fields
	CurrentAttest AttestTracker
	// AttestFee     types.AttestFee
	// Event channels
	AttestRequired chan AttestRequired
	EndOfWindow    chan struct{}
}

func NewEventDispatcher[S signerP.Signer]() EventDispatcher[S] {
	return EventDispatcher[S]{
		CurrentAttest: NewAttestTracker(),
		// AttestFee:      *attestFee,
		AttestRequired: make(chan AttestRequired),
		EndOfWindow:    make(chan struct{}),
	}
}

func (d *EventDispatcher[S]) Dispatch(
	signer S,
	logger *utils.ZapLogger,
	metricsServer *metrics.Metrics,
) {
	wg := conc.NewWaitGroup()
	defer wg.Wait()

	// var failedAttests uint64 = 0

	for {
		select {
		case event, ok := <-d.AttestRequired:
			if !ok {
				return
			}

			if event == d.CurrentAttest.Event &&
				d.CurrentAttest.Status != Successful &&
				d.CurrentAttest.TransactionHash != felt.Zero {
				setAttestStatusOnTracking(signer, logger, &d.CurrentAttest)
			}

			if event == d.CurrentAttest.Event &&
				(d.CurrentAttest.Status == Ongoing || d.CurrentAttest.Status == Successful) {
				continue
			}

			d.CurrentAttest.setEvent(&event)
			d.CurrentAttest.setOngoing()

			logger.Infow("Invoking attest", "block hash", event.BlockHash.String())

			resp, err := signerP.InvokeAttest(signer, &event)
			if err != nil {
				logger.Errorw(
					"Failed to attest", "block hash", event.BlockHash.String(), "error", err,
				)

				if strings.Contains(err.Error(), "Attestation is done for this epoch") {
					metricsServer.RecordAttestationConfirmed(ChainID)
					logger.Infow(
						"Attestation is done for this epoch",
						"block hash", event.BlockHash.String(),
					)
					d.CurrentAttest.setSuccessful()
					return
				}

				d.CurrentAttest.setFailed()
				d.CurrentAttest.resetTransactionHash()

				// Record attestation failure in metrics
				metricsServer.RecordAttestationFailure(ChainID)

				continue
			}

			// Record attestation submission in metrics
			metricsServer.RecordAttestationSubmitted(ChainID)

			logger.Debugw("Attest transaction sent", "hash", resp.TransactionHash)
			d.CurrentAttest.setTransactionHash(resp.TransactionHash)
		case <-d.EndOfWindow:
			logger.Info("End of window reached")

			if d.CurrentAttest.Status != Successful {
				setAttestStatusOnTracking(signer, logger, &d.CurrentAttest)
			}

			if d.CurrentAttest.Status == Successful {
				logger.Infow(
					"Successfully attested to target block",
					"target block hash", d.CurrentAttest.Event.BlockHash.String(),
				)

				// Record attestation confirmation in metrics
				metricsServer.RecordAttestationConfirmed(ChainID)
			} else {
				logger.Warnw(
					"Failed to attest to target block",
					"target block hash", d.CurrentAttest.Event.BlockHash.String(),
				)
			}
		}
	}
}

func setAttestStatusOnTracking[S signerP.Signer](
	signer S,
	logger *utils.ZapLogger,
	attestToTrack *AttestTracker,
) {
	status := TrackAttest(signer, logger, &attestToTrack.Event, &attestToTrack.TransactionHash)
	attestToTrack.Status = status
	switch status {
	case Ongoing:
		attestToTrack.setOngoing()
	case Successful:
		attestToTrack.setSuccessful()
	case Failed:
		attestToTrack.setFailed()
	default:
		panic(fmt.Sprintf("Invalid attest status: %d", status))
	}
}

func TrackAttest[S signerP.Signer](
	signer S,
	logger *utils.ZapLogger,
	event *AttestRequired,
	txHash *felt.Felt,
) AttestStatus {
	txStatus, err := signer.GetTransactionStatus(context.Background(), txHash)
	if err != nil {
		if err.Error() == ErrTxnHashNotFound.Error() {
			logger.Infow(
				"Transaction status was not found.",
				"hash", txHash,
			)
			return Ongoing
		} else {
			logger.Errorw(
				"Attest transaction failed",
				"target block hash", event.BlockHash.String(),
				"transaction hash", txHash,
				"error", err,
			)
			return Failed
		}
	}

	if txStatus.FinalityStatus == rpc.TxnStatus_Received {
		logger.Infow(
			"Transaction status is RECEIVED.",
			"hash", txHash,
		)
		return Ongoing
	}

	if txStatus.FinalityStatus == rpc.TxnStatus_Rejected {
		// TODO: are we guaranteed err is nil if tx got rejected ?
		logger.Errorw(
			"Attest transaction REJECTED",
			"target block hash", event.BlockHash.String(),
			"transaction hash", txHash,
		)
		return Failed
	}

	if txStatus.ExecutionStatus == rpc.TxnExecutionStatusREVERTED {
		logger.Errorw(
			"Attest transaction REVERTED",
			"target block hash", event.BlockHash.String(),
			"transaction hash", txHash,
			"failure reason", txStatus.FailureReason,
		)
		return Failed
	}

	logger.Infow(
		"Attest transaction successful",
		"block hash", event.BlockHash.String(),
		"transaction hash", txHash,
		"finality status", txStatus.FinalityStatus,
		"execution status", txStatus.ExecutionStatus,
	)
	return Successful
}
