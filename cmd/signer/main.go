package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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

const logLevels = "trace, debug, info, warn, error"

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
			// Juno's error lists the trace level as "Level(-2)"
			return fmt.Errorf("unknown log level %q (known: %s)", logLevelF, logLevels)
		}
		logger, err = log.NewZapLogger(logLevel, log.WithColour(true))
		if err != nil {
			return err
		}

		privKey, err = readSignerKeyFromEnv(envFilePath, cmd.Flags().Changed("env"), logger)
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

		// SIGINT and SIGTERM stop the server gracefully, and the signer exits with code 0
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
	cmd.Flags().StringVar(
		&envFilePath,
		"env",
		".env",
		"Path to a dotenv file (KEY=value lines) that can set SIGNER_PRIVATE_KEY.\n"+
			"The default file is optional, a file passed with this flag must exist",
	)
	cmd.Flags().StringVar(&logLevelF, "log-level", log.INFO.String(), "Options: "+logLevels)

	return cmd
}

func main() {
	cmd := NewCommand()
	// Cobra already printed the error
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}

// Reads the private key from the SIGNER_PRIVATE_KEY env var, which the env file can set.
// The env file is optional unless it is passed explicitly
func readSignerKeyFromEnv(
	envFilePath string, explicitEnvFile bool, logger *log.ZapLogger,
) (string, error) {
	err := godotenv.Load(envFilePath)
	var pathErr *fs.PathError
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist) && !explicitEnvFile:
		logger.Debug("no env var file", zap.String("envFilePath", envFilePath))
	case errors.As(err, &pathErr):
		return "", fmt.Errorf("cannot read the env var file: %w", err)
	default:
		// The parse error isn't shown: it can quote the file content, and so the key
		return "", fmt.Errorf(
			"cannot parse the env var file %s: it must be a dotenv file with KEY=value lines",
			envFilePath,
		)
	}

	signerKey := os.Getenv("SIGNER_PRIVATE_KEY")
	if signerKey == "" {
		return "", errors.New(
			"SIGNER_PRIVATE_KEY env var is not set. Set it in the environment or in the " +
				"env var file (--env)",
		)
	}

	return signerKey, nil
}
