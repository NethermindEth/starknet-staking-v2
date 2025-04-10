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
		if configPath != "" {
			fileConfig, err := validator.ConfigFromFile(configPath)
			if err != nil {
				return err
			}
			config.Fill(&fileConfig)
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

	var rootCmd = cobra.Command{
		Use:     "validator",
		Short:   "Program for Starknet validators to attest to epochs with respect to Staking v2",
		PreRunE: preRunE,
		Run:     run,
		Args:    cobra.NoArgs,
	}

	// Config file path flag
	rootCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to JSON config file")

	// Config provider flags
	rootCmd.Flags().StringVar(&config.Provider.Http, "provider-http", "", "Provider http address")
	rootCmd.Flags().StringVar(&config.Provider.Ws, "provider-ws", "", "Provider ws address")
	// Config signer flags
	rootCmd.Flags().StringVar(
		&config.Signer.ExternalUrl,
		"signer-url",
		"",
		"Signer url address, required if using an external signer",
	)
	rootCmd.Flags().StringVar(
		&config.Signer.PrivKey, "signer-priv-key", "", "Signer private key, required for signing",
	)
	rootCmd.Flags().StringVar(
		&config.Signer.OperationalAddress,
		"signer-op-address",
		"",
		"Signer operational address, required for attesting",
	)

	// Other flags
	rootCmd.Flags().StringVar(
		&logLevelF, "log-level", utils.INFO.String(), "Options: trace, debug, info, warn, error.",
	)

	return rootCmd
}

func main() {
	command := NewCommand()
	if err := command.ExecuteContext(context.Background()); err != nil {
		fmt.Println("Unexpected error:\n", err)
		os.Exit(1)
	}
}
