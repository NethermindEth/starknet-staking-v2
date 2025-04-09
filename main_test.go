package main_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	main "github.com/NethermindEth/starknet-staking-v2"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"lukechampine.com/uint128"
)

func TestLoadConfig(t *testing.T) {
	t.Run("Error when reading from file", func(t *testing.T) {
		config, err := main.ConfigFromFile("some non existing file name hopefully")

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

		config, err := main.ConfigFromFile(tmpFile.Name())

		require.Equal(t, main.Config{}, config)
		require.NotNil(t, err)
	})

	t.Run("Successfully load config", func(t *testing.T) {
		data := []byte(`{
            "provider": {
                "http": "http://localhost:1234",
                "ws": "ws://localhost:1235"
            },
            "signer": {
                "url": "http://localhost:5678",
                "privateKey": "0x123", 
                "operationalAddress": "0x456"
            }
        }`)
		config, err := main.ConfigFromData(data)
		require.NoError(t, err)
		require.NoError(t, config.Check())

		expectedConfig := main.Config{
			Provider: main.Provider{
				Http: "http://localhost:1234",
				Ws:   "ws://localhost:1235",
			},
			Signer: main.Signer{
				ExternalUrl:        "http://localhost:5678",
				PrivKey:            "0x123",
				OperationalAddress: "0x456",
			},
		}
		require.Equal(t, expectedConfig, config)
	})
}

func TestCorrectConfig(t *testing.T) {
	t.Run("Missing provider http url", func(t *testing.T) {
		data := []byte(`{
            "provider": {
                "ws": "ws://localhost:1235"
            },
            "signer": {
                "url": "http://localhost:5678",
                "privateKey": "0x123", 
                "operationalAddress": "0x456"
            }
        }`)
		config, err := main.ConfigFromData(data)
		require.NoError(t, err)
		require.ErrorContains(t, config.Check(), "http provider url")
	})

	t.Run("Missing provider ws url", func(t *testing.T) {
		data := []byte(`{
            "provider": {
                "http": "http://localhost:1234"
            },
            "signer": {
                "url": "http://localhost:5678",
                "privateKey": "0x123", 
                "operationalAddress": "0x456"
            }
        }`)
		config, err := main.ConfigFromData(data)
		require.NoError(t, err)
		require.ErrorContains(t, config.Check(), "ws provider url")
	})

	t.Run("Missing operational address", func(t *testing.T) {
		data := []byte(`{
            "provider": {
                "http": "http://localhost:1234",
                "ws": "ws://localhost:1235"
            },
            "signer": {
                "url": "http://localhost:5678",
                "privateKey": "0x123"
            }
        }`)
		config, err := main.ConfigFromData(data)
		require.NoError(t, err)
		require.ErrorContains(t, config.Check(), "operational address")
	})

	t.Run("Missing external signer url", func(t *testing.T) {
		data := []byte(`{
            "provider": {
                "http": "http://localhost:1234",
                "ws": "ws://localhost:1235"
            },
            "signer": {
                "privateKey": "0x123", 
                "operationalAddress": "0x456"
            }
        }`)
		config, err := main.ConfigFromData(data)
		require.NoError(t, err)
		require.NoError(t, config.Check())
		require.False(t, config.Signer.External())
	})

	t.Run("Missing private key", func(t *testing.T) {
		data := []byte(`{
            "provider": {
                "http": "http://localhost:1234",
                "ws": "ws://localhost:1235"
            },
            "signer": {
                "url": "http://localhost:5678",
                "operationalAddress": "0x456"
            }
        }`)
		config, err := main.ConfigFromData(data)
		require.NoError(t, err)
		require.NoError(t, config.Check())
		require.True(t, config.Signer.External())
	})

	t.Run("Missing private key and external signer", func(t *testing.T) {
		data := []byte(`{
            "provider": {
                "http": "http://localhost:1234",
                "ws": "ws://localhost:1235"
            },
            "signer": {
                "operationalAddress": "0x456"
            }
        }`)
		config, err := main.ConfigFromData(data)
		require.NoError(t, err)
		require.ErrorContains(t, config.Check(), "private key")
	})
}

func TestConfigFill(t *testing.T) {
	// Test data
	config1, err := main.ConfigFromData(
		[]byte(`{
            "provider": {
                "http": "http://localhost:1234"
            },
            "signer": {
                "privateKey": "0x123", 
                "operationalAddress": "0x456"
            }
        }`),
	)
	require.NoError(t, err)
	config2, err := main.ConfigFromData([]byte(`{
            "provider": {
                "http": "http://localhost:9999",
                "ws": "ws://localhost:1235"
            },
            "signer": {
                "url": "http://localhost:5678",
                "privateKey": "0x999"
            }
        }`),
	)
	require.NoError(t, err)

	// Expected values
	expectedConfig1, err := main.ConfigFromData(
		[]byte(`{
            "provider": {
                "http": "http://localhost:1234",
                "ws": "ws://localhost:1235"
            },
            "signer": {
                "url": "http://localhost:5678",
                "privateKey": "0x123", 
                "operationalAddress": "0x456"
            }
        }`),
	)
	require.NoError(t, err)
	expectedConfig2 := config2

	// Test
	config1.Fill(&config2)
	assert.Equal(t, expectedConfig1, config1)
	assert.Equal(t, expectedConfig2, config2)
}

func TestNewCommand(t *testing.T) {
	t.Run("PreRunE returns an error: cannot load inexisting config", func(t *testing.T) {
		command := main.NewCommand()
		command.SetArgs([]string{"--config", "some inexisting file name"})

		err := command.ExecuteContext(context.Background())
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("PreRunE returns an error: config file verification fails", func(t *testing.T) {
		command := main.NewCommand()

		config := main.Config{
			Provider: main.Provider{
				Http: "http://localhost:1234",
				Ws:   "ws://localhost:1235",
			},
			Signer: main.Signer{
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

		config := main.Config{
			Provider: main.Provider{
				Http: "http://localhost:1234",
				Ws:   "ws://localhost:1235",
			},
			Signer: main.Signer{
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
		config, err := main.ConfigFromData([]byte(`{
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
		config, err := main.ConfigFromData([]byte(`{
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

func createTemporaryConfigFile(t *testing.T, config *main.Config) string {
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
