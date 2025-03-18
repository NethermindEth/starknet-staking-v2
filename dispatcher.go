package main

import (
	"context"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
)

const defaultAttestDelay = 10

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
	Successful AttestationStatus = iota + 1
	Ongoing
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

func (d *EventDispatcher) Dispatch(
	provider *rpc.Provider,
	account *account.Account,
	validator *Address,
	stakedAmount *Balance,
) {
	var currentEpoch uint64
	stakedAmountPerEpoch := NewStakedAmount()

	activeAttestations := make(map[BlockHash]AttestationStatus)

	for {
		select {
		case event, ok := <-d.AttestRequired:
			if !ok {
				break
			}

			switch status, exists := activeAttestations[*event.blockHash]; {
			case !exists, status == Failed:
				activeAttestations[*event.blockHash] = Ongoing
			case status == Ongoing, status == Successful:
				continue
			}

			stakedAmountPerEpoch.Get(currentEpoch)

			resp, err := invokeAttest(account, &event)
			if err != nil {
				// throw a detailed error of what happened

				go repeatAttest(d.AttestRequired, event, defaultAttestDelay, activeAttestations)
				continue
			}

			go trackAttest(provider, d.AttestRequired, event, resp, activeAttestations)
		case event, ok := <-d.AttestationsToRemove:
			if !ok {
				break
			}
			for _, blockHash := range event {
				delete(activeAttestations, blockHash)
			}
		case event, ok := <-d.StakeUpdated:
			if !ok {
				break
			}
			stakedAmountPerEpoch.Update(&event)
		}
	}
}

func invokeAttest(
	account *account.Account, attest *AttestRequired,
) (*rpc.TransactionResponse, error) {
	// Todo this might be worth doing async
	nonce, err := nonce(account)
	if err != nil {
		return nil, err
	}

	fnCall := rpc.FunctionCall{
		ContractAddress:    attestationContractAddress,
		EntryPointSelector: utils.GetSelectorFromNameFelt("attest"),
		Calldata:           []*felt.Felt{attest.blockHash},
	}

	invokeCalldata, err := account.FmtCalldata([]rpc.FunctionCall{fnCall})
	if err != nil {
		return nil, err
	}

	invoke := rpc.BroadcastInvokev3Txn{
		InvokeTxnV3: rpc.InvokeTxnV3{
			Type:          rpc.TransactionType_Invoke,
			SenderAddress: account.AccountAddress,
			Calldata:      invokeCalldata,
			Version:       rpc.TransactionV3,
			// Signature: , // Set during signing below
			Nonce: nonce,
			// ResourceBounds: ,
			// Tip: rpc.U64, // Investigate if this is applicable, perhaps it can be a way of
			//               // prioritizing this transaction if there is congestion
			// PayMasterData: , // I don't know if this is applicable, investigate. Maybe not in v1
			// AccountDeploymentData: , // It shouldn't be required
			// NonceDataMode: , // Investigate what goes here
			// FeeMode: , // Investigate
		},
	}

	if err = account.SignInvokeTransaction(context.Background(), &invoke.InvokeTxnV3); err != nil {
		return nil, err
	}

	// why not use dedicated fct `account.Provider().AddInvokeTransaction(context.Background(), invoke)` ?
	return account.SendTransaction(context.Background(), invoke)
}

// Repeat an Attest event after the `delay` in seconds
func repeatAttest(attestChan chan<- AttestRequired, event AttestRequired, delay uint64, activeAttestations map[BlockHash]AttestationStatus) {
	// Will retry sending attestation at the next block (if still within window) and/or after sleep
	//
	// Problem: If we sleep, we have no idea if the current block after waking up is still within attestation window
	// Solution: Could maybe remove this sleeping retry mechanism, and just wait for next event from main.go.
	//
	// And actually after additional thought, sleeping and waking up & sending an event after the max window might cause a bug
	// resetting the attestation as an active one.
	activeAttestations[*event.blockHash] = Failed

	time.Sleep(time.Second * time.Duration(delay))
	attestChan <- event
}

// do something with response
// Have it checked that is included in pending block
// Have it checked that it was included in the latest block (success, close the goroutine)
// ---
// If not included, then what was the reason? Try to include it again
// ---
// If the transaction was actually reverted log it and repeat the attestation
func trackAttest(
	provider *rpc.Provider,
	attestChan chan AttestRequired,
	event AttestRequired,
	txResp *rpc.TransactionResponse,
	activeAttestations map[BlockHash]AttestationStatus,
) {
	startTime := time.Now()
	txStatus, err := trackTransactionStatus(provider, txResp.TransactionHash)

	if err != nil {
		// log exactly what's the error

		elapsedTime := time.Now().Sub(startTime).Seconds()
		var attestDelay uint64
		if elapsedTime < defaultAttestDelay {
			attestDelay = defaultAttestDelay - uint64(elapsedTime)
		}
		repeatAttest(attestChan, event, attestDelay, activeAttestations)
		return
	}

	if txStatus.FinalityStatus == rpc.TxnStatus_Rejected {
		// log exactly the rejection and why was it

		repeatAttest(attestChan, event, defaultAttestDelay, activeAttestations)
		return
	}

	// if we got here, then the transaction status was accepted
	activeAttestations[*event.blockHash] = Successful
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
func trackTransactionStatus(provider *rpc.Provider, txHash *felt.Felt) (*rpc.TxnStatusResp, error) {
	for elapsedSeconds := 0; elapsedSeconds < defaultAttestDelay; elapsedSeconds++ {
		txStatus, err := provider.GetTransactionStatus(context.Background(), txHash)
		if err != nil {
			return nil, err
		}
		if txStatus.FinalityStatus != rpc.TxnStatus_Received {
			return txStatus, nil
		}
		time.Sleep(time.Second)
	}

	// If we are here, it means the transaction didn't change it's status for a long time.
	// Should we track again?
	// Should we have a finite number of tries?
	//
	// I guess here we can return and retry from the next block (we might not even be in attestation window anymore)
	// wdyt ?
	return trackTransactionStatus(provider, txHash)
}
