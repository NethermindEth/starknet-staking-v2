package validator

import (
	"context"
	"fmt"
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

const Version = "0.2.7"

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
		return Validator{}, fmt.Errorf("failed to connect to provider: %w", err)
	}

	var signer signerP.Signer
	if config.Signer.External() {
		externalSigner, err := signerP.NewExternalSigner(
			context.Background(),
			provider,
			&logger,
			&config.Signer,
			&snConfig.ContractAddresses,
			braavos,
		)
		if err != nil {
			return Validator{}, fmt.Errorf("failed to connect to external signer: %w", err)
		}
		signer = &externalSigner
		logger.Infof("using external signer at %s", config.Signer.ExternalURL)
	} else {
		internalSigner, err := signerP.NewInternalSigner(
			context.Background(),
			provider,
			&logger,
			&config.Signer,
			&snConfig.ContractAddresses,
			braavos,
		)
		if err != nil {
			return Validator{}, fmt.Errorf("failed to initialize internal signer: %w", err)
		}
		signer = &internalSigner
		logger.Info("using internal signer")
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
	ctx context.Context, maxRetries types.Retries, balanceThreshold float64, tracer metrics.Tracer,
) error {
	// Initial check of the account balance
	go CheckBalance(v.signer, balanceThreshold, &v.logger, tracer)

	// Create the event dispatcher
	dispatcher := NewEventDispatcher[signerP.Signer]()
	wg := conc.NewWaitGroup()
	wg.Go(func() {
		dispatcher.Dispatch(v.signer, balanceThreshold, &v.logger, tracer)
		v.logger.Debug("Dispatch method finished")
	})
	defer wg.Wait()
	defer close(dispatcher.PrepareAttest)

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

	retries := maxRetries
	for {
		wsProvider, headersFeed, clientSubscription, err := SubscribeToBlockHeaders(
			ctx, wsProviderURL, logger,
		)
		if err != nil {
			if retries.IsZero() {
				return err
			}
			logger.Errorf("cannot connect to ws provider, %s retries left.", &retries)
			logger.Debug(err.Error())
			retries.Sub()
			Sleep(5 * time.Second)
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
			logger.Errorw("client subscription error", "error", err.Error())
			logger.Debug("ending headers subscription, closing websocket connection and retrying...")
			cleanUp(wsProvider, headersFeed)
		case err := <-stopProcessingHeaders:
			logger.Errorw("processing block headers", "error", err.Error())
			cleanUp(wsProvider, headersFeed)
			return err
		}
	}
}

func ProcessBlockHeaders[Account signerP.Signer](
	ctx context.Context,
	headersFeed chan *rpc.BlockHeader,
	account Account,
	logger *utils.ZapLogger,
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
				strconv.FormatUint(prevEpochInfo.EpochId+1, 10),
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
			logger.Infow(
				"Target block reached",
				"block hash", block.Hash,
			)
			dispatcher.PrepareAttest <- types.PrepareAttest{
				BlockHash: attestInfo.TargetBlockHash,
			}
		}

		if types.BlockNumber(block.Number) >= attestInfo.TargetBlock &&
			// From [target block, window start), make sure to prepare the transaction
			types.BlockNumber(block.Number) < attestInfo.WindowStart-1 {
			dispatcher.PrepareAttest <- types.PrepareAttest{
				BlockHash: attestInfo.TargetBlockHash,
			}
		} else if types.BlockNumber(block.Number) >= attestInfo.WindowStart-1 &&
			// from [window start, window end), make sure the attestation is done
			types.BlockNumber(block.Number) < attestInfo.WindowEnd {
			dispatcher.DoAttest <- types.DoAttest{
				BlockHash: attestInfo.TargetBlockHash,
			}
		} else if types.BlockNumber(block.Number) == attestInfo.WindowEnd {
			dispatcher.EndOfWindow <- struct{}{}
		}
	}

	return nil
}

func SetTargetBlockHashIfExists[Account signerP.Signer](
	account Account,
	logger *utils.ZapLogger,
	attestInfo *types.AttestInfo,
) {
	targetBlockNumber := attestInfo.TargetBlock.Uint64()
	res, err := account.BlockWithTxHashes(rpc.BlockID{Number: &targetBlockNumber})

	// If no error, then target block already exists
	if err == nil {
		if block, ok := res.(*rpc.BlockTxHashes); ok {
			attestInfo.TargetBlockHash = types.BlockHash(*block.Hash)
			logger.Infow(
				"target block already exists. Registering block hash.",
				"target block", attestInfo.TargetBlock.Uint64(),
			)
		}
	}
}

func FetchEpochAndAttestInfoWithRetry[Signer signerP.Signer](
	signer Signer,
	logger *utils.ZapLogger,
	prevEpoch *types.EpochInfo,
	isEpochSwitchCorrect func(prevEpoch *types.EpochInfo, newEpoch *types.EpochInfo) bool,
	maxRetries types.Retries,
	newEpochId string,
) (types.EpochInfo, types.AttestInfo, error) {
	// storing the initial value for error reporting
	totalRetryAmount := maxRetries.String()

	newEpoch, newAttestInfo, err := signerP.FetchEpochAndAttestInfo(signer, logger)

	for (err != nil || !isEpochSwitchCorrect(prevEpoch, &newEpoch)) && !maxRetries.IsZero() {
		if err != nil {
			logger.Debugw("failed to fetch epoch info",
				"epoch id", newEpochId,
				"error", err.Error(),
			)
		} else {
			logger.Debugw(
				"wrong epoch switch",
				"from epoch", prevEpoch,
				"to epoch", &newEpoch,
			)
		}
		logger.Debugf("retrying to fetch epoch info: %s retries remaining", &maxRetries)

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
				newEpochId,
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

func CorrectEpochSwitch(prevEpoch *types.EpochInfo, newEpoch *types.EpochInfo) bool {
	return newEpoch.EpochId == prevEpoch.EpochId+1 &&
		newEpoch.StartingBlock.Uint64() == prevEpoch.StartingBlock.Uint64()+prevEpoch.EpochLen
}
