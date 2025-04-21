package main

import (
	"context"
	"fmt"
	"os"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/spf13/cobra"
)

func NewCommand() cobra.Command {
	var configPath string
	var logLevelF string

	var config validator.Config
	var logger utils.ZapLogger

	preRunE := func(cmd *cobra.Command, args []string) error {
		// Config takes the values from flags directly,
		// then fills the missing ones from the env vars
		configFromEnv := validator.ConfigFromEnv()
		config.Fill(&configFromEnv)

		// It fills the missing one from the ones defined
		// in a config file
		if configPath != "" {
			configFromFile, err := validator.ConfigFromFile(configPath)
			if err != nil {
				return err
			}
			config.Fill(&configFromFile)
		}

		if err := config.Check(); err != nil {
			return err
		}

		var logLevel utils.LogLevel
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
		if err := validator.Attest(&config, logger); err != nil {
			logger.Error(err)
		}
	}

	cmd := cobra.Command{
		Use:     "validator",
		Short:   "Program for Starknet validators to attest to epochs with respect to Staking v2",
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
		&config.Signer.ExternalUrl,
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

	// Other flags
	cmd.Flags().StringVar(
		&logLevelF, "log-level", utils.INFO.String(), "Options: trace, debug, info, warn, error.",
	)

	return cmd
}

func main() {
	command := NewCommand()
	if err := command.ExecuteContext(context.Background()); err != nil {
		fmt.Println("Unexpected error:\n", err)
		os.Exit(1)
	}
}
