package validator

import (
	"context"
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

type AttestTracking struct {
	Event           AttestRequired
	TransactionHash felt.Felt
	Status          AttestStatus
}

type EventDispatcher[Account Accounter, Log Logger] struct {
	// Current epoch attest related fields
	CurrentAttest AttestTracking
	// Event channels
	AttestRequired chan AttestRequired
	EndOfWindow    chan struct{}
}

func NewEventDispatcher[Account Accounter, Log Logger]() EventDispatcher[Account, Log] {
	return EventDispatcher[Account, Log]{
		CurrentAttest: AttestTracking{
			Event:           AttestRequired{},
			TransactionHash: felt.Zero,
			Status:          Failed,
		},
		AttestRequired: make(chan AttestRequired),
		EndOfWindow:    make(chan struct{}),
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

			if event == d.CurrentAttest.Event && d.CurrentAttest.Status != Successful && d.CurrentAttest.TransactionHash != felt.Zero {
				d.CurrentAttest.Status = TrackAttest(account, logger, &d.CurrentAttest.Event, &d.CurrentAttest.TransactionHash)
			}

			if event == d.CurrentAttest.Event &&
				(d.CurrentAttest.Status == Ongoing || d.CurrentAttest.Status == Successful) {
				continue
			}

			d.CurrentAttest.Event = event
			d.CurrentAttest.Status = Ongoing

			logger.Infow("Invoking attest", "block hash", event.BlockHash.String())
			resp, err := InvokeAttest(account, &event)
			if err != nil {
				logger.Errorw(
					"Failed to attest", "block hash", event.BlockHash.String(), "error", err,
				)
				d.CurrentAttest.Status = Failed
				d.CurrentAttest.TransactionHash = felt.Zero
				continue
			}
			logger.Debugw("Attest transaction sent", "hash", resp.TransactionHash)
			d.CurrentAttest.TransactionHash = *resp.TransactionHash
		case <-d.EndOfWindow:
			logger.Infow("End of window reached")

			if d.CurrentAttest.Status != Successful {
				d.CurrentAttest.Status = TrackAttest(account, logger, &d.CurrentAttest.Event, &d.CurrentAttest.TransactionHash)
			}

			if d.CurrentAttest.Status == Successful {
				logger.Infow(
					"Successfully attested to target block",
					"target block hash", d.CurrentAttest.Event.BlockHash.String(),
				)
			} else {
				logger.Infow(
					"Failed to attest to target block",
					"target block hash", d.CurrentAttest.Event.BlockHash.String(),
				)
			}
		}
	}
}

func TrackAttest[Account Accounter, Log Logger](
	account Account,
	logger Log,
	event *AttestRequired,
	txHash *felt.Felt,
) AttestStatus {
	txStatus, err := account.GetTransactionStatus(context.Background(), txHash)

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
