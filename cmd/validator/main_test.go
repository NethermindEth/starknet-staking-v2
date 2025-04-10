package main_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	main "github.com/NethermindEth/starknet-staking-v2/cmd/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/stretchr/testify/require"
)

func TestNewCommand(t *testing.T) {
	t.Run("PreRunE returns an error: cannot load inexisting config", func(t *testing.T) {
		command := main.NewCommand()
		command.SetArgs([]string{"--config", "some inexisting file name"})

		err := command.ExecuteContext(context.Background())
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("PreRunE returns an error: config file verification fails", func(t *testing.T) {
		command := main.NewCommand()

		config := validator.Config{
			Provider: validator.Provider{
				Http: "http://localhost:1234",
				Ws:   "ws://localhost:1235",
			},
			Signer: validator.Signer{
				OperationalAddress: "0x456",
			},
		}
		filePath := createTemporaryConfigFile(t, &config)
		defer os.Remove(filePath)

		command.SetArgs([]string{"--config", filePath})

		err := command.ExecuteContext(context.Background())
		require.ErrorContains(t, err, "private key")
	})

	t.Run("Full command setup works with config file", func(t *testing.T) {
		command := main.NewCommand()

		config := validator.Config{
			Provider: validator.Provider{
				Http: "http://localhost:1234",
				Ws:   "ws://localhost:1235",
			},
			Signer: validator.Signer{
				OperationalAddress: "0x456",
				PrivKey:            "0x123",
			},
		}
		filePath := createTemporaryConfigFile(t, &config)
		defer os.Remove(filePath)

		command.SetArgs([]string{"--config", filePath})

		err := command.ExecuteContext(context.Background())
		require.Nil(t, err)
	})

	t.Run("Full command setup works with config through flags", func(t *testing.T) {
		command := main.NewCommand()
		command.SetArgs([]string{
			"--provider-http", "http://localhost:1234",
			"--provider-ws", "ws://localhost:1234",
			"--signer-op-address", "0x456",
			"--signer-url", "http://localhost:5555",
		})
		err := command.ExecuteContext(context.Background())
		require.NoError(t, err)
	})

	t.Run("Full command setup works with config file and with flags", func(t *testing.T) {
		config, err := validator.ConfigFromData([]byte(`{
            "provider": {
                "http": "http://localhost:1234"
            },
            "signer": {
                "url": "http://localhost:5678"
            }
        }`),
		)
		require.NoError(t, err)
		filePath := createTemporaryConfigFile(t, &config)
		defer os.Remove(filePath)

		command := main.NewCommand()
		command.SetArgs([]string{
			"--config", filePath,
			"--provider-ws", "ws://localhost:1234",
			"--signer-op-address", "0x456",
		})

		err = command.ExecuteContext(context.Background())
		require.NoError(t, err)
	})
	t.Run("Flags take priority over config file", func(t *testing.T) {
		config, err := validator.ConfigFromData([]byte(`{
            "provider": {
                "http": "http://localhost:1234",
                "ws": "ws://localhost:1235"
            },
            "signer": {
                "url": "http://localhost:5678",
                "operationalAddress": "0x456"
            }
        }`),
		)
		require.NoError(t, err)
		filePath := createTemporaryConfigFile(t, &config)
		defer os.Remove(filePath)

		command := main.NewCommand()
		command.SetArgs([]string{
			"--config", filePath,
			"--provider-http", "12",
		})

		err = command.ExecuteContext(context.Background())
		// Very hard to test with the current architecture
		// return in the future to fix it
		require.NoError(t, err)
	})
}

func createTemporaryConfigFile(t *testing.T, config *validator.Config) string {
	t.Helper()

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "config-*.json")
	require.NoError(t, err)

	// Encode the mocked config to JSON and write to the file
	jsonData, err := json.Marshal(config)
	require.NoError(t, err)
	_, err = tmpFile.Write(jsonData)
	require.NoError(t, err)
	tmpFile.Close()

	return tmpFile.Name()
}
