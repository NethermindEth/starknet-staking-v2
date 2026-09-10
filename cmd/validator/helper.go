package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"go.uber.org/zap"
)

func tryNewValidator(
	ctx context.Context,
	conf *config.Config,
	snConfig *config.StarknetConfig,
	retries types.Retries,
	logger log.Logger,
	braavosAccount bool,
) (validator.Validator, error) {
	for {
		v, err := validator.New(ctx, conf, snConfig, logger, braavosAccount)
		if err == nil {
			return v, nil
		}

		if strings.Contains(err.Error(), "cannot connect to RPC provider") {
			logger.Warn(
				"couldn't connect with RPC Provider. Retrying in 3s...",
				zap.String("provider", conf.Provider.HTTP),
				zap.String("attemptsLeft", retries.String()),
			)
			time.Sleep(3 * time.Second)
		} else {
			return validator.Validator{},
				fmt.Errorf("cannot start validator. Unexepcted error: %w", err)
		}

		retries.Sub()
		if retries.IsZero() {
			return validator.Validator{},
				fmt.Errorf(
					"RPC provider unreachable at %s", conf.Provider.HTTP,
				)
		}
	}
}
