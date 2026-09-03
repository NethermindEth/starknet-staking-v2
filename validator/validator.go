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
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
	"github.com/sourcegraph/conc"
	"go.uber.org/zap"
)

// The current version of the validator tool.  This is set at build time
var Version string = "dev"

type Validator struct {
	provider *rpc.Provider
	signer   signerP.Signer
	logger   log.Logger

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
	provider, err := NewProvider(ctx, conf.Provider.HTTP, logger)
	if err != nil {
		return Validator{}, fmt.Errorf("failed to connect to provider: %w", err)
	}

	var signer signerP.Signer
	if conf.Signer.External() {
		externalSigner, err := signerP.NewExternalSigner(
			ctx,
			provider,
			logger,
			&conf.Signer,
			&snConfig.ContractAddresses,
			braavos,
		)
		if err != nil {
			return Validator{}, fmt.Errorf("failed to connect to external signer: %w", err)
		}
		signer = &externalSigner
		logger.Info(
			"using external signer",
			zap.String("externalSignerURL", conf.Signer.ExternalURL),
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
			return Validator{}, fmt.Errorf("failed to initialise internal signer: %w", err)
		}
		signer = &internalSigner
		logger.Info("using internal signer")
	}

	return Validator{
		provider:   provider,
		signer:     signer,
		logger:     logger,
		wsProvider: conf.Provider.WS,
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
	go CheckBalance(v.signer, balanceThreshold, v.logger, tracer)

	// Create the event dispatcher
	dispatcher := NewEventDispatcher[signerP.Signer]()
	wg := conc.NewWaitGroup()
	wg.Go(func() {
		dispatcher.Dispatch(v.signer, balanceThreshold, v.logger, tracer)
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
				zap.String("retriesLeft", retries.String()),
			)
			logger.Debug(err.Error())
			retries.Sub()
			Sleep(5 * time.Second) //nolint:mnd // Number of seconds to sleep

			continue
		}
		retries = maxRetries

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
		case err := <-clientSubscription.Err():
			logger.Error("client subscription error", zap.Error(err))
			cleanUp(wsProvider, headersFeed)
		case reorgEvent := <-clientSubscription.Reorg():
			logger.Info(
				"reorg detected. Restarting WS subscription...",
				zap.Uint64("startBlock", reorgEvent.StartBlockNum),
				zap.Uint64("endBlock", reorgEvent.EndBlockNum),
			)
			cleanUp(wsProvider, headersFeed)
		case err := <-stopProcessingHeaders:
			logger.Error("processing block headers", zap.Error(err))
			cleanUp(wsProvider, headersFeed)

			return err
		}
	}
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
		account, logger, nil, noEpochSwitch, maxRetries, "at app startup",
	)
	if err != nil {
		return err
	}

	SetTargetBlockHashIfExists(account, logger, &attestInfo)

	logNewEpoch(&epochInfo, &attestInfo, logger)
	tracer.UpdateEpochInfo(&epochInfo, attestInfo.TargetBlock.Uint64())

	for block := range headersFeed {
		logBlock(block.Number, &epochInfo, &attestInfo, logger)
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
			)
			if err != nil {
				return err
			}
			logNewEpoch(&epochInfo, &attestInfo, logger)
			// Update epoch info metrics
			tracer.UpdateEpochInfo(&epochInfo, attestInfo.TargetBlock.Uint64())
		}
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

func FetchEpochAndAttestInfoWithRetry[Signer signerP.Signer](
	signer Signer,
	logger log.Logger,
	prevEpoch *types.EpochInfo,
	isEpochSwitchCorrect func(prevEpoch *types.EpochInfo, newEpoch *types.EpochInfo) bool,
	maxRetries types.Retries,
	newEpochID string,
) (types.EpochInfo, types.AttestInfo, error) {
	// storing the initial value for error reporting
	totalRetryAmount := maxRetries.String()

	newEpoch, newAttestInfo, err := signerP.FetchEpochAndAttestInfo(signer, logger)

	for (err != nil || !isEpochSwitchCorrect(prevEpoch, &newEpoch)) && !maxRetries.IsZero() {
		if err != nil {
			logger.Debug("failed to fetch epoch info",
				zap.String("epochID", newEpochID),
				zap.Error(err),
			)
		} else {
			logger.Debug(
				"wrong epoch switch",
				zap.Any("fromEpoch", prevEpoch),
				zap.Any("toEpoch", &newEpoch),
			)
		}
		logger.Debug(
			"retrying to fetch epoch info",
			zap.String("retriesRemaining", maxRetries.String()),
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

	return newEpoch, newAttestInfo, nil
}

func CorrectEpochSwitch(prevEpoch, newEpoch *types.EpochInfo) bool {
	return newEpoch.EpochID == prevEpoch.EpochID+1 &&
		newEpoch.StartingBlock.Uint64() == prevEpoch.StartingBlock.Uint64()+prevEpoch.EpochLen
}
