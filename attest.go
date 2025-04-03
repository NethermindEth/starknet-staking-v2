package main

import (
	"context"
	"log"
	"time"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/sourcegraph/conc"
)

// Main execution loop of the program. Listens to the blockchain and sends
// attest invoke when it's the right time
func Attest(config *Config) {
	zapLogger, loggerErr := utils.NewZapLogger(utils.INFO, false)
	if loggerErr != nil {
		log.Fatalf("Error creating logger: %s", loggerErr)
	}

	provider := NewProvider(config.httpProviderUrl, zapLogger)
	validatorAccount := NewValidatorAccount(provider, zapLogger, &config.accountData)
	dispatcher := NewEventDispatcher[*ValidatorAccount, *utils.ZapLogger]()

	wg := conc.NewWaitGroup()
	defer wg.Wait()
	wg.Go(func() { dispatcher.Dispatch(&validatorAccount, zapLogger) })

	// Subscribe to the block headers
	wsProvider, headersFeed := BlockHeaderSubscription(config.wsProviderUrl, zapLogger)
	defer wsProvider.Close()
	defer close(headersFeed)

	ProcessBlockHeaders(headersFeed, &validatorAccount, zapLogger, &dispatcher)
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
	epochInfo, attestInfo := FetchEpochAndAttestInfoWithRetry(account, logger, "Failed to fetch epoch info at startup")

	SetTargetBlockHashIfExists(account, logger, &attestInfo)

	for blockHeader := range headersFeed {
		logger.Infow("Block header received", "blockHeader", blockHeader)

		// Re-fetch epoch info on new epoch (validity guaranteed for 1 epoch even if updates are made)
		if blockHeader.BlockNumber == epochInfo.CurrentEpochStartingBlock.Uint64()+epochInfo.EpochLen {
			logger.Infow("New epoch start", "epoch id", epochInfo.EpochId+1)
			prevEpochInfo := epochInfo

			epochInfo, attestInfo = FetchEpochAndAttestInfoWithRetry(account, logger, "Failed to fetch epoch info", "epoch id", prevEpochInfo.EpochId+1)

			epochInfo, attestInfo = EpochSwitchSanityCheckWithRetry(account, logger, prevEpochInfo, epochInfo, attestInfo)
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
		switch res.(type) {
		case *rpc.BlockTxHashes:
			block, ok := res.(*rpc.BlockTxHashes)
			if ok {
				attestInfo.TargetBlockHash = BlockHash(*block.BlockHash)
				logger.Infow(
					"Target block already exists, registered block hash to attest to it if still within attestation window",
					"block hash", attestInfo.TargetBlockHash.String(),
				)
			}
			// If case *rpc.PendingBlockTxHashes, then we'll just receive the block in the listening for loop
		}
	}
}

func FetchEpochAndAttestInfoWithRetry[Account Accounter, Log Logger](
	account Account,
	logger Log,
	logMsg string,
	keysAndValues ...any,
) (EpochInfo, AttestInfo) {
	epochInfo, attestInfo, err := FetchEpochAndAttestInfo(account, logger)

	for i := 0; err != nil && i < DEFAULT_MAX_RETRIES; i++ {
		logger.Errorw(logMsg, keysAndValues, "error", err)
		Sleep(time.Second)
		epochInfo, attestInfo, err = FetchEpochAndAttestInfo(account, logger)
	}

	// If fetched info are still incorrect, exit program
	if err != nil {
		logger.Fatalf(logMsg, keysAndValues, "error", err)
	}

	return epochInfo, attestInfo
}

// Should never fail. If it does, it means the contract logic has a bug
// TODO: should we even retry... ? not exit program directly ?
func EpochSwitchSanityCheckWithRetry[Account Accounter, Log Logger](
	account Account,
	logger Log,
	previousEpochInfo EpochInfo,
	newEpochInfo EpochInfo,
	newAttestInfo AttestInfo,
) (EpochInfo, AttestInfo) {
	wrongEpochSwitch := newEpochInfo.EpochId != previousEpochInfo.EpochId+1 ||
		newEpochInfo.CurrentEpochStartingBlock.Uint64() != previousEpochInfo.CurrentEpochStartingBlock.Uint64()+previousEpochInfo.EpochLen

	for i := 0; wrongEpochSwitch && i < DEFAULT_MAX_RETRIES; i++ {
		logger.Errorw("Wrong epoch change", "from epoch", previousEpochInfo, "to epoch", newEpochInfo)
		Sleep(time.Second)

		// TODO: what should we do with the 3rd return value (error) ? Call FetchEpochAndAttestInfoWithRetry
		newEpochInfo, newAttestInfo, _ = FetchEpochAndAttestInfo(account, logger)

		wrongEpochSwitch = newEpochInfo.EpochId != previousEpochInfo.EpochId+1 ||
			newEpochInfo.CurrentEpochStartingBlock.Uint64() != previousEpochInfo.CurrentEpochStartingBlock.Uint64()+previousEpochInfo.EpochLen
	}

	// If epoch switch is still incorrect, exit program
	if wrongEpochSwitch {
		logger.Fatalf("Wrong epoch switch", "from epoch", previousEpochInfo, "to epoch", newEpochInfo)
	}

	return newEpochInfo, newAttestInfo
}
