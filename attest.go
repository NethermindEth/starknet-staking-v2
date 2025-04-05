package main

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/sourcegraph/conc"
)

// Main execution loop of the program. Listens to the blockchain and sends
// attest invoke when it's the right time
func Attest(config *Config) {
	logger, err := utils.NewZapLogger(utils.INFO, false)
	if err != nil {
		log.Fatalf("Error creating logger: %s", err)
	}

	provider := NewProvider(config.httpProviderUrl, logger)
	validatorAccount := NewValidatorAccount(provider, logger, &config.accountData)
	dispatcher := NewEventDispatcher[*ValidatorAccount, *utils.ZapLogger]()

	wg := conc.NewWaitGroup()
	defer wg.Wait()
	wg.Go(func() { dispatcher.Dispatch(&validatorAccount, logger) })

	// Subscribe to the block headers
	wsProvider, headersFeed := BlockHeaderSubscription(config.wsProviderUrl, logger)
	defer wsProvider.Close()
	defer close(headersFeed)

	ProcessBlockHeaders(headersFeed, &validatorAccount, logger, &dispatcher)
	// I'd also like to check the balance of the address from time to time to verify
	// that they have enough money for the next 10 attestations (value modifiable by user)
	// Once it goes below it, the console should start giving warnings
	// This the least prio but we should implement nonetheless

	// Should also track re-org and check if the re-org means we have to attest again or not
}

func ProcessBlockHeaders[Account Accounter, Log Logger](
	headersFeed chan *rpc.BlockHeader,
	account Account,
	logger Log,
	dispatcher *EventDispatcher[Account, Log],
) {
	noEpochSwitch := func(*EpochInfo, *EpochInfo) bool { return true }
	epochInfo, attestInfo := FetchEpochAndAttestInfoWithRetry(account, logger, nil, noEpochSwitch, "at app startup")

	SetTargetBlockHashIfExists(account, logger, &attestInfo)

	for blockHeader := range headersFeed {
		logger.Infow("Block header received", "blockHeader", blockHeader)

		// Re-fetch epoch info on new epoch (validity guaranteed for 1 epoch even if updates are made)
		if blockHeader.BlockNumber == epochInfo.CurrentEpochStartingBlock.Uint64()+epochInfo.EpochLen {
			logger.Infow("New epoch start", "epoch id", epochInfo.EpochId+1)
			prevEpochInfo := epochInfo

			epochInfo, attestInfo = FetchEpochAndAttestInfoWithRetry(
				account, logger, &prevEpochInfo, isEpochSwitchCorrect, strconv.FormatUint(prevEpochInfo.EpochId+1, 10),
			)
		}

		if BlockNumber(blockHeader.BlockNumber) == attestInfo.TargetBlock {
			logger.Infow("Target block reached", "block number", blockHeader.BlockNumber, "block hash", blockHeader.BlockHash)
			attestInfo.TargetBlockHash = BlockHash(*blockHeader.BlockHash)
			logger.Infof("Will attest to target block in window [%d, %d]", attestInfo.WindowStart, attestInfo.WindowEnd)
		}

		if BlockNumber(blockHeader.BlockNumber) >= attestInfo.WindowStart-1 &&
			BlockNumber(blockHeader.BlockNumber) < attestInfo.WindowEnd {
			dispatcher.AttestRequired <- AttestRequired{
				BlockHash: attestInfo.TargetBlockHash,
			}
		}

		if BlockNumber(blockHeader.BlockNumber) == attestInfo.WindowEnd {
			dispatcher.EndOfWindow <- struct{}{}
		}
	}
}

func SetTargetBlockHashIfExists[Account Accounter, Log Logger](
	account Account,
	logger Log,
	attestInfo *AttestInfo,
) {
	targetBlockNumber := attestInfo.TargetBlock.Uint64()
	res, err := account.BlockWithTxHashes(context.Background(), rpc.BlockID{Number: &targetBlockNumber})

	// If no error, then target block already exists
	if err == nil {
		if block, ok := res.(*rpc.BlockTxHashes); ok {
			attestInfo.TargetBlockHash = BlockHash(*block.BlockHash)
			logger.Infow(
				"Target block already exists, registered block hash to attest to it if still within attestation window",
				"block hash", attestInfo.TargetBlockHash.String(),
			)
		}
		// If case *rpc.PendingBlockTxHashes, then we'll just receive the block in the listening for loop
	}
}

func FetchEpochAndAttestInfoWithRetry[Account Accounter, Log Logger](
	account Account,
	logger Log,
	prevEpoch *EpochInfo,
	isEpochSwitchCorrect func(prevEpoch *EpochInfo, newEpoch *EpochInfo) bool,
	newEpochId string,
) (EpochInfo, AttestInfo) {
	newEpoch, newAttestInfo, err := FetchEpochAndAttestInfo(account, logger)

	for i := 0; (err != nil || !isEpochSwitchCorrect(prevEpoch, &newEpoch)) && i < DEFAULT_MAX_RETRIES; i++ {
		if err != nil {
			logger.Errorw("Failed to fetch epoch info", "epoch id", newEpochId, "error", err)
		} else {
			logger.Errorw("Wrong epoch switch", "from epoch", prevEpoch, "to epoch", newEpoch)
		}
		logger.Infow("Retrying to fetch epoch info...", "attempt", i+1)
		Sleep(time.Second)
		newEpoch, newAttestInfo, err = FetchEpochAndAttestInfo(account, logger)
	}

	// If still an issue after all retries, exit program
	if err != nil {
		logger.Fatalf("Failed to fetch epoch info", "epoch id", newEpochId, "error", err)
	} else if !isEpochSwitchCorrect(prevEpoch, &newEpoch) {
		logger.Fatalf("Wrong epoch switch", "from epoch", prevEpoch, "to epoch", newEpoch)
	}

	return newEpoch, newAttestInfo
}

func isEpochSwitchCorrect(prevEpoch *EpochInfo, newEpoch *EpochInfo) bool {
	return newEpoch.EpochId == prevEpoch.EpochId+1 &&
		newEpoch.CurrentEpochStartingBlock.Uint64() == prevEpoch.CurrentEpochStartingBlock.Uint64()+prevEpoch.EpochLen
}
