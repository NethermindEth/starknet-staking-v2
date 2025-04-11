package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/signer"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

func NewCommand() cobra.Command {
	var address string
	var envFilePath string
	var logLevelF string

	var privKey string
	var logger *utils.ZapLogger

	preRunE := func(_ *cobra.Command, args []string) error {
		var err error

		var logLevel utils.LogLevel
		if err := logLevel.Set(logLevelF); err != nil {
			return err
		}
		logger, err = utils.NewZapLogger(logLevel, true)
		if err != nil {
			return err
		}

		privKey, err = readSignerKeyFromEnv(envFilePath, logger)
		if err != nil {
			return err
		}

		return nil
	}

	runE := func(_ *cobra.Command, args []string) error {
		remoteSigner, err := signer.New(privKey, logger)
		if err != nil {
			return err
		}
		return remoteSigner.Listen(address)
	}

	cmd := cobra.Command{
		Use:     "signer",
		Short:   "Program that signs transactions hashes recieved by http request",
		PreRunE: preRunE,
		RunE:    runE,
		Args:    cobra.NoArgs,
	}

	cmd.Flags().StringVar(
		&address, "address", "localhost:8080", "Address where to listen for requests",
	)
	cmd.Flags().StringVar(&envFilePath, "env", ".env", "Path to JSON config file")
	cmd.Flags().StringVar(
		&logLevelF, "log-level", utils.INFO.String(), "Options: trace, debug, info, warn, error",
	)

	return cmd
}

func main() {
	cmd := NewCommand()
	err := cmd.ExecuteContext(context.Background())
	if err != nil {
		fmt.Printf("Signer closed unexepctedly: %s", err)
		os.Exit(1)
	}
}

func readSignerKeyFromEnv(envFilePath string, logger *utils.ZapLogger) (string, error) {
	err := godotenv.Load(envFilePath)
	if err != nil {
		logger.Debugf("couldn't load env var at %s: %s", envFilePath, err)
	}

	signerKey := os.Getenv("SIGNER_PRIVATE_KEY")
	if signerKey == "" {
		return "",
			errors.New(
				"couldn't read SIGNER_PRIVATE_KEY env var." +
					"Please make sure it is set before running this program",
			)
	}

	return signerKey, nil
}
