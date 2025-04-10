package main

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/NethermindEth/juno/core/felt"
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

type EventDispatcher[Account Accounter, Log Logger] struct {
	// Current epoch attest related fields
	CurrentAttest       AttestRequired
	CurrentAttestStatus AttestStatus

	// Event channels
	AttestRequired chan AttestRequired
	EndOfWindow    chan struct{}
}

func NewEventDispatcher[Account Accounter, Log Logger]() EventDispatcher[Account, Log] {
	return EventDispatcher[Account, Log]{
		CurrentAttest:       AttestRequired{},
		CurrentAttestStatus: Failed,
		AttestRequired:      make(chan AttestRequired),
		EndOfWindow:         make(chan struct{}),
	}
}

func (d *EventDispatcher[Account, Log]) Dispatch(
	account Account,
	logger Log,
) {
	wg := conc.NewWaitGroup()
	defer wg.Wait()

	for {
		select {
		case event, ok := <-d.AttestRequired:
			if !ok {
				return
			}

			if event == d.CurrentAttest && (d.CurrentAttestStatus == Ongoing || d.CurrentAttestStatus == Successful) {
				continue
			}

			d.CurrentAttest = event
			d.CurrentAttestStatus = Ongoing

			logger.Infow("Attestation sent", "block hash", event.BlockHash.String())
			resp, err := InvokeAttest(account, &event)
			if err != nil {
				logger.Errorw("Failed to attest", "block hash", event.BlockHash.String(), "error", err)
				d.CurrentAttestStatus = Failed
				continue
			}

			wg.Go(func() {
				txStatus := TrackAttest(account, logger, &event, resp)
				// Even if tx tracking takes time, we have at least MIN_ATTESTATION_WINDOW blocks before next attest
				// so, we can assume we're safe to update the status (for the expected target block, and not the next one)
				d.CurrentAttestStatus = txStatus
			})
		case <-d.EndOfWindow:
			logger.Infow("End of window reached")
			if d.CurrentAttestStatus == Successful {
				logger.Infow("Successfully attested to target block", "target block hash", d.CurrentAttest.BlockHash.String())
			} else {
				logger.Infow("Failed to attest to target block", "target block hash", d.CurrentAttest.BlockHash.String())
			}
		}
	}
}

func TrackAttest[Account Accounter, Log Logger](
	account Account,
	logger Log,
	event *AttestRequired,
	txResp *rpc.AddInvokeTransactionResponse,
) AttestStatus {
	txStatus, err := TrackTransactionStatus(account, logger, txResp.TransactionHash)

	if err != nil {
		logger.Errorw(
			"Attest transaction failed",
			"target block hash", event.BlockHash.String(),
			"transaction hash", txResp.TransactionHash,
			"error", err,
		)
		return Failed
	}

	if txStatus.FinalityStatus == rpc.TxnStatus_Rejected {
		// TODO: are we guaranteed err is nil if tx got rejected ?
		logger.Errorw(
			"Attest transaction REJECTED",
			"target block hash", event.BlockHash.String(),
			"transaction hash", txResp.TransactionHash,
		)
		return Failed
	}

	if txStatus.ExecutionStatus == rpc.TxnExecutionStatusREVERTED {
		logger.Errorw(
			"Attest transaction REVERTED",
			"target block hash", event.BlockHash.String(),
			"transaction hash", txResp.TransactionHash,
			"failure reason", txStatus.FailureReason,
		)
		return Failed
	}

	logger.Infow(
		"Attest transaction successful",
		"block hash", event.BlockHash.String(),
		"transaction hash", txResp.TransactionHash,
		"finality status", txStatus.FinalityStatus,
		"execution status", txStatus.ExecutionStatus,
	)
	return Successful
}

func TrackTransactionStatus[Account Accounter, Log Logger](account Account, logger Log, txHash *felt.Felt) (*rpc.TxnStatusResp, error) {
	for elapsedSeconds := 0; elapsedSeconds < DEFAULT_MAX_RETRIES; elapsedSeconds++ {
		txStatus, err := account.GetTransactionStatus(context.Background(), txHash)

		if err != nil && err.Error() != ErrTxnHashNotFound.Error() {
			return nil, err
		}
		if err == nil && txStatus.FinalityStatus != rpc.TxnStatus_Received {
			return txStatus, nil
		}

		if err != nil {
			logger.Infow(
				"Attest transaction status was not found: tracking was too fast for sequencer to be aware of transaction, retrying...",
				"transaction hash", txHash,
			)
		} else {
			logger.Infow("Attest transaction status was RECEIVED: retrying tracking it...", "transaction hash", txHash)
		}

		Sleep(time.Second)
	}

	// If we are here, it means the transaction didn't change it's status for `DEFAULT_MAX_RETRIES` seconds
	// Return and retry from the next block (if still in attestation window)
	return nil, errors.New("Tx status did not change for at least " + strconv.Itoa(DEFAULT_MAX_RETRIES) + " seconds, retrying from next block")
}
