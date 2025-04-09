package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"

	"github.com/NethermindEth/juno/core/crypto"
	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/spf13/cobra"
)

type Provider struct {
	Http string `json:"http"`
	Ws   string `json:"ws"`
}

type Signer struct {
	ExternalUrl        string  `json:"url"`
	PrivKey            string  `json:"privateKey"`
	OperationalAddress Address `json:"operationalAddress"`
}

func (s *Signer) External() bool {
	return s.ExternalUrl != ""
}

type Config struct {
	Provider Provider `json:"provider"`
	Signer   Signer   `json:"signer"`
}

// Function to load and parse the JSON file
func ConfigFromFile(filePath string) (Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return Config{}, err
	}
	return ConfigFromData(data)
}

func ConfigFromData(data []byte) (Config, error) {
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, err
	}
	if err := checkConfig(&config); err != nil {
		return Config{}, err
	}

	return config, nil
}

func checkConfig(config *Config) error {
	if err := checkProvider(&config.Provider); err != nil {
		return err
	}
	if err := checkSigner(&config.Signer); err != nil {
		return err
	}
	return nil
}

func checkProvider(provider *Provider) error {
	if provider.Http == "" {
		return errors.New("http provider url not set in config")
	}
	if provider.Ws == "" {
		return errors.New("ws provider url not set in config")
	}
	return nil
}

func checkSigner(signer *Signer) error {
	if signer.OperationalAddress == Address(felt.Zero) {
		return errors.New("operational address is not set in config")
	}
	if signer.External() {
		return nil
	}
	if signer.PrivKey == "" {
		return errors.New("signer private key is not set in config")
	}
	return nil
}

func NewCommand() cobra.Command {
	var configPathF string
	var logLevelF string

	var config Config
	var logger utils.ZapLogger

	preRunE := func(cmd *cobra.Command, args []string) error {
		loadedConfig, err := ConfigFromFile(configPathF)
		if err != nil {
			return err
		}
		config = loadedConfig

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
		if err := Attest(&config, logger); err != nil {
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

	rootCmd.Flags().StringVarP(&configPathF, "config", "c", "", "Path to JSON config file")
	rootCmd.MarkFlagRequired("config")

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

func ComputeBlockNumberToAttestTo[Account Accounter](
	account Account, epochInfo *EpochInfo, attestWindow uint64,
) BlockNumber {
	accountAddress := account.Address()
	hash := crypto.PoseidonArray(
		new(felt.Felt).SetBigInt(epochInfo.Stake.Big()),
		new(felt.Felt).SetUint64(epochInfo.EpochId),
		accountAddress,
	)

	var hashBigInt *big.Int = new(big.Int)
	hashBigInt = hash.BigInt(hashBigInt)

	var blockOffsetBigInt *big.Int = new(big.Int)
	blockOffsetBigInt = blockOffsetBigInt.Mod(hashBigInt, big.NewInt(int64(epochInfo.EpochLen-attestWindow)))

	return BlockNumber(epochInfo.CurrentEpochStartingBlock.Uint64() + blockOffsetBigInt.Uint64())
}
