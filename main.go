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

func isZero[T comparable](v T) bool {
	var x T
	return v == x
}

type Provider struct {
	Http string `json:"http"`
	Ws   string `json:"ws"`
}

// Merge it's missing fields with data from other provider
func (p *Provider) Fill(other *Provider) {
	if isZero(p.Http) {
		p.Http = other.Http
	}
	if isZero(p.Ws) {
		p.Ws = other.Ws
	}
}

type Signer struct {
	ExternalUrl        string `json:"url"`
	PrivKey            string `json:"privateKey"`
	OperationalAddress string `json:"operationalAddress"`
}

// Merge it's missing fields with data from other signer
func (s *Signer) Fill(other *Signer) {
	if isZero(s.ExternalUrl) {
		s.ExternalUrl = other.ExternalUrl
	}
	if isZero(s.PrivKey) {
		s.PrivKey = other.PrivKey
	}
	if isZero(s.OperationalAddress) {
		s.OperationalAddress = other.OperationalAddress
	}
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
	return config, nil
}

// Fills it's missing fields with data from other config
func (c *Config) Fill(other *Config) {
	c.Provider.Fill(&other.Provider)
	c.Signer.Fill(&other.Signer)
}

// Verifies it's data is appropiatly set
func (c *Config) Check() error {
	if err := checkProvider(&c.Provider); err != nil {
		return err
	}
	if err := checkSigner(&c.Signer); err != nil {
		return err
	}
	return nil
}

func checkProvider(provider *Provider) error {
	if provider.Http == "" {
		return errors.New("http provider url not set in provider config")
	}
	if provider.Ws == "" {
		return errors.New("ws provider url not set in provder config")
	}
	return nil
}

func checkSigner(signer *Signer) error {
	if signer.OperationalAddress == "" {
		return errors.New("operational address is not set in signer config")
	}
	if signer.External() {
		return nil
	}
	if signer.PrivKey == "" {
		return errors.New("neither private key nor url properties set in signer config")
	}
	return nil
}

func NewCommand() cobra.Command {
	var configPath string
	var logLevelF string

	var config Config
	var logger utils.ZapLogger

	preRunE := func(cmd *cobra.Command, args []string) error {
		if configPath != "" {
			fileConfig, err := ConfigFromFile(configPath)
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
