package main

import (
	"fmt"

	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/sourcegraph/conc"
)

// Main execution loop of the program. Listens to the blockchain and sends
// attest invoke when it's the right time
func Attest(config *Config) {
	provider := NewProvider(config.providerUrl)
	validatorAccount := NewValidatorAccount(provider, &config.accountData)
	dispatcher := NewEventDispatcher[*ValidatorAccount]()

	wg := conc.NewWaitGroup()
	defer wg.Wait()
	wg.Go(func() {
		currentAttest := AttestRequired{}
		currentAttestStatus := Failed
		dispatcher.Dispatch(&validatorAccount, &currentAttest, &currentAttestStatus)
	})

	// Subscribe to the block headers
	wsProvider, headersFeed := BlockHeaderSubscription(config.providerUrl)
	defer wsProvider.Close()
	defer close(headersFeed)

	ProcessBlockHeaders(headersFeed, &validatorAccount, &dispatcher)
	// I'd also like to check the balance of the address from time to time to verify
	// that they have enough money for the next 10 attestations (value modifiable by user)
	// Once it goes below it, the console should start giving warnings
	// This the least prio but we should implement nonetheless

	// Should also track re-org and check if the re-org means we have to attest again or not
}

func ProcessBlockHeaders[Account Accounter](
	headersFeed chan *rpc.BlockHeader,
	account Account,
	dispatcher *EventDispatcher[Account],
) {
	epochInfo, attestInfo, err := FetchEpochAndAttestInfo(account)
	if err != nil {
		// If we fail at this point it means there is probably something wrong with the
		// configuration we might log the error and do a re-try just to make sure
	}

	for blockHeader := range headersFeed {
		fmt.Println("Block header:", blockHeader)

		// Re-fetch epoch info on new epoch (validity guaranteed for 1 epoch even if updates are made)
		if blockHeader.BlockNumber == epochInfo.CurrentEpochStartingBlock.Uint64()+epochInfo.EpochLen {
			// TODO: log new epoch start
			prevEpochInfo := epochInfo

			epochInfo, attestInfo, err = FetchEpochAndAttestInfo(account)
			if err != nil {
				// TODO: implement a retry mechanism ?
			}

			// Sanity check
			if epochInfo.EpochId != prevEpochInfo.EpochId+1 ||
				epochInfo.CurrentEpochStartingBlock.Uint64() != prevEpochInfo.CurrentEpochStartingBlock.Uint64()+prevEpochInfo.EpochLen {
				// TODO: give more details concerning the epoch info
				fmt.Printf("Wrong epoch change: from %d to %d", prevEpochInfo.EpochId, epochInfo.EpochId)
				// TODO: what should we do ?
			}
		}

		if BlockNumber(blockHeader.BlockNumber) == attestInfo.TargetBlock {
			attestInfo.TargetBlockHash = BlockHash(*blockHeader.BlockHash)
		}

		if BlockNumber(blockHeader.BlockNumber) >= attestInfo.WindowStart-1 &&
			BlockNumber(blockHeader.BlockNumber) < attestInfo.WindowEnd {
			dispatcher.AttestRequired <- AttestRequired{
				BlockHash: attestInfo.TargetBlockHash,
			}
		}
	}
}
