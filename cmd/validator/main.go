package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	configP "github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const greeting = `

   _____  __  _   __     ___    __     __          
  / __/ |/ / | | / /__ _/ (_)__/ /__ _/ /____  ____
 _\ \/    /  | |/ / _ \/ / / _  / _ \/ __/ _ \/ __/
/___/_/|_/   |___/\_,_/_/_/\_,_/\_,_/\__/\___/_/v%s   
Validator program for Starknet stakers created by Nethermind

`

const longDescription = `Validator program for Starknet stakers created by Nethermind

Configuration can be provided through flags, environment variables or a JSON
config file, in that order of priority.

Auto-claim reward feature (optional):
  Automatically claims the staker rewards once the unclaimed amount 
  reaches --reward-claim-threshold. If any reward option is set, --reward-address,
  --reward-claim-threshold and exactly one of --reward-priv-key or
  --reward-signer-url are required.
	
Full documentation: https://nethermindeth.github.io/starknet-staking-v2/	
`

//nolint:funlen // It's the main function, so it's normal to be long
func NewCommand() cobra.Command {
	var configPath string
	var logLevelF string
	var maxRetriesF string
	var metricsF bool
	var metricsHostF string
	var metricsPortF string
	var braavosAccount bool

	var config configP.Config
	var maxRetries types.Retries
	var balanceThreshold float64
	var snConfig configP.StarknetConfig
	var logger *log.ZapLogger

	preRunE := func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(&config, configPath); err != nil {
			return err
		}

		parsedRetries, err := types.RetriesFromString(maxRetriesF)
		if err != nil {
			return err
		}
		maxRetries = parsedRetries

		logLevel := log.NewLevel(log.INFO)
		err = logLevel.Set(logLevelF)
		if err != nil {
			return err
		}

		loadedLogger, err := log.NewZapLogger(logLevel, log.WithColour(true))
		if err != nil {
			return err
		}

		logger = loadedLogger

		return nil
	}

	run := func(cmd *cobra.Command, args []string) {
		fmt.Printf(greeting, validator.Version)

		v, err := tryNewValidator(
			cmd.Context(),
			&config,
			&snConfig,
			maxRetries,
			logger,
			braavosAccount,
		)
		if err != nil {
			logger.Error("couldn't start validator", zap.Error(err))

			return
		}

		var tracer metrics.Tracer = metrics.NewNoOpMetrics()
		if metricsF {
			// Create metrics server
			address := fmt.Sprintf("%s:%s", metricsHostF, metricsPortF)
			metrics := metrics.NewMetrics(address, v.ChainID(cmd.Context()), logger)
			tracer = metrics

			// Start metrics server in a goroutine
			go func() {
				if err := metrics.Start(); err != nil && err.Error() != "http: Server closed" {
					logger.Error("failed to start metrics server", zap.Error(err))
				}
			}()
			// Graceful shutdown at the end
			defer func() {
				shutdownCtx, shutdownCancel := context.WithTimeout(
					cmd.Context(), 5*time.Second, //nolint:mnd // Timeout time
				)
				defer shutdownCancel()
				if err := metrics.Stop(shutdownCtx); err != nil {
					logger.Error("failed to stop metrics server", zap.Error(err))
				}
			}()
		}

		signalCh := make(chan os.Signal, 1)
		signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

		// Start validator in a goroutine
		errCh := make(chan error, 1)
		go func() {
			err := v.Attest(cmd.Context(), maxRetries, balanceThreshold, tracer)
			if err != nil {
				errCh <- err
			}
		}()

		// run upgrader tracker
		go trackLatestRelease(cmd.Context(), logger)

		// Wait for signal or error
		select {
		case <-signalCh:
			logger.Info("Received shutdown signal")
		case err := <-errCh:
			logger.Error("validator stopped with error", zap.Error(err))
		}
	}

	//nolint:exhaustruct_v5 // Only specifying used fields
	cmd := cobra.Command{
		Use:     "validator",
		Short:   "Validator program for Starknet stakers created by Nethermind",
		Long:    longDescription,
		Version: validator.Version,
		PreRunE: preRunE,
		Run:     run,
		Args:    cobra.NoArgs,
	}

	// Config file path flag
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to JSON config file")

	// Config provider flags
	cmd.Flags().StringVar(&config.Provider.HTTP, "provider-http", "", "Provider http address")
	cmd.Flags().StringVar(&config.Provider.WS, "provider-ws", "", "Provider ws address")

	// Config signer flags
	cmd.Flags().StringVar(
		&config.Signer.ExternalURL,
		"signer-url",
		"",
		"Signer url address, required if using an external signer",
	)
	cmd.Flags().StringVar(
		&config.Signer.PrivKey, "signer-priv-key", "", "Signer private key, required for signing",
	)
	cmd.Flags().StringVar(
		&config.Signer.OperationalAddress,
		"signer-op-address",
		"",
		"Signer operational address, required for attesting",
	)

	// Config reward flags
	cmd.Flags().StringVar(
		&config.Reward.Address,
		"reward-address",
		"",
		"(Optional) Reward account address. \n"+
			"Required for the auto-claim reward feature.",
	)
	cmd.Flags().Uint64Var(
		&config.Reward.ClaimMaxFee,
		"reward-claim-max-fee",
		0,
		"(Optional) Max fee, in FRI, to pay for the claim transaction. \n"+
			" Defaults to 'unlimited' if not specified (max uint64, which is ~18.4 STRK)",
	)
	cmd.Flags().Uint64Var(
		&config.Reward.ClaimThreshold,
		"reward-claim-threshold",
		0,
		"(Optional) Amount of unclaimed rewards, in STRK, that triggers an automatic claim. \n"+
			" Required for the auto-claim reward feature.",
	)
	cmd.Flags().StringVar(
		&config.Reward.PrivKey,
		"reward-priv-key",
		"",
		"(Optional) Reward account private key, used for internal signing of the reward account. \n"+
			"Required for the auto-claim reward feature. Can be replaced by the 'reward-signer-url' flag.",
	)
	cmd.Flags().StringVar(
		&config.Reward.ExternalURL,
		"reward-signer-url",
		"",
		"(Optional) Reward signer url address, used for external signing of the reward account. \n"+
			"Required for the auto-claim reward feature. Can be replaced by the 'reward-priv-key' flag.",
	)

	// Config starknet flags
	cmd.Flags().StringVar(
		&snConfig.ContractAddresses.Attest,
		"attest-contract-address",
		"",
		"Staking contract address. Defaults values are provided for Sepolia and Mainnet",
	)
	cmd.Flags().StringVar(
		&snConfig.ContractAddresses.Staking,
		"staking-contract-address",
		"",
		"Staking contract address. Defaults values are provided for Sepolia and Mainnet",
	)

	// Metric tracking flags
	cmd.Flags().BoolVar(&metricsF, "metrics", false, "Enable metric tracking via Prometheus")
	cmd.Flags().StringVar(&metricsHostF, "metrics-host", "localhost", "Host for the metric server")
	cmd.Flags().StringVar(&metricsPortF, "metrics-port", "9090", "Port for the metric server")

	// Other flags
	cmd.Flags().StringVar(
		&maxRetriesF,
		"max-retries",
		"infinite",
		"How many times to retry to get information required for attestation.\n"+
			" It can be either a positive integer or the key word 'infinite'",
	)
	cmd.Flags().Float64Var(
		&balanceThreshold,
		"balance-threshold",
		100, //nolint:mnd // Default balance threshold (100 STRK)
		"Triggers a warning if it detects the signer account (i.e. operational address)\n"+
			"stark balance below the specified threshold. One stark equals 1 << 1e18.",
	)
	cmd.Flags().BoolVar(
		&braavosAccount,
		"braavos-account",
		false,
		"Changes the the transaction version format from 0x3 to 1<<128 + 0x3, required by\n"+
			"Braavos accounts. Only applies for internal signing.",
	)
	cmd.Flags().StringVar(
		&logLevelF, "log-level", log.INFO.String(), "Options: trace, debug, info, warn, error.",
	)

	return cmd
}

// Completes the config, which already holds the values from flags, with the
// ones from env vars and then from the config file. Afterwards it sets the
// defaults for the missing values and verifies the result
func loadConfig(config *configP.Config, configPath string) error {
	configFromEnv, err := configP.FromEnv()
	if err != nil {
		return err
	}
	config.Fill(&configFromEnv)

	if configPath != "" {
		configFromFile, err := configP.FromFile(configPath)
		if err != nil {
			return err
		}
		config.Fill(&configFromFile)
	}
	config.SetDefaults()

	return config.Check()
}

func main() {
	command := NewCommand()
	if err := command.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}
