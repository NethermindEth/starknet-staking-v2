package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	configP "github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/spf13/cobra"
)

const greeting = `

   _____  __  _   __     ___    __     __          
  / __/ |/ / | | / /__ _/ (_)__/ /__ _/ /____  ____
 _\ \/    /  | |/ / _ \/ / / _  / _ \/ __/ _ \/ __/
/___/_/|_/   |___/\_,_/_/_/\_,_/\_,_/\__/\___/_/v%s   
Validator program for Starknet stakers created by Nethermind

`

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
	var logger utils.ZapLogger

	preRunE := func(cmd *cobra.Command, args []string) error {
		// Config takes the values from flags directly,
		// then fills the missing ones from the env vars
		configFromEnv := configP.FromEnv()
		config.Fill(&configFromEnv)

		// It fills the missing one from the ones defined
		// in a config file
		if configPath != "" {
			configFromFile, err := configP.FromFile(configPath)
			if err != nil {
				return err
			}
			config.Fill(&configFromFile)
		}
		if err := config.Check(); err != nil {
			return err
		}

		parsedRetries, err := types.RetriesFromString(maxRetriesF)
		if err != nil {
			return err
		}
		maxRetries = parsedRetries

		logLevel := utils.NewLogLevel(utils.INFO)
		if err := logLevel.Set(logLevelF); err != nil {
			return err
		}

		loadedLogger, err := utils.NewZapLogger(logLevel, true)
		if err != nil {
			return err
		}

		logger = *loadedLogger

		return nil
	}

	run := func(cmd *cobra.Command, args []string) {
		fmt.Printf(greeting, validator.Version)

		v, err := validator.New(&config, &snConfig, logger, braavosAccount)
		for err != nil {
			if strings.Contains(err.Error(), "cannot connect to RPC provider") {
				logger.Errorf(
					"couldn't connect with RPC Provider at %s. Will retry after 3s",
					config.Provider.Http,
				)
				time.Sleep(3 * time.Second)
				v, err = validator.New(&config, &snConfig, logger, braavosAccount)
			} else {
				logger.Errorw(
					"cannot start validator. Unexpected error:",
					"error",
					err.Error(),
				)
				return
			}
		}

		var tracer metrics.Tracer = metrics.NewNoOpMetrics()
		if metricsF {
			// Create metrics server
			address := fmt.Sprintf("%s:%s", metricsHostF, metricsPortF)
			metrics := metrics.NewMetrics(address, v.ChainID(), &logger)
			tracer = metrics

			// // Setup signal handling for graceful shutdown
			// ctx, cancel := context.WithCancel(cmd.Context())
			// defer cancel()

			// Start metrics server in a goroutine
			go func() {
				if err := metrics.Start(); err != nil && err.Error() != "http: Server closed" {
					logger.Errorw("Failed to start metrics server", "error", err)
				}
			}()
			// Graceful shutdown at the end
			defer func() {
				shutdownCtx, shutdownCancel := context.WithTimeout(
					cmd.Context(), 5*time.Second,
				)
				defer shutdownCancel()
				if err := metrics.Stop(shutdownCtx); err != nil {
					logger.Errorw("Failed to stop metrics server", "error", err)
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
		go trackLatestRelease(cmd.Context(), &logger)

		// Wait for signal or error
		select {
		case <-signalCh:
			logger.Info("Received shutdown signal")
		case err := <-errCh:
			logger.Errorw("Validator stopped with error", "error", err)
		}
	}

	cmd := cobra.Command{
		Use:     "validator",
		Short:   "Validator program for Starknet stakers created by Nethermind",
		Version: validator.Version,
		PreRunE: preRunE,
		Run:     run,
		Args:    cobra.NoArgs,
	}

	// Config file path flag
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to JSON config file")

	// Config provider flags
	cmd.Flags().StringVar(&config.Provider.Http, "provider-http", "", "Provider http address")
	cmd.Flags().StringVar(&config.Provider.Ws, "provider-ws", "", "Provider ws address")

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
		"How many times to retry to get information required for attestation."+
			" It can be either a positive integer or the key word 'infinite'",
	)
	cmd.Flags().Float64Var(
		&balanceThreshold,
		"balance-threshold",
		100,
		"Triggers a warning if it detects the signer account (i.e. operational address)"+
			" stark balance below the specified threshold. One stark equals 1 << 1e18.",
	)
	cmd.Flags().BoolVar(
		&braavosAccount,
		"braavos-account",
		false,
		"Changes the the transaction version format from 0x3 to 1<<128 + 0x3, required by"+
			" Braavos accounts. Only applies for internal signing.",
	)
	cmd.Flags().StringVar(
		&logLevelF, "log-level", utils.INFO.String(), "Options: trace, debug, info, warn, error.",
	)

	return cmd
}

func main() {
	command := NewCommand()
	if err := command.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}
