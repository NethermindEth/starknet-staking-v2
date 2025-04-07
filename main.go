package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"

	"github.com/NethermindEth/juno/core/crypto"
	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/spf13/cobra"
)

// TODO(rdr): make fields required so that if they are not set then the tool signals it and fails
type AccountData struct {
	PrivKey            string `json:"privateKey"`
	OperationalAddress string `json:"operationalAddress"`
}

type Config struct {
	HttpProviderUrl string `json:"httpProviderUrl"`
	// TODO: should we have this additional url or do we parse the http one and create a ws out of it ?
	// I think having a 2nd one is more flexible
	WsProviderUrl string `json:"wsProviderUrl"`
	AccountData
}

// Function to load and parse the JSON file
func LoadConfig(filePath string) (Config, error) {
	file, err := os.ReadFile(filePath)
	if err != nil {
		return Config{}, err
	}

	var config Config
	err = json.Unmarshal(file, &config)
	if err != nil {
		return Config{}, err
	}

	return config, nil
}

func NewCommand() cobra.Command {
	var configPathF string
	var logLevelF string

	var config Config
	var logger utils.ZapLogger
	preRunE := func(cmd *cobra.Command, args []string) error {
		loadedConfig, err := LoadConfig(configPathF)
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
