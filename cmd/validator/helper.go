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

const startupRetryDelay = 3 * time.Second

func tryNewValidator(
	ctx context.Context,
	conf *config.Config,
	snConfig *config.StarknetConfig,
	maxRetries types.Retries,
	logger log.Logger,
) (validator.Validator, error) {
	retries := maxRetries
	for {
		v, err := validator.New(ctx, conf, snConfig, logger)
		if err == nil {
			return v, nil
		}
		if !errors.Is(err, validator.ErrTransient) || ctx.Err() != nil {
			return validator.Validator{}, fmt.Errorf("cannot start validator: %w", err)
		}
		if retries.IsZero() {
			return validator.Validator{}, fmt.Errorf(
				"cannot start validator, no retries left (--max-retries %s): %w",
				maxRetries.String(), err,
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
