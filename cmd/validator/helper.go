package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
)

func tryNewValidator(
	ctx context.Context,
	config *config.Config,
	snConfig *config.StarknetConfig,
	retries types.Retries,
	logger utils.ZapLogger,
	braavosAccount bool,
) (validator.Validator, error) {
	for {
		v, err := validator.New(ctx, config, snConfig, logger, braavosAccount)
		if err == nil {
			return v, nil
		}

		if strings.Contains(err.Error(), "cannot connect to RPC provider") {
			logger.Warnf(
				"couldn't connect with RPC Provider at %s (attempts left: %s)."+
					" Retrying in 3s...",
				config.Provider.Http,
				retries.String(),
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
					"RPC provider unreachable at %s", config.Provider.Http,
				)
		}
	}
}
