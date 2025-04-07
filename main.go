package main

import (
	"context"
	"encoding/json"
	"math/big"
	"os"

	"github.com/NethermindEth/juno/core/crypto"
	"github.com/NethermindEth/juno/core/felt"
	"github.com/spf13/cobra"
)

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
	var configPath string
	var config Config

	preRunE := func(cmd *cobra.Command, args []string) error {
		loadedConfig, err := LoadConfig(configPath)
		if err != nil {
			return err
		}
		config = loadedConfig

		return nil
	}

	runE := func(cmd *cobra.Command, args []string) error {
		if err := Attest(config); err != nil {
			return err
		}
		return nil
	}

	var rootCmd = cobra.Command{
		Use:     "starknet-staking-v2",
		Short:   "Program for Starknet validators to attest to epochs with respect to Staking v2",
		PreRunE: preRunE,
		RunE:    runE,
		Args:    cobra.NoArgs,
	}

	// Add a flag for the config file path
	rootCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to JSON config file")
	rootCmd.MarkFlagRequired("config")

	return rootCmd
}

func main() {
	command := NewCommand()
	if err := command.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}

func ComputeBlockNumberToAttestTo[Account Accounter](account Account, attestationInfo EpochInfo, attestationWindow uint64) BlockNumber {
	accountAddress := account.Address()
	hash := crypto.PoseidonArray(
		new(felt.Felt).SetBigInt(attestationInfo.Stake.Big()),
		new(felt.Felt).SetUint64(attestationInfo.EpochId),
		accountAddress,
	)

	var hashBigInt *big.Int = new(big.Int)
	hashBigInt = hash.BigInt(hashBigInt)

	var blockOffsetBigInt *big.Int = new(big.Int)
	blockOffsetBigInt = blockOffsetBigInt.Mod(hashBigInt, big.NewInt(int64(attestationInfo.EpochLen-attestationWindow)))

	return BlockNumber(attestationInfo.CurrentEpochStartingBlock.Uint64() + blockOffsetBigInt.Uint64())
}
