package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/rpc"
)

const defaultAttestDelay = 10

// Created a function variable for mocking purposes in tests
var SleepFn = time.Sleep

// Keep track of the stake amount through the different epochs
// Because there is a latency the most current stake doesn't need to match
// to the current epoch attestation
type stakedAmount struct{}

func NewStakedAmount() stakedAmount {
	return stakedAmount{}
}

func (s *stakedAmount) Update(newStake *StakeUpdated) {

}

func (s *stakedAmount) Get(epoch uint64) {

}

type AttestationStatus uint8

const (
	Ongoing AttestationStatus = iota + 1
	Successful
	Failed
)

// requires filling with the right values
type StakeUpdated struct{}

type EventDispatcher struct {
	StakeUpdated         chan StakeUpdated
	AttestRequired       chan AttestRequired
	AttestationsToRemove chan []BlockHash
}

func NewEventDispatcher() EventDispatcher {
	return EventDispatcher{
		AttestRequired:       make(chan AttestRequired),
		StakeUpdated:         make(chan StakeUpdated),
		AttestationsToRemove: make(chan []BlockHash),
	}
}

func (d *EventDispatcher) Dispatch(account Accounter, activeAttestations map[BlockHash]AttestationStatus, wg *sync.WaitGroup) {
	var currentEpoch uint64
	stakedAmountPerEpoch := NewStakedAmount()

for_loop:
	for {
		select {
		case event, ok := <-d.AttestRequired:
			if !ok {
				// Should never get closed
				break for_loop
			}

			switch status, exists := activeAttestations[event.BlockHash]; {
			case !exists, status == Failed:
				activeAttestations[event.BlockHash] = Ongoing
			case status == Ongoing, status == Successful:
				continue
			}

			stakedAmountPerEpoch.Get(currentEpoch)

			resp, err := invokeAttest(account, &event)
			if err != nil {
				// throw a detailed error of what happened
				continue
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				TrackAttest(account, event, resp, activeAttestations)
			}()
		case event, ok := <-d.AttestationsToRemove:
			if !ok {
				// Should never get closed
				break for_loop
			}
			// TODO: when deleting, could check the status and if it's not successful,
			// then log the block was not successfuly attested!
			for _, blockHash := range event {
				delete(activeAttestations, blockHash)
			}
			// Might delete this case later if we really don't need it
		case event, ok := <-d.StakeUpdated:
			if !ok {
				break
			}
			stakedAmountPerEpoch.Update(&event)
		}
	}

	wg.Done()
	// If we ever break from the loop, wait for subprocesses to finish to avoid any undefined behaviour
	// where routines access data that got deallocated after this routine returns
	wg.Wait()
}

// do something with response
// Have it checked that is included in pending block
// Have it checked that it was included in the latest block (success, close the goroutine)
// ---
// If not included, then what was the reason? Try to include it again
// ---
// If the transaction was actually reverted log it and repeat the attestation
func TrackAttest(
	account Accounter,
	event AttestRequired,
	txResp *rpc.AddInvokeTransactionResponse,
	activeAttestations map[BlockHash]AttestationStatus,
) {
	txStatus, err := TrackTransactionStatus(account, txResp.TransactionHash)

	if err != nil {
		// log exactly what's the error
		setStatusIfExists(activeAttestations, &event.BlockHash, Failed)
		return
	}

	if txStatus.FinalityStatus == rpc.TxnStatus_Rejected {
		// log exactly the rejection and why was it
		setStatusIfExists(activeAttestations, &event.BlockHash, Failed)
		return
	}

	if txStatus.ExecutionStatus == rpc.TxnExecutionStatusREVERTED {
		// log the failure & the reason, in here `txStatus.FailureReason` probably
		setStatusIfExists(activeAttestations, &event.BlockHash, Failed)
		return
	}

	// If we got here, then the transaction status was accepted & successful
	//
	// It might have been deleted from map if routine took some time and in the meantime
	// the next block got processed (and window for attestation passed). In that case,
	// we do not want to re-put it into the map
	setStatusIfExists(activeAttestations, &event.BlockHash, Successful)
	// log attestation was successful
}

func setStatusIfExists(activeAttestations map[BlockHash]AttestationStatus, blockHash *BlockHash, status AttestationStatus) {
	if _, exists := activeAttestations[*blockHash]; exists {
		activeAttestations[*blockHash] = status
	}
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
func TrackTransactionStatus(account Accounter, txHash *felt.Felt) (*rpc.TxnStatusResp, error) {
	for elapsedSeconds := 0; elapsedSeconds < defaultAttestDelay; elapsedSeconds++ {
		txStatus, err := account.GetTransactionStatus(context.Background(), txHash)
		if err != nil {
			return nil, err
		}
		if txStatus.FinalityStatus != rpc.TxnStatus_Received {
			return txStatus, nil
		}
		SleepFn(time.Second)
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
