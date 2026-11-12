package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
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

const logLevels = "trace, debug, info, warn, error"

//nolint:funlen // It's the main function, so it's normal to be long
func NewCommand() cobra.Command {
	var configPath string
	var logLevelF string
	var maxRetriesF string
	var metricsF bool
	var metricsHostF string
	var metricsPortF string

	var config configP.Config
	var maxRetries types.Retries
	var balanceThreshold float64
	var snConfig configP.StarknetConfig
	var logger *log.ZapLogger
	var maxTipF uint64

	preRunE := func(cmd *cobra.Command, args []string) error {
		// Flags and args are already parsed at this point, so from here on
		// errors are not about the command usage and it shouldn't be printed
		cmd.SilenceUsage = true

		if err := checkThresholdFlags(cmd, &config, balanceThreshold); err != nil {
			return err
		}
		if cmd.Flags().Changed("max-tip") {
			config.MaxTip = &maxTipF
		}
		if err := loadConfig(&config, configPath); err != nil {
			return err
		}

		parsedRetries, err := types.RetriesFromString(maxRetriesF)
		if err != nil {
			return err
		}
		maxRetries = parsedRetries

		if metricsF {
			if err = checkMetricsAddress(cmd.Context(), metricsHostF, metricsPortF); err != nil {
				return fmt.Errorf("metrics: %w", err)
			}
		}

		logLevel := log.NewLevel(log.INFO)
		err = logLevel.Set(logLevelF)
		if err != nil {
			return fmt.Errorf("unknown log level %q (known: %s)", logLevelF, logLevels)
		}

		loadedLogger, err := log.NewZapLogger(logLevel, log.WithColour(isTerminal(os.Stderr)))
		if err != nil {
			return err
		}

		logger = loadedLogger.WithOptions(zap.AddStacktrace(zap.DPanicLevel))

		return nil
	}

	runE := func(cmd *cobra.Command, args []string) error {
		// Errors from here on are already logged, so cobra shouldn't print them again
		cmd.SilenceErrors = true

		fmt.Printf(greeting, validator.Version)

		var metricsAddress string
		if metricsF {
			metricsAddress = net.JoinHostPort(metricsHostF, metricsPortF)
		}

		return run(cmd.Context(), &runOptions{
			config:           &config,
			snConfig:         &snConfig,
			maxRetries:       maxRetries,
			balanceThreshold: balanceThreshold,
			metricsAddress:   metricsAddress,
			logger:           logger,
		})
	}

	//nolint:exhaustruct_v5 // Only specifying used fields
	cmd := cobra.Command{
		Use:     "validator",
		Short:   "Validator program for Starknet stakers created by Nethermind",
		Long:    longDescription,
		Version: validator.Version,
		PreRunE: preRunE,
		RunE:    runE,
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
		"Signer url address, required if using an external signer. Exclusive with --signer-priv-key",
	)
	cmd.Flags().StringVar(
		&config.Signer.PrivKey,
		"signer-priv-key",
		"",
		"Signer private key, required if using internal signing. Exclusive with --signer-url",
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
		"(Optional) Reward account address.\n"+
			"Required for the auto-claim reward feature.",
	)
	cmd.Flags().Uint64Var(
		&config.Reward.ClaimMaxFee,
		"reward-claim-max-fee",
		0,
		"(Optional) Max fee, in FRI, to pay for the claim transaction.\n"+
			"Defaults to 'unlimited' if not specified (max uint64, which is ~18.4 STRK)",
	)
	cmd.Flags().Uint64Var(
		&config.Reward.ClaimThreshold,
		"reward-claim-threshold",
		0,
		"(Optional) Amount of unclaimed rewards, in STRK, that triggers an automatic claim.\n"+
			"Required for the auto-claim reward feature.",
	)
	cmd.Flags().StringVar(
		&config.Reward.PrivKey,
		"reward-priv-key",
		"",
		"(Optional) Reward account private key, used for internal signing of the reward account.\n"+
			"Required for the auto-claim reward feature. Can be replaced by the 'reward-signer-url' flag.",
	)
	cmd.Flags().StringVar(
		&config.Reward.ExternalURL,
		"reward-signer-url",
		"",
		"(Optional) Reward signer url address, used for external signing of the reward account.\n"+
			"Required for the auto-claim reward feature. Can be replaced by the 'reward-priv-key' flag.",
	)

	// Config starknet flags
	cmd.Flags().StringVar(
		&snConfig.ContractAddresses.Attest,
		"attest-contract-address",
		"",
		"Attestation contract address. Default values are provided for Sepolia and Mainnet",
	)
	cmd.Flags().StringVar(
		&snConfig.ContractAddresses.Staking,
		"staking-contract-address",
		"",
		"Staking contract address. Default values are provided for Sepolia and Mainnet",
	)

	// Metric tracking flags
	cmd.Flags().BoolVar(&metricsF, "metrics", false, "Enable metric tracking via Prometheus")
	cmd.Flags().StringVar(
		&metricsHostF,
		"metrics-host",
		"localhost",
		"Host for the metric server. An empty value listens on all interfaces",
	)
	cmd.Flags().StringVar(&metricsPortF, "metrics-port", "9090", "Port for the metric server")

	// Other flags
	cmd.Flags().StringVar(
		&maxRetriesF,
		"max-retries",
		"infinite",
		"How many times to retry to get information required for attestation.\n"+
			"It can be either a positive integer or the key word 'infinite'",
	)
	cmd.Flags().Float64Var(
		&balanceThreshold,
		"balance-threshold",
		100, //nolint:mnd // Default balance threshold (100 STRK)
		"Triggers a warning if it detects the signer account (i.e. operational address)\n"+
			"stark balance below the specified threshold. If the auto-claim reward feature\n"+
			"is configured, the reward account balance is checked as well.\n"+
			"The threshold is in STRK (1 STRK = 1e18 FRI).",
	)

	cmd.Flags().Uint64Var(
		&maxTipF,
		"max-tip",
		0,
		"Max tip, in FRI per L2 gas unit, to pay for the attestation transaction.\n"+
			"The estimated tip is capped to this value. Set it to 0 to never pay a tip.\n"+
			"Defaults to 'unlimited' if not specified (max uint64)",
	)

	cmd.Flags().StringVar(&logLevelF, "log-level", log.INFO.String(), "Options: "+logLevels+".")

	return cmd
}

type runOptions struct {
	config           *configP.Config
	snConfig         *configP.StarknetConfig
	maxRetries       types.Retries
	balanceThreshold float64
	metricsAddress   string
	logger           *log.ZapLogger
}

func run(ctx context.Context, opts *runOptions) error {
	logger := opts.logger

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	v, stopped, err := startValidator(ctx, signalCh, logger, func() (validator.Validator, error) {
		return tryNewValidator(
			ctx, opts.config, opts.snConfig, opts.maxRetries, logger,
		)
	})
	if stopped {
		return nil
	}
	if err != nil {
		logger.Error("couldn't start validator", errorField(err))

		return err
	}

	var tracer metrics.Tracer = metrics.NewNoOpMetrics()
	var metricsErrCh <-chan error
	if opts.metricsAddress != "" {
		metricsServer, errCh, stopMetrics := startMetricsServer(
			ctx, opts.metricsAddress, v.ChainID(ctx), logger,
		)
		tracer = metricsServer
		metricsErrCh = errCh
		// Graceful shutdown at the end
		defer stopMetrics()
	}

	// Start validator in a goroutine
	errCh := make(chan error, 1)
	go func() {
		err := v.Attest(ctx, opts.maxRetries, opts.balanceThreshold, tracer)
		if err != nil {
			errCh <- err
		}
	}()

	// run upgrader tracker
	go trackLatestRelease(ctx, logger)

	// Wait for signal, context cancellation or error
	select {
	case <-signalCh:
		logger.Info("Received shutdown signal")

		return nil
	case <-ctx.Done():
		logger.Info("Context cancelled, shutting down")

		return nil
	case err := <-errCh:
		logger.Error("validator stopped with error", errorField(err))

		return err
	case err := <-metricsErrCh:
		logger.Error("metrics server failed, stopping the validator", errorField(err))

		return err
	}
}

func startValidator(
	ctx context.Context,
	signalCh <-chan os.Signal,
	logger *log.ZapLogger,
	newValidator func() (validator.Validator, error),
) (v validator.Validator, stopped bool, err error) {
	type result struct {
		v   validator.Validator
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		v, err := newValidator()
		resultCh <- result{v: v, err: err}
	}()

	select {
	case <-signalCh:
		logger.Info("Received shutdown signal")

		return validator.Validator{}, true, nil
	case <-ctx.Done():
		logger.Info("Context cancelled, shutting down")

		return validator.Validator{}, true, nil
	case res := <-resultCh:
		return res.v, false, res.err
	}
}

func errorField(err error) zap.Field {
	return zap.String("error", err.Error())
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()

	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func checkThresholdFlags(
	cmd *cobra.Command, config *configP.Config, balanceThreshold float64,
) error {
	if cmd.Flags().Changed("reward-claim-threshold") && config.Reward.ClaimThreshold == 0 {
		return errors.New("--reward-claim-threshold must be a positive number of STRK")
	}
	if math.IsNaN(balanceThreshold) || math.IsInf(balanceThreshold, 0) {
		return fmt.Errorf("--balance-threshold must be a finite number, got %v", balanceThreshold)
	}

	return nil
}

func checkMetricsAddress(ctx context.Context, host, port string) error {
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return fmt.Errorf("invalid --metrics-port %q, it must be a number from 1 to 65535", port)
	}

	address := net.JoinHostPort(host, port)
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("cannot listen on %s (--metrics-host, --metrics-port): %w", address, err)
	}

	return listener.Close()
}

func startMetricsServer(
	ctx context.Context, address, chainID string, logger *log.ZapLogger,
) (server *metrics.Metrics, failed <-chan error, stop func()) {
	server = metrics.NewMetrics(address, chainID, logger)

	errCh := make(chan error, 1)
	var startFailed atomic.Bool
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			startFailed.Store(true)
			errCh <- err
		}
	}()

	stop = func() {
		if startFailed.Load() {
			return
		}
		shutdownCtx, shutdownCancel := context.WithTimeout(
			ctx, 5*time.Second, //nolint:mnd // Timeout time
		)
		defer shutdownCancel()
		if err := server.Stop(shutdownCtx); err != nil {
			logger.Error("failed to stop metrics server", errorField(err))
		}
	}

	return server, errCh, stop
}

// Completes the config, which already holds the values from flags, with the
// ones from env vars and then from the config file. Afterwards it sets the
// defaults for the missing values and verifies the result
func loadConfig(config *configP.Config, configPath string) error {
	configFromFlags := *config
	configFromEnv, err := configP.FromEnv()
	if err != nil {
		return err
	}
	config.Fill(&configFromEnv)

	sources := []configP.Source{
		{Name: "flags", Config: &configFromFlags},
		{Name: "env vars", Config: &configFromEnv},
	}
	if configPath != "" {
		configFromFile, err := configP.FromFile(configPath)
		if err != nil {
			return err
		}
		config.Fill(&configFromFile)
		sources = append(sources, configP.Source{
			Name:   "config file " + configPath,
			Config: &configFromFile,
		})
	}
	config.SetDefaults()

	return config.CheckSources(sources...)
}

func main() {
	command := NewCommand()
	if err := command.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}
