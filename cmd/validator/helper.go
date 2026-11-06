package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"go.uber.org/zap"
)

// Delay between two attempts to start the validator
const startupRetryDelay = 3 * time.Second

// Creates the validator. Transient errors, such as an unreachable RPC provider, are retried
// up to `maxRetries` times; any other error, such as an unsupported node version or a wrong
// configuration, is returned at once.
func tryNewValidator(
	ctx context.Context,
	conf *config.Config,
	snConfig *config.StarknetConfig,
	maxRetries types.Retries,
	logger log.Logger,
	braavosAccount bool,
) (validator.Validator, error) {
	retries := maxRetries
	for {
		v, err := validator.New(ctx, conf, snConfig, logger, braavosAccount)
		if err == nil {
			return v, nil
		}
		if !errors.Is(err, validator.ErrTransient) || ctx.Err() != nil {
			return validator.Validator{}, fmt.Errorf("cannot start validator: %w", err)
		}
		if retries.IsZero() {
			return validator.Validator{}, fmt.Errorf(
				"cannot start validator after %s retries: %w", maxRetries.String(), err,
			)
		}

		logger.Warn(
			"cannot start validator. Retrying in "+startupRetryDelay.String(),
			zap.Error(err),
			zap.String("retriesLeft", retries.String()),
		)
		retries.Sub()
		validator.Sleep(startupRetryDelay)
	}
}
