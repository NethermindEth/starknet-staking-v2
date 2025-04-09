package main_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	main "github.com/NethermindEth/starknet-staking-v2"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"lukechampine.com/uint128"
)

func TestLoadConfig(t *testing.T) {
	t.Run("Error when reading from file", func(t *testing.T) {
		config, err := main.LoadConfig("some non existing file name hopefully")

		require.Equal(t, main.Config{}, config)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("Error when unmarshalling file data", func(t *testing.T) {
		// Create a temporary file
		tmpFile, err := os.CreateTemp("", "config-*.json")
		require.NoError(t, err)

		// Remove temporary file at the end of test
		defer os.Remove(tmpFile.Name())

		// Invalid JSON content
		invalidJSON := `{"someField": 1,}` // Trailing comma makes it invalid

		// Write invalid JSON content to the file
		if _, err := tmpFile.Write([]byte(invalidJSON)); err != nil {
			require.NoError(t, err)
		}
		tmpFile.Close()

		config, err := main.LoadConfig(tmpFile.Name())

		require.Equal(t, main.Config{}, config)
		require.NotNil(t, err)
	})

	t.Run("Successfully load config", func(t *testing.T) {
		mockedConfig := main.Config{
			HttpProviderUrl:   "http://localhost:1234",
			WsProviderUrl:     "ws://localhost:1235",
			ExternalSignerUrl: "http://localhost:5678",
			AccountData: main.AccountData{
				PrivKey:            "0x123",
				OperationalAddress: main.AddressFromString("0x456"),
			},
		}

		tmpFilePath := writeMockConfigToTemporaryFile(t, mockedConfig)

		// Remove temporary file at the end of test
		defer os.Remove(tmpFilePath)

		config, err := main.LoadConfig(tmpFilePath)

		require.Equal(t, mockedConfig, config)
		require.Nil(t, err)
	})
}

func writeMockConfigToTemporaryFile(t *testing.T, config main.Config) string {
	t.Helper()

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "config-*.json")
	require.NoError(t, err)

	// Encode the mocked config to JSON and write to the file
	jsonData, err := json.Marshal(config)
	require.NoError(t, err)
	if _, err := tmpFile.Write(jsonData); err != nil {
		require.NoError(t, err)
	}
	tmpFile.Close()

	return tmpFile.Name()
}

func TestVerifyLoadedConfig(t *testing.T) {
	t.Run("Missing httpProviderUrl", func(t *testing.T) {
		config := main.Config{
			WsProviderUrl:     "ws://localhost:1235",
			ExternalSignerUrl: "http://localhost:5678",
			AccountData: main.AccountData{
				PrivKey:            "0x123",
				OperationalAddress: main.AddressFromString("0x456"),
			},
		}

		err := main.VerifyLoadedConfig(config, true, false)

		require.Equal(t, errors.New("you must specify the httpProviderUrl field in the config.json"), err)
	})

	t.Run("Missing wsProviderUrl", func(t *testing.T) {
		config := main.Config{
			HttpProviderUrl:   "http://localhost:1234",
			ExternalSignerUrl: "http://localhost:5678",
			AccountData: main.AccountData{
				PrivKey:            "0x123",
				OperationalAddress: main.AddressFromString("0x456"),
			},
		}

		err := main.VerifyLoadedConfig(config, true, false)

		require.Equal(t, errors.New("you must specify the wsProviderUrl field in the config.json"), err)
	})

	t.Run("Missing operationalAddress", func(t *testing.T) {
		config := main.Config{
			HttpProviderUrl:   "http://localhost:1234",
			WsProviderUrl:     "ws://localhost:1235",
			ExternalSignerUrl: "http://localhost:5678",
			AccountData: main.AccountData{
				PrivKey: "0x123",
			},
		}

		err := main.VerifyLoadedConfig(config, true, false)

		require.Equal(t, errors.New("you must specify the operationalAddress field in the config.json"), err)
	})

	t.Run("Both local and external signer flags are used", func(t *testing.T) {
		config := main.Config{
			HttpProviderUrl:   "http://localhost:1234",
			WsProviderUrl:     "ws://localhost:1235",
			ExternalSignerUrl: "http://localhost:5678",
			AccountData: main.AccountData{
				PrivKey:            "0x123",
				OperationalAddress: main.AddressFromString("0x456"),
			},
		}

		err := main.VerifyLoadedConfig(config, true, true)

		require.Equal(t, errors.New("you must specify exactly one of --local-signer or --external-signer"), err)
	})

	t.Run("Local signer flag is used but no private key is specified", func(t *testing.T) {
		config := main.Config{
			HttpProviderUrl:   "http://localhost:1234",
			WsProviderUrl:     "ws://localhost:1235",
			ExternalSignerUrl: "http://localhost:5678",
			AccountData: main.AccountData{
				OperationalAddress: main.AddressFromString("0x456"),
			},
		}

		err := main.VerifyLoadedConfig(config, true, false)

		require.Equal(t, errors.New("you must specify the privateKey field in the config.json when using --local-signer flag"), err)
	})

	t.Run("External signer flag is used but no external signer URL is specified", func(t *testing.T) {
		config := main.Config{
			HttpProviderUrl: "http://localhost:1234",
			WsProviderUrl:   "ws://localhost:1235",
			AccountData: main.AccountData{
				PrivKey:            "0x123",
				OperationalAddress: main.AddressFromString("0x456"),
			},
		}

		err := main.VerifyLoadedConfig(config, false, true)

		require.Equal(t, errors.New("you must specify the externalSignerUrl field in the config.json when using --external-signer flag"), err)
	})
}

func TestNewCommand(t *testing.T) {
	t.Run("Command contains expected fields", func(t *testing.T) {
		command := main.NewCommand()

		require.Equal(t, "validator", command.Use)
		require.Equal(
			t,
			"Program for Starknet validators to attest to epochs with respect to Staking v2",
			command.Short,
		)

		err := command.ValidateRequiredFlags()
		require.Equal(t, `required flag(s) "config" not set`, err.Error())

		// config needs to be a flag not an argument
		err = command.ValidateArgs([]string{"config"})
		require.Equal(t, `unknown command "config" for "validator"`, err.Error())
	})

	t.Run("PreRunE returns an error: cannot load config", func(t *testing.T) {
		command := main.NewCommand()

		command.SetArgs([]string{"--config", "some inexisting file name"})

		// Not ideal but a temporary file where to redirect stderr to avoid polluting the console with unwanted cli prints
		tmpFile, err := os.CreateTemp("", "test_output_")
		require.NoError(t, err)

		originalStderr := os.Stderr
		// Redirect stderr to the temporary file
		os.Stderr = tmpFile

		defer tmpFile.Close()
		defer os.Remove(tmpFile.Name())
		defer func() { os.Stderr = originalStderr }()

		err = command.ExecuteContext(context.Background())
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("PreRunE returns an error: config verification fails", func(t *testing.T) {
		command := main.NewCommand()

		mockedConfig := main.Config{
			HttpProviderUrl: "http://localhost:1234",
			WsProviderUrl:   "ws://localhost:1235",
			AccountData: main.AccountData{
				OperationalAddress: main.AddressFromString("0x456"),
			},
		}
		tmpFilePath := writeMockConfigToTemporaryFile(t, mockedConfig)

		// Remove temporary file at the end of test
		defer os.Remove(tmpFilePath)

		command.SetArgs([]string{"--config", tmpFilePath, "--external-signer"})

		// Not ideal but a temporary file where to redirect stderr to avoid polluting the console with unwanted cli prints
		tmpFile, err := os.CreateTemp("", "test_output_")
		require.NoError(t, err)

		originalStderr := os.Stderr
		// Redirect stderr to the temporary file
		os.Stderr = tmpFile

		defer tmpFile.Close()
		defer os.Remove(tmpFile.Name())
		defer func() { os.Stderr = originalStderr }()

		err = command.ExecuteContext(context.Background())
		require.Equal(t, errors.New("you must specify the externalSignerUrl field in the config.json when using --external-signer flag"), err)
	})

	t.Run("Full command setup works", func(t *testing.T) {
		command := main.NewCommand()

		mockedConfig := main.Config{
			HttpProviderUrl: "http://localhost:1234",
			WsProviderUrl:   "ws://localhost:1235",
			AccountData: main.AccountData{
				PrivKey:            "0x123",
				OperationalAddress: main.AddressFromString("0x456"),
			},
		}
		tmpFilePath := writeMockConfigToTemporaryFile(t, mockedConfig)

		// Remove temporary file at the end of test
		defer os.Remove(tmpFilePath)

		command.SetArgs([]string{"--config", tmpFilePath, "--local-signer"})

		// Not ideal but a temporary file where to redirect stderr to avoid polluting the console with unwanted cli prints
		tmpFile, err := os.CreateTemp("", "test_output_")
		require.NoError(t, err)

		originalStderr := os.Stderr
		// Redirect stderr to the temporary file
		os.Stderr = tmpFile

		defer tmpFile.Close()
		defer os.Remove(tmpFile.Name())
		defer func() { os.Stderr = originalStderr }()

		err = command.ExecuteContext(context.Background())
		require.Nil(t, err)
	})
}

func TestComputeBlockNumberToAttestTo(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)
	t.Run("Correct target block number computation - example 1", func(t *testing.T) {
		mockAccount.EXPECT().Address().Return(utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e"))
		epochInfo := main.EpochInfo{
			Stake:                     uint128.From64(1000000000000000000),
			EpochLen:                  40,
			EpochId:                   1516,
			CurrentEpochStartingBlock: main.BlockNumber(639270),
		}
		attestationWindow := uint64(16)

		blockNumber := main.ComputeBlockNumberToAttestTo(mockAccount, &epochInfo, attestationWindow)

		require.Equal(t, main.BlockNumber(639291), blockNumber)
	})

	t.Run("Correct target block number computation - example 2", func(t *testing.T) {
		test, err := new(felt.Felt).SetString("0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		require.NoError(t, err)
		mockAccount.EXPECT().Address().Return(test)

		epochInfo := main.EpochInfo{
			StakerAddress:             main.Address(*test),
			Stake:                     uint128.From64(1000000000000000000),
			EpochLen:                  40,
			EpochId:                   1517,
			CurrentEpochStartingBlock: main.BlockNumber(639310),
		}
		attestationWindow := uint64(16)

		blockNumber := main.ComputeBlockNumberToAttestTo(mockAccount, &epochInfo, attestationWindow)
		require.Equal(t, main.BlockNumber(639316), blockNumber)
	})

	t.Run("Correct target block number computation - example 3", func(t *testing.T) {
		mockAccount.EXPECT().Address().Return(utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e"))
		epochInfo := main.EpochInfo{
			Stake:                     uint128.From64(1000000000000000000),
			EpochLen:                  40,
			EpochId:                   1518,
			CurrentEpochStartingBlock: main.BlockNumber(639350),
		}
		attestationWindow := uint64(16)

		blockNumber := main.ComputeBlockNumberToAttestTo(mockAccount, &epochInfo, attestationWindow)

		require.Equal(t, main.BlockNumber(639369), blockNumber)
	})
}
