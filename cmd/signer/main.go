package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func NewCommand() cobra.Command {
	var address string
	var envFilePath string
	var logLevelF string

	var privKey string
	var logger *log.ZapLogger

	preRunE := func(cmd *cobra.Command, args []string) error {
		// Flags and args are already parsed at this point, so from here on
		// errors are not about the command usage and it shouldn't be printed
		cmd.SilenceUsage = true

		var err error

		logLevel := log.NewLevel(log.INFO)
		err = logLevel.Set(logLevelF)
		if err != nil {
			return err
		}
		logger, err = log.NewZapLogger(logLevel, log.WithColour(true))
		if err != nil {
			return err
		}

		privKey, err = readSignerKeyFromEnv(envFilePath, logger)
		if err != nil {
			return err
		}

		return nil
	}

	runE := func(cmd *cobra.Command, args []string) error {
		remoteSigner, err := signer.New(privKey, logger)
		if err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		return remoteSigner.Listen(ctx, address)
	}

	//nolint:exhaustruct_v5 // Only specifying used fields
	cmd := cobra.Command{
		Use:     "signer",
		Short:   "Program that signs transactions received by http request",
		Version: validator.Version,
		PreRunE: preRunE,
		RunE:    runE,
		Args:    cobra.NoArgs,
	}

	cmd.Flags().StringVar(
		&address, "address", "localhost:8080", "Address where to listen for requests",
	)
	cmd.Flags().StringVar(&envFilePath, "env", ".env", "Path to JSON config file")
	cmd.Flags().StringVar(
		&logLevelF, "log-level", log.INFO.String(), "Options: trace, debug, info, warn, error",
	)

	return cmd
}

func main() {
	cmd := NewCommand()
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}

func readSignerKeyFromEnv(envFilePath string, logger *log.ZapLogger) (string, error) {
	err := godotenv.Load(envFilePath)
	if err != nil {
		logger.Debug("couldn't load env var file",
			zap.String("envFilePath", envFilePath),
			zap.Error(err),
		)
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
