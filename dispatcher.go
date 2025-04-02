package main

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/sourcegraph/conc"
)

// Created a function variable for mocking purposes in tests
var Sleep = time.Sleep

type AttestStatus uint8

const (
	Ongoing AttestStatus = iota + 1
	Successful
	Failed
)

type EventDispatcher[Account Accounter, Logger utils.Logger] struct {
	// Could potentially add an event like EndOfEpoch to log in this file whether the attestation was successful.
	// Could still log that when receiving next attest event without EndOfEpoch event,
	// but will force us to wait at least 11 blocks.
	AttestRequired chan AttestRequired
}

func NewEventDispatcher[Account Accounter, Logger utils.Logger]() EventDispatcher[Account, Logger] {
	return EventDispatcher[Account, Logger]{
		AttestRequired: make(chan AttestRequired),
	}
}

func (d *EventDispatcher[Account, Logger]) Dispatch(
	account Account,
	logger Logger,
	currentAttest *AttestRequired,
	currentAttestStatus *AttestStatus,
) {
	wg := conc.NewWaitGroup()
	defer wg.Wait()

	for {
		select {
		case event, ok := <-d.AttestRequired:
			if !ok {
				return
			}

			if event == *currentAttest && (*currentAttestStatus == Ongoing || *currentAttestStatus == Successful) {
				continue
			}

			// When changing to a new block to attest to, check the status of previous target block
			// and if it was not successful, log the block was not successfully attested
			initialAttest := AttestRequired{}
			if event != *currentAttest && *currentAttest != initialAttest && *currentAttestStatus == Failed {
				logger.Errorw("Failed to attest to previous target block", "previous target block hash", currentAttest.BlockHash)
			}

			*currentAttest = event
			*currentAttestStatus = Ongoing

			resp, err := InvokeAttest(account, &event)
			if err != nil {
				logger.Errorw("Failed to attest", "block hash", event.BlockHash, "error", err)
				*currentAttestStatus = Failed
				continue
			}

			wg.Go(func() {
				txStatus := TrackAttest(account, logger, &event, resp)
				*currentAttestStatus = txStatus
			})
		}
	}
}

func TrackAttest[Account Accounter, Logger utils.Logger](
	account Account,
	logger Logger,
	event *AttestRequired,
	txResp *rpc.AddInvokeTransactionResponse,
) AttestStatus {
	txStatus, err := TrackTransactionStatus(account, logger, txResp.TransactionHash)

	if err != nil {
		logger.Errorw("Attest transaction failed", "target block hash", event.BlockHash, "transaction hash", txResp.TransactionHash, "error", err)
		return Failed
	}

	if txStatus.FinalityStatus == rpc.TxnStatus_Rejected {
		// TODO: are we guaranteed err is nil if tx got rejected ?
		logger.Errorw("Attest transaction rejected", "target block hash", event.BlockHash, "transaction hash", txResp.TransactionHash)
		return Failed
	}

	if txStatus.ExecutionStatus == rpc.TxnExecutionStatusREVERTED {
		logger.Errorw("Attest transaction reverted", "target block hash", event.BlockHash, "transaction hash", txResp.TransactionHash, "failure reason", txStatus.FailureReason)
		return Failed
	}

	logger.Infow("Attest transaction successful", "target block hash", event.BlockHash, "transaction hash", txResp.TransactionHash)
	// Even if tx tracking takes time, we have at least MIN_ATTESTATION_WINDOW blocks before next attest
	// so, we can assume we're safe to update the status (for the expected target block, and not the next one)
	return Successful
}

func TrackTransactionStatus[Account Accounter, Logger utils.Logger](account Account, logger Logger, txHash *felt.Felt) (*rpc.TxnStatusResp, error) {
	for elapsedSeconds := 0; elapsedSeconds < DEFAULT_MAX_RETRIES; elapsedSeconds++ {
		txStatus, err := account.GetTransactionStatus(context.Background(), txHash)
		if err != nil && err.Error() != "Transaction hash not found" {
			return nil, err
		}
		if err == nil && txStatus.FinalityStatus != rpc.TxnStatus_Received {
			return txStatus, nil
		}

		if err != nil {
			// TODO: should it be here Errorw or Infow ?
			// Also, those 2 logs might be only for dev and not user ?
			logger.Errorw("Attest transaction status was not found: tracking was too fast for sequencer to be aware of tx, retrying...", "tx hash", txHash)
		} else {
			logger.Errorw("Attest transaction status was received: retrying tracking it...", "tx hash", txHash)
		}

		Sleep(time.Second)
	}

	// If we are here, it means the transaction didn't change it's status for `DEFAULT_MAX_RETRIES` seconds
	// Return and retry from the next block (if still in attestation window)
	return nil, errors.New("Tx status did not change for at least " + strconv.Itoa(DEFAULT_MAX_RETRIES) + " seconds, retrying from next block")
}
