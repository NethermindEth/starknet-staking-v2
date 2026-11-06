package validator

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/client"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
	"github.com/sourcegraph/conc"
	"go.uber.org/zap"
)

// The current version of the validator tool.  This is set at build time
var Version string = "dev"

const (
	wsDialRetryDelay    = 5 * time.Second
	resubscribeMinDelay = time.Second
	resubscribeMaxDelay = 30 * time.Second
)

var (
	errReorg             = errors.New("reorg notification")
	errSubscriptionEnded = errors.New("WS subscription ended")
)

type Validator struct {
	provider *rpc.Provider
	signer   signerP.Signer
	logger   log.Logger

	rewardSigner signerP.RewardSigner

	// Used to initiate a websocket connection later on
	wsProvider string
}

func New(
	ctx context.Context,
	conf *config.Config,
	snConfig *config.StarknetConfig,
	logger log.Logger,
	braavos bool,
) (Validator, error) {
	if err := checkWSProviderURL(conf.Provider.WS); err != nil {
		return Validator{}, err
	}

	provider, err := NewProvider(ctx, conf.Provider.HTTP, logger)
	if err != nil {
		return Validator{}, fmt.Errorf("failed to connect to provider: %w", err)
	}

	var signer signerP.Signer
	if conf.Signer.IsExternal() {
		externalSigner, err := signerP.NewExternalSigner(
			ctx,
			provider,
			logger,
			&conf.Signer,
			&snConfig.ContractAddresses,
			braavos,
		)
		if err != nil {
			return Validator{}, markIfTransient(
				fmt.Errorf("failed to connect to external signer: %w", err),
			)
		}
		signer = &externalSigner
		logger.Info(
			"using external signer",
			zap.String("externalSignerURL", conf.Signer.ExternalURL),
			zap.Bool("braavosAccount", braavos),
		)
	} else {
		internalSigner, err := signerP.NewInternalSigner(
			ctx,
			provider,
			logger,
			&conf.Signer,
			&snConfig.ContractAddresses,
			braavos,
		)
		if err != nil {
			return Validator{}, markIfTransient(
				fmt.Errorf("failed to initialise internal signer: %w", err),
			)
		}
		signer = &internalSigner
		logger.Info("using internal signer", zap.Bool("braavosAccount", braavos))
	}

	logger.Info(
		"Validation contracts",
		zap.String("validationContracts", signer.ValidationContracts().String()),
	)

	var rewardSigner signerP.RewardSigner
	if conf.Reward.IsSet() {
		epochInfo, err := signerP.FetchEpochInfo(signer)
		if err != nil {
			return Validator{}, markIfTransient(
				fmt.Errorf("failed to fetch staking address from epoch info: %w", err),
			)
		}

		rewardSigner, err = signerP.NewRewardSigner(
			ctx,
			provider,
			logger,
			&conf.Reward,
			&snConfig.ContractAddresses,
			&epochInfo.StakerAddress,
			conf.Reward.IsBraavos(),
			conf.Reward.IsExternal(),
		)
		if err != nil {
			return Validator{}, markIfTransient(
				fmt.Errorf("failed to initialise reward signer: %w", err),
			)
		}

		logger.Info(
			"Reward auto-claiming enabled",
			zap.String("rewardAddress", rewardSigner.Address().String()),
			zap.Bool("braavosAccount", conf.Reward.IsBraavos()),
		)
	}

	return Validator{
		provider:     provider,
		signer:       signer,
		logger:       logger,
		rewardSigner: rewardSigner,
		wsProvider:   conf.Provider.WS,
	}, nil
}

func (v *Validator) ChainID(ctx context.Context) string {
	chainID, err := v.provider.ChainID(ctx)
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
	ctx context.Context, maxRetries types.Retries, balanceThreshold float64, tracer metrics.Tracer,
) error {
	// Initial check of the account balance
	go CheckBalance(v.signer, v.rewardSigner, balanceThreshold, v.logger, tracer)

	// Create the event dispatcher
	dispatcher := NewEventDispatcher[signerP.Signer]()
	wg := conc.NewWaitGroup()
	wg.Go(func() {
		dispatcher.Dispatch(v.signer, v.rewardSigner, balanceThreshold, v.logger, tracer)
		v.logger.Debug("Dispatch method finished")
	})
	defer wg.Wait()
	defer close(dispatcher.PrepareAttest)

	return RunBlockHeaderWatcher(
		ctx, v.wsProvider, v.logger, v.signer, &dispatcher, maxRetries, wg, tracer,
	)
}

func RunBlockHeaderWatcher[S signerP.Signer](
	ctx context.Context,
	wsProviderURL string,
	logger log.Logger,
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

	retries := maxRetries
	failedSubscriptions := 0
	for {
		wsProvider, headersFeed, clientSubscription, err := SubscribeToBlockHeaders(
			ctx, wsProviderURL, logger,
		)
		if err != nil {
			if retries.IsZero() {
				return err
			}
			logger.Error(
				"cannot connect to ws provider",
				zap.String("wsProviderURL", wsProviderURL),
				zap.Error(err),
				zap.String("retriesLeft", retries.String()),
			)
			retries.Sub()
			Sleep(wsDialRetryDelay)

			continue
		}

		highestBefore, processedBefore := dispatcher.progress.highestBlock()
		var endErr error
		stopProcessingHeaders := make(chan error)
		wg.Go(func() {
			err := ProcessBlockHeaders(
				ctx,
				headersFeed,
				signer,
				logger,
				dispatcher,
				maxRetries,
				tracer,
			)
			if err != nil {
				stopProcessingHeaders <- err
			}
		})

		select {
		case <-ctx.Done():
			wg.Wait()

			return nil
		case endErr = <-clientSubscription.Err():
			logSubscriptionError(logger, wsProviderURL, endErr)
			cleanUp(wsProvider, headersFeed)
		case reorgEvent, ok := <-clientSubscription.Reorg():
			if ok {
				logger.Info(
					"reorg detected. Restarting WS subscription...",
					zap.Uint64("startBlock", reorgEvent.StartBlockNum),
					zap.Uint64("endBlock", reorgEvent.EndBlockNum),
				)
				endErr = errReorg
			} else {
				endErr = subscriptionEndError(clientSubscription)
				logSubscriptionError(logger, wsProviderURL, endErr)
			}
			cleanUp(wsProvider, headersFeed)
		case err := <-stopProcessingHeaders:
			logger.Error("processing block headers", zap.Error(err))
			cleanUp(wsProvider, headersFeed)

			return err
		}

		highestAfter, processedAfter := dispatcher.progress.highestBlock()
		if processedAfter && (!processedBefore || highestAfter > highestBefore) {
			retries = maxRetries
			failedSubscriptions = 0

			continue
		}
		if retries.IsZero() {
			return errors.Errorf(
				"WS subscription to %s ended %d times in a row without a new block header: %w",
				wsProviderURL, failedSubscriptions+1, endErr,
			)
		}
		failedSubscriptions++
		delay := resubscribeDelay(failedSubscriptions)
		logger.Warn(
			"WS subscription ended without a new block header. Resubscribing in "+
				delay.String(),
			zap.String("wsProviderURL", wsProviderURL),
			zap.String("retriesLeft", retries.String()),
		)
		retries.Sub()
		Sleep(delay)
	}
}

func logSubscriptionError(logger log.Logger, wsProviderURL string, err error) {
	logger.Error(
		"client subscription error",
		zap.String("wsProviderURL", wsProviderURL),
		zap.Error(err),
	)
}

func subscriptionEndError(clientSubscription *client.ClientSubscription) error {
	select {
	case err := <-clientSubscription.Err():
		if err != nil {
			return err
		}
	default:
	}

	return errSubscriptionEnded
}

func resubscribeDelay(failedSubscriptions int) time.Duration {
	delay := resubscribeMinDelay
	for range failedSubscriptions - 1 {
		delay *= 2
		if delay >= resubscribeMaxDelay {
			return resubscribeMaxDelay
		}
	}

	return delay
}

func ProcessBlockHeaders[Account signerP.Signer](
	ctx context.Context,
	headersFeed chan *rpc.BlockHeader,
	account Account,
	logger log.Logger,
	dispatcher *EventDispatcher[Account],
	maxRetries types.Retries,
	tracer metrics.Tracer,
) error {
	noEpochSwitch := func(*types.EpochInfo, *types.EpochInfo) bool { return true }
	epochInfo, attestInfo, err := FetchEpochAndAttestInfoWithRetry(
		account, logger, nil, noEpochSwitch, maxRetries, "at app startup", tracer,
	)
	if err != nil {
		return err
	}

	SetTargetBlockHashIfExists(account, logger, &attestInfo)

	if dispatcher.progress.newEpoch(epochInfo.EpochID) {
		logNewEpoch(&epochInfo, &attestInfo, logger)
	}
	tracer.UpdateEpochInfo(&epochInfo, attestInfo.TargetBlock.Uint64())

	for block := range headersFeed {
		if !dispatcher.progress.addBlock(block.Number) {
			logger.Debug("block header already processed, skipping it",
				zap.Uint64("blockNumber", block.Number),
			)

			continue
		}
		tracer.UpdateLatestBlockNumber(block.Number)

		// todo(rdr): look for some nice way of refactoring this if/else blocks
		if block.Number >= uint64(epochInfo.StartingBlock)+epochInfo.EpochLen {
			prevEpochInfo := epochInfo
			epochInfo, attestInfo, err = FetchEpochAndAttestInfoWithRetry(
				account,
				logger,
				&prevEpochInfo,
				CorrectEpochSwitch,
				maxRetries,
				strconv.FormatUint(prevEpochInfo.EpochID+1, 10),
				tracer,
			)
			if err != nil {
				return err
			}
			if dispatcher.progress.newEpoch(epochInfo.EpochID) {
				logNewEpoch(&epochInfo, &attestInfo, logger)
			}
			// Update epoch info metrics
			tracer.UpdateEpochInfo(&epochInfo, attestInfo.TargetBlock.Uint64())
		}
		logBlock(block.Number, &epochInfo, &attestInfo, logger)
		if uint64(attestInfo.TargetBlock) == block.Number {
			attestInfo.TargetBlockHash = types.BlockHash(*block.Hash)
			logger.Info(
				"Target block reached",
				zap.String("blockHash", block.Hash.String()),
			)
			dispatcher.PrepareAttest <- types.PrepareAttest{
				BlockHash: attestInfo.TargetBlockHash,
			}
		}

		blockNum := types.BlockNumber(block.Number)
		switch {
		case blockNum >= attestInfo.TargetBlock &&
			// From [target block, window start), make sure to prepare the transaction
			blockNum < attestInfo.WindowStart-1:
			dispatcher.PrepareAttest <- types.PrepareAttest{
				BlockHash: attestInfo.TargetBlockHash,
			}
		case blockNum >= attestInfo.WindowStart-1 &&
			// from [window start, window end), make sure the attestation is done
			blockNum < attestInfo.WindowEnd:
			dispatcher.DoAttest <- types.DoAttest{
				BlockHash: attestInfo.TargetBlockHash,
			}
		case blockNum == attestInfo.WindowEnd:
			dispatcher.EndOfWindow <- struct{}{}
		}
	}

	return nil
}

func SetTargetBlockHashIfExists[Account signerP.Signer](
	account Account,
	logger log.Logger,
	attestInfo *types.AttestInfo,
) {
	targetBlockNumber := attestInfo.TargetBlock.Uint64()
	res, err := account.BlockWithTxHashes(rpc.WithBlockNumber(targetBlockNumber))

	// If no error, then target block already exists
	if err == nil {
		if block, ok := res.(*rpc.BlockTxHashes); ok {
			attestInfo.TargetBlockHash = types.BlockHash(*block.Hash)
			logger.Info(
				"target block already exists. Registering block hash.",
				zap.Uint64("targetBlock", attestInfo.TargetBlock.Uint64()),
			)
		}
	}
}

const epochFetchWarnEvery = 10

func FetchEpochAndAttestInfoWithRetry[Signer signerP.Signer](
	signer Signer,
	logger log.Logger,
	prevEpoch *types.EpochInfo,
	isEpochSwitchCorrect func(prevEpoch *types.EpochInfo, newEpoch *types.EpochInfo) bool,
	maxRetries types.Retries,
	newEpochID string,
	tracer metrics.Tracer,
) (types.EpochInfo, types.AttestInfo, error) {
	// storing the initial value for error reporting
	totalRetryAmount := maxRetries.String()
	failedAttempts := 0

	newEpoch, newAttestInfo, err := signerP.FetchEpochAndAttestInfo(signer, logger)
	for err != nil || !isEpochSwitchCorrect(prevEpoch, &newEpoch) {
		failedAttempts++
		tracer.RecordEpochInfoFetchFailure()
		if maxRetries.IsZero() {
			break
		}
		logEpochFetchRetry(
			logger, failedAttempts, newEpochID, &maxRetries, err, prevEpoch, &newEpoch,
		)

		Sleep(time.Second)

		newEpoch, newAttestInfo, err = signerP.FetchEpochAndAttestInfo(signer, logger)
		maxRetries.Sub()
	}

	if err != nil {
		return types.EpochInfo{},
			types.AttestInfo{},
			errors.Errorf(
				"failed to fetch epoch info after %s retries. Epoch id: %s. Error: %w",
				totalRetryAmount,
				newEpochID,
				err,
			)
	}
	if !isEpochSwitchCorrect(prevEpoch, &newEpoch) {
		return types.EpochInfo{},
			types.AttestInfo{},
			errors.Errorf("wrong epoch switch after %s retries from epoch:\n%s\nTo epoch:\n%s",
				totalRetryAmount,
				prevEpoch.String(),
				newEpoch.String(),
			)
	}
	if failedAttempts > 0 {
		logger.Info(
			"fetched epoch info after failed attempts. Resuming block processing",
			zap.String("epochID", newEpochID),
			zap.Int("failedAttempts", failedAttempts),
		)
	}

	return newEpoch, newAttestInfo, nil
}

func logEpochFetchRetry(
	logger log.Logger,
	failedAttempts int,
	newEpochID string,
	retriesLeft *types.Retries,
	err error,
	prevEpoch, newEpoch *types.EpochInfo,
) {
	logFn := logger.Debug
	if failedAttempts%epochFetchWarnEvery == 1 {
		logFn = logger.Warn
	}

	fields := []zap.Field{
		zap.String("epochID", newEpochID),
		zap.Int("failedAttempts", failedAttempts),
		zap.String("retriesRemaining", retriesLeft.String()),
	}
	if err != nil {
		fields = append(fields, zap.Error(err))
		logFn("failed to fetch epoch info, block headers are not processed. Retrying in 1s",
			fields...)

		return
	}
	fields = append(fields, zap.Any("fromEpoch", prevEpoch), zap.Any("toEpoch", newEpoch))
	logFn("wrong epoch switch, block headers are not processed. Retrying in 1s", fields...)
}

func CorrectEpochSwitch(prevEpoch, newEpoch *types.EpochInfo) bool {
	return newEpoch.EpochID == prevEpoch.EpochID+1 &&
		newEpoch.StartingBlock.Uint64() == prevEpoch.StartingBlock.Uint64()+prevEpoch.EpochLen
}
