package main

import (
	"context"
	"errors"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/sourcegraph/conc"
)

const defaultAttestDelay = 10

// Created a function variable for mocking purposes in tests
var Sleep = time.Sleep

type AttestStatus uint8

const (
	Ongoing AttestStatus = iota + 1
	Successful
	Failed
)

type EventDispatcher[Account Accounter] struct {
	// Could potentially add an event like EndOfEpoch to log in this file if the attestation was successful.
	// Could still log that when receiving next attest event without EndOfEpoch event,
	// but will force us to wait at least 11 blocks.
	AttestRequired chan AttestRequired
}

func NewEventDispatcher[Account Accounter]() EventDispatcher[Account] {
	return EventDispatcher[Account]{
		AttestRequired: make(chan AttestRequired),
	}
}

func (d *EventDispatcher[Account]) Dispatch(
	account Account,
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

			// TODO: when changing to a new attestation, could check the status and if it's not successful,
			// then log the block was not successfuly attested!
			*currentAttest = event
			*currentAttestStatus = Ongoing

			resp, err := InvokeAttest(account, &event)
			if err != nil {
				// throw a detailed error of what happened
				*currentAttestStatus = Failed
				continue
			}

			wg.Go(func() {
				txStatus := TrackAttest(account, resp)
				*currentAttestStatus = txStatus
			})
		}
	}
}

// do something with response
// Have it checked that is included in pending block
// Have it checked that it was included in the latest block (success, close the goroutine)
// ---
// If not included, then what was the reason? Try to include it again
// ---
// If the transaction was actually reverted log it and repeat the attestation
func TrackAttest[Account Accounter](
	account Account,
	txResp *rpc.AddInvokeTransactionResponse,
) AttestStatus {
	txStatus, err := TrackTransactionStatus(account, txResp.TransactionHash)

	if err != nil {
		// log exactly what's the error
		return Failed
	}

	if txStatus.FinalityStatus == rpc.TxnStatus_Rejected {
		// log exactly the rejection and why was it
		return Failed
	}

	if txStatus.ExecutionStatus == rpc.TxnExecutionStatusREVERTED {
		// log the failure & the reason, in here `txStatus.FailureReason` probably
		return Failed
	}

	// If we got here, then the transaction status was accepted & successful
	//
	// It might have been deleted from map if routine took some time and in the meantime
	// the next block got processed (and window for attestation passed). In that case,
	// we do not want to re-put it into the map
	//
	// log attestation was successful
	return Successful
}

// I guess we could sleep & wait a bit here because I believe canceling & then retrying from the next block
// will not allow us to get our tx included faster (increasing our chances to be within the attestation window)
// as this one is getting processed rn
// (except if we were to implement a mechanism to increase Tip fee in the future? as it is not released/active yet)
//
// That being said, a possibility could also be to return and signal the tx status as `Failed` in the `TransactionStatus` enum
// so that next block's event triggers a retry.
// - In the "worst" case scenario, our tx will be included twice: we'll get an error from the contract the 2nd time saying "already an attestation for that epoch"
// - In the "best" case scenario, our tx ends up not getting included the 1st time, so, we did well to let the next block trigger a retry.
func TrackTransactionStatus[Account Accounter](account Account, txHash *felt.Felt) (*rpc.TxnStatusResp, error) {
	for elapsedSeconds := 0; elapsedSeconds < defaultAttestDelay; elapsedSeconds++ {
		txStatus, err := account.GetTransactionStatus(context.Background(), txHash)
		if err != nil && err.Error() != "Transaction hash not found" {
			return nil, err
		}
		if err == nil && txStatus.FinalityStatus != rpc.TxnStatus_Received {
			return txStatus, nil
		}
		Sleep(time.Second)
	}

	// If we are here, it means the transaction didn't change it's status for a long time.
	// Should we track again?
	// Should we have a finite number of tries?
	//
	// I guess here we can return and retry from the next block (we might not even be in attestation window anymore)
	// And we already waited `defaultAttestDelay` seconds
	// wdyt ?
	return nil, errors.New("Tx status did not change for a long time, retrying with next block")
}
