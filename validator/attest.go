package validator

import (
	"context"
	"strconv"
	"time"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
	"github.com/sourcegraph/conc"
)

const Version = "0.2.0"

// Main execution loop of the program. Listens to the blockchain and sends
// attest invoke when it's the right time
func Attest(
	ctx context.Context,
	config *config.Config,
	snConfig *config.StarknetConfig,
	maxRetries types.Retries,
	logger utils.ZapLogger,
	metricsServer *metrics.Metrics,
) error {
	provider, err := NewProvider(config.Provider.Http, &logger)
	if err != nil {
		return err
	}

	// Ignoring from now, until Starknet.go allow us to have a fixed Starknet option
	_, _ = types.AttestFeeFromString(snConfig.AttestOptions)
	// if err != nil {
	// 	// do nothing for now
	// 	// return err
	// }

	var signer signerP.Signer
	if config.Signer.External() {
		externalSigner, err := signerP.NewExternalSigner(
			provider, &logger, &config.Signer, &snConfig.ContractAddresses,
		)
		if err != nil {
			return err
		}
		signer = &externalSigner
	} else {
		internalSigner, err := signerP.NewInternalSigner(
			provider, &logger, &config.Signer, &snConfig.ContractAddresses,
		)
		if err != nil {
			return err
		}
		signer = &internalSigner
	}

	dispatcher := NewEventDispatcher[signerP.Signer]()
	wg := conc.NewWaitGroup()
	wg.Go(func() { dispatcher.Dispatch(signer, &logger, metricsServer) })
	defer wg.Wait()
	defer close(dispatcher.AttestRequired)

	return RunBlockHeaderWatcher(ctx, config, &logger, signer, &dispatcher, maxRetries, wg, metricsServer)
}

func RunBlockHeaderWatcher[Account signerP.Signer](
	ctx context.Context,
	config *config.Config,
	logger *utils.ZapLogger,
	signer Account,
	dispatcher *EventDispatcher[Account],
	maxRetries types.Retries,
	wg *conc.WaitGroup,
	metricsServer *metrics.Metrics,
) error {
	cleanUp := func(wsProvider *rpc.WsProvider, headersFeed chan *rpc.BlockHeader) {
		wsProvider.Close()
		close(headersFeed)
	}

	for {
		wsProvider, headersFeed, clientSubscription, err := SubscribeToBlockHeaders(
			config.Provider.Ws, logger,
		)
		if err != nil {
			return err
		}

		stopProcessingHeaders := make(chan error)

		wg.Go(func() {
			err := ProcessBlockHeaders(headersFeed, signer, logger, dispatcher, maxRetries, metricsServer)
			if err != nil {
				stopProcessingHeaders <- err
			}
		})

		select {
		case err := <-clientSubscription.Err():
			logger.Errorw("Block header subscription", "error", err)
			logger.Debugw("Ending headers subscription, closing websocket connection, and retrying...")
			cleanUp(wsProvider, headersFeed)
		case err := <-stopProcessingHeaders:
			cleanUp(wsProvider, headersFeed)
			return err
		}
	}
}

func ProcessBlockHeaders[Account signerP.Signer](
	headersFeed chan *rpc.BlockHeader,
	account Account,
	logger *utils.ZapLogger,
	dispatcher *EventDispatcher[Account],
	maxRetries types.Retries,
	metricsServer *metrics.Metrics,
) error {
	noEpochSwitch := func(*EpochInfo, *EpochInfo) bool { return true }
	epochInfo, attestInfo, err := FetchEpochAndAttestInfoWithRetry(
		account, logger, nil, noEpochSwitch, maxRetries, "at app startup",
	)
	if err != nil {
		return err
	}

	// Update initial epoch info metrics
	metricsServer.UpdateEpochInfo(ChainID, &epochInfo, attestInfo.TargetBlock.Uint64())

	SetTargetBlockHashIfExists(account, logger, &attestInfo)

	for blockHeader := range headersFeed {
		logger.Infof("Block %d received", blockHeader.Number)
		logger.Debugw("Block header information", "block header", blockHeader)

		// Update latest block number metric
		metricsServer.UpdateLatestBlockNumber(ChainID, blockHeader.Number)

		if blockHeader.Number == epochInfo.CurrentEpochStartingBlock.Uint64()+epochInfo.EpochLen {
			logger.Infow("New epoch start", "epoch id", epochInfo.EpochId+1)
			prevEpochInfo := epochInfo
			epochInfo, attestInfo, err = FetchEpochAndAttestInfoWithRetry(
				account,
				logger,
				&prevEpochInfo,
				CorrectEpochSwitch,
				maxRetries,
				strconv.FormatUint(prevEpochInfo.EpochId+1, 10),
			)
			if err != nil {
				return err
			}

			// Update epoch info metrics
			metricsServer.UpdateEpochInfo(ChainID, &epochInfo, attestInfo.TargetBlock.Uint64())
		}

		if BlockNumber(blockHeader.Number) == attestInfo.TargetBlock {
			attestInfo.TargetBlockHash = BlockHash(*blockHeader.Hash)
			logger.Infow(
				"Target block reached",
				"block number", blockHeader.Number,
				"block hash", blockHeader.Hash,
			)
			logger.Infow("Window to attest to",
				"start", attestInfo.WindowStart,
				"end", attestInfo.WindowEnd,
			)
		}

		if BlockNumber(blockHeader.Number) >= attestInfo.WindowStart-1 &&
			BlockNumber(blockHeader.Number) < attestInfo.WindowEnd {
			dispatcher.AttestRequired <- AttestRequired{
				BlockHash: attestInfo.TargetBlockHash,
			}
		}

		if BlockNumber(blockHeader.Number) == attestInfo.WindowEnd {
			dispatcher.EndOfWindow <- struct{}{}
		}
	}

	return nil
}

func SetTargetBlockHashIfExists[Account signerP.Signer](
	account Account,
	logger *utils.ZapLogger,
	attestInfo *AttestInfo,
) {
	targetBlockNumber := attestInfo.TargetBlock.Uint64()
	res, err := account.BlockWithTxHashes(
		context.Background(), rpc.BlockID{Number: &targetBlockNumber},
	)

	// If no error, then target block already exists
	if err == nil {
		if block, ok := res.(*rpc.BlockTxHashes); ok {
			attestInfo.TargetBlockHash = BlockHash(*block.Hash)
			logger.Infow(
				"Target block already exists. Registering block hash.",
				"target block", attestInfo.TargetBlock.Uint64(),
				"block hash", attestInfo.TargetBlockHash.String(),
				"window start", attestInfo.WindowStart.Uint64(),
				"window end", attestInfo.WindowEnd.Uint64(),
			)
		}
	}
}

func FetchEpochAndAttestInfoWithRetry[Account signerP.Signer](
	account Account,
	logger *utils.ZapLogger,
	prevEpoch *EpochInfo,
	isEpochSwitchCorrect func(prevEpoch *EpochInfo, newEpoch *EpochInfo) bool,
	maxRetries types.Retries,
	newEpochId string,
) (EpochInfo, AttestInfo, error) {
	// storing the initial value for error reporting
	totalRetryAmount := maxRetries.String()

	newEpoch, newAttestInfo, err := signerP.FetchEpochAndAttestInfo(account, logger)

	for (err != nil || !isEpochSwitchCorrect(prevEpoch, &newEpoch)) && !maxRetries.IsZero() {
		if err != nil {
			logger.Debugw("Failed to fetch epoch info", "epoch id", newEpochId, "error", err.Error())
		} else {
			logger.Debugw("Wrong epoch switch", "from epoch", prevEpoch, "to epoch", &newEpoch)
		}
		logger.Debugf("Retrying to fetch epoch info: %s retries remaining", &maxRetries)

		Sleep(time.Second)

		newEpoch, newAttestInfo, err = signerP.FetchEpochAndAttestInfo(account, logger)
		maxRetries.Sub()
	}

	if err != nil {
		return EpochInfo{},
			AttestInfo{},
			errors.Errorf(
				"Failed to fetch epoch info after %s retries. Epoch id: %s. Error: %s",
				totalRetryAmount,
				newEpochId,
				err.Error(),
			)
	}
	if !isEpochSwitchCorrect(prevEpoch, &newEpoch) {
		return EpochInfo{},
			AttestInfo{},
			errors.Errorf("Wrong epoch switch after %s retries from epoch:\n%s\nTo epoch:\n%s",
				totalRetryAmount,
				prevEpoch.String(),
				newEpoch.String(),
			)
	}

	return newEpoch, newAttestInfo, nil
}

func CorrectEpochSwitch(prevEpoch *EpochInfo, newEpoch *EpochInfo) bool {
	return newEpoch.EpochId == prevEpoch.EpochId+1 &&
		newEpoch.CurrentEpochStartingBlock.Uint64() == prevEpoch.CurrentEpochStartingBlock.Uint64()+prevEpoch.EpochLen
}
