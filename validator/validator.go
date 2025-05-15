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

type Validator struct {
	provider *rpc.Provider
	signer   signerP.Signer
	logger   utils.ZapLogger

	// Used to initiate a websocket connection later on
	wsProvider string
}

func New(
	config *config.Config, snConfig *config.StarknetConfig, logger utils.ZapLogger, braavos bool,
) (Validator, error) {
	provider, err := NewProvider(config.Provider.Http, &logger)
	if err != nil {
		return Validator{}, err
	}

	var signer signerP.Signer
	if config.Signer.External() {
		externalSigner, err := signerP.NewExternalSigner(
			provider, &logger, &config.Signer, &snConfig.ContractAddresses, braavos,
		)
		if err != nil {
			return Validator{}, err
		}
		signer = &externalSigner
	} else {
		internalSigner, err := signerP.NewInternalSigner(
			provider, &logger, &config.Signer, &snConfig.ContractAddresses, braavos,
		)
		if err != nil {
			return Validator{}, err
		}
		signer = &internalSigner
	}

	return Validator{
		provider:   provider,
		signer:     signer,
		logger:     logger,
		wsProvider: config.Provider.Ws,
	}, nil
}

func (v *Validator) ChainID() string {
	chainID, err := v.provider.ChainID(context.Background())
	// This shouldn't ever happened because the chainID query is done during the validator
	// initialization with `New`. After that the value is cached, so we are just accessing
	// a property at this point
	if err != nil {
		panic(err)
	}
	return chainID
}

// Main execution loop of the program. Listens to the blockchain and sends
// attest invoke when it's the right time
func (v *Validator) Attest(
	ctx context.Context, maxRetries types.Retries, tracer metrics.Tracer,
) error {
	// Create the event dispatcher
	dispatcher := NewEventDispatcher[signerP.Signer]()
	wg := conc.NewWaitGroup()
	wg.Go(func() {
		dispatcher.Dispatch(v.signer, &v.logger, tracer)
		v.logger.Debug("Dispatch method finished")
	})
	defer wg.Wait()
	defer close(dispatcher.AttestRequired)

	return RunBlockHeaderWatcher(
		ctx, v.wsProvider, &v.logger, v.signer, &dispatcher, maxRetries, wg, tracer,
	)
}

func RunBlockHeaderWatcher[S signerP.Signer](
	ctx context.Context,
	wsProviderURL string,
	logger *utils.ZapLogger,
	signer S,
	dispatcher *EventDispatcher[S],
	maxRetries types.Retries,
	wg *conc.WaitGroup,
	tracer metrics.Tracer,
) error {
	cleanUp := func(wsProvider *rpc.WsProvider, headersFeed chan *rpc.BlockHeader) {
		wsProvider.Close()
		close(headersFeed)
	}

	for {
		wsProvider, headersFeed, clientSubscription, err := SubscribeToBlockHeaders(
			wsProviderURL, logger,
		)
		if err != nil {
			return err
		}

		stopProcessingHeaders := make(chan error)

		wg.Go(func() {
			err := ProcessBlockHeaders(headersFeed, signer, logger, dispatcher, maxRetries, tracer)
			if err != nil {
				stopProcessingHeaders <- err
			}
		})

		select {
		case err := <-clientSubscription.Err():
			logger.Errorw("Block header subscription", "error", err)
			logger.Debugw(
				"Ending headers subscription, closing websocket connection and retrying...",
			)
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
	tracer metrics.Tracer,
) error {
	noEpochSwitch := func(*EpochInfo, *EpochInfo) bool { return true }
	epochInfo, attestInfo, err := FetchEpochAndAttestInfoWithRetry(
		account, logger, nil, noEpochSwitch, maxRetries, "at app startup",
	)
	if err != nil {
		return err
	}

	// Update initial epoch info metrics
	tracer.UpdateEpochInfo(&epochInfo, attestInfo.TargetBlock.Uint64())

	SetTargetBlockHashIfExists(account, logger, &attestInfo)

	for blockHeader := range headersFeed {
		logger.Infof("Block %d received", blockHeader.Number)
		logger.Debugw("Block header information", "block header", blockHeader)

		// Update latest block number metric
		tracer.UpdateLatestBlockNumber(blockHeader.Number)

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
			tracer.UpdateEpochInfo(&epochInfo, attestInfo.TargetBlock.Uint64())
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
