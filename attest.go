package main

import (
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

	provider := NewProvider(config.providerUrl, zapLogger)
	validatorAccount := NewValidatorAccount(provider, zapLogger, &config.accountData)
	dispatcher := NewEventDispatcher[*ValidatorAccount, *utils.ZapLogger]()

	wg := conc.NewWaitGroup()
	defer wg.Wait()
	wg.Go(func() {
		currentAttest := AttestRequired{}
		currentAttestStatus := Failed
		dispatcher.Dispatch(&validatorAccount, zapLogger, &currentAttest, &currentAttestStatus)
	})

	// Subscribe to the block headers
	wsProvider, headersFeed := BlockHeaderSubscription(config.providerUrl, zapLogger)
	defer wsProvider.Close()
	defer close(headersFeed)

	ProcessBlockHeaders(headersFeed, &validatorAccount, zapLogger, &dispatcher)
	// I'd also like to check the balance of the address from time to time to verify
	// that they have enough money for the next 10 attestations (value modifiable by user)
	// Once it goes below it, the console should start giving warnings
	// This the least prio but we should implement nonetheless

	// Should also track re-org and check if the re-org means we have to attest again or not
}

func ProcessBlockHeaders[Account Accounter, Logger utils.Logger](
	headersFeed chan *rpc.BlockHeader,
	account Account,
	logger Logger,
	dispatcher *EventDispatcher[Account, Logger],
) {
	epochInfo, attestInfo := FetchEpochAndAttestInfoWithRetry(account, logger, "Failed to fetch epoch info at startup")

	for blockHeader := range headersFeed {
		logger.Infow("Block header received", "blockHeader", blockHeader)

		// Re-fetch epoch info on new epoch (validity guaranteed for 1 epoch even if updates are made)
		if blockHeader.BlockNumber == epochInfo.CurrentEpochStartingBlock.Uint64()+epochInfo.EpochLen {
			logger.Infof("New epoch start %d", epochInfo.EpochId+1)
			prevEpochInfo := epochInfo

			epochInfo, attestInfo = FetchEpochAndAttestInfoWithRetry(account, logger, "Failed to fetch epoch info", "epoch id", prevEpochInfo.EpochId+1)

			epochInfo, attestInfo = EpochSwitchSanityCheckWithRetry(account, logger, prevEpochInfo, epochInfo, attestInfo)
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

func FetchEpochAndAttestInfoWithRetry[Account Accounter, Logger utils.Logger](
	account Account,
	logger Logger,
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
func EpochSwitchSanityCheckWithRetry[Account Accounter, Logger utils.Logger](
	account Account,
	logger Logger,
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
