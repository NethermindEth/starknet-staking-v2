package validator_test

import (
	"os"
	"testing"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"lukechampine.com/uint128"
)

func TestConfigFromFile(t *testing.T) {
	t.Run("Error when reading from file", func(t *testing.T) {
		config, err := validator.ConfigFromFile("some non existing file name hopefully")

		require.Equal(t, validator.Config{}, config)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("Error when unmarshalling file data", func(t *testing.T) {
		// Create a temporary file
		tmpFile, err := os.CreateTemp("", "config-*.json")
		require.NoError(t, err)

		// Remove temporary file at the end of test
		defer func() { require.NoError(t, os.Remove(tmpFile.Name())) }()

		// Invalid JSON content
		invalidJSON := `{"someField": 1,}` // Trailing comma makes it invalid

		// Write invalid JSON content to the file
		if _, err := tmpFile.Write([]byte(invalidJSON)); err != nil {
			require.NoError(t, err)
		}
		require.NoError(t, tmpFile.Close())

		config, err := validator.ConfigFromFile(tmpFile.Name())

		require.Equal(t, validator.Config{}, config)
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
		config, err := validator.ConfigFromData(data)
		require.NoError(t, err)
		require.NoError(t, config.Check())

		expectedConfig := validator.Config{
			Provider: validator.Provider{
				Http: "http://localhost:1234",
				Ws:   "ws://localhost:1235",
			},
			Signer: validator.Signer{
				ExternalUrl:        "http://localhost:5678",
				PrivKey:            "0x123",
				OperationalAddress: "0x456",
			},
		}
		require.Equal(t, expectedConfig, config)
	})
}

func TestConfigFromEnv(t *testing.T) {
	// Test Provider
	http := "hola"
	ws := "ola"

	require.NoError(t, os.Setenv("PROVIDER_HTTP_URL", http))
	defer func() {
		require.NoError(t, os.Unsetenv("PROVIDER_HTTP_URL"))
	}()
	require.NoError(t, os.Setenv("PROVIDER_WS_URL", ws))
	defer func() {
		require.NoError(t, os.Unsetenv("PROVIDER_WS_URL"))
	}()

	provider := validator.ProviderFromEnv()
	expectedProvider := validator.Provider{
		Http: http,
		Ws:   ws,
	}
	require.Equal(
		t,
		expectedProvider,
		provider,
	)

	// Test Signer
	url := "ciao"
	privateKey := "bonjour"
	operationalAddress := "hallo"

	require.NoError(t, os.Setenv("SIGNER_EXTERNAL_URL", url))
	defer func() {
		require.NoError(t, os.Unsetenv("SIGNER_EXTERNAL_URL"))
	}()
	require.NoError(t, os.Setenv("SIGNER_PRIVATE_KEY", privateKey))
	defer func() {
		require.NoError(t, os.Unsetenv("SIGNER_PRIVATE_KEY"))
	}()
	require.NoError(t, os.Setenv("SIGNER_OPERATIONAL_ADDRESS", operationalAddress))
	defer func() {
		require.NoError(t, os.Unsetenv("SIGNER_OPERATIONAL_ADDRESS"))
	}()

	signer := validator.SignerFromEnv()
	expectedSigner := validator.Signer{
		ExternalUrl:        url,
		PrivKey:            privateKey,
		OperationalAddress: operationalAddress,
	}
	require.Equal(
		t,
		expectedSigner,
		signer,
	)

	// Test Config
	config := validator.ConfigFromEnv()
	expectedConfig := validator.Config{
		Provider: expectedProvider,
		Signer:   expectedSigner,
	}
	require.Equal(t, expectedConfig, config)
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
		config, err := validator.ConfigFromData(data)
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
		config, err := validator.ConfigFromData(data)
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
		config, err := validator.ConfigFromData(data)
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
		config, err := validator.ConfigFromData(data)
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
		config, err := validator.ConfigFromData(data)
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
		config, err := validator.ConfigFromData(data)
		require.NoError(t, err)
		require.ErrorContains(t, config.Check(), "private key")
	})
}

func TestConfigFill(t *testing.T) {
	// Test data
	config1, err := validator.ConfigFromData(
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
	config2, err := validator.ConfigFromData([]byte(`{
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
	expectedConfig1, err := validator.ConfigFromData(
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

func TestComputeBlockNumberToAttestTo(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	t.Run("Correct target block number computation - example 1", func(t *testing.T) {
		stakerAddress := utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		epochInfo := validator.EpochInfo{
			StakerAddress:             validator.Address(*stakerAddress),
			Stake:                     uint128.From64(1000000000000000000),
			EpochLen:                  40,
			EpochId:                   1516,
			CurrentEpochStartingBlock: validator.BlockNumber(639270),
		}
		attestationWindow := uint64(16)

		blockNumber := validator.ComputeBlockNumberToAttestTo(&epochInfo, attestationWindow)

		require.Equal(t, validator.BlockNumber(639291), blockNumber)
	})

	t.Run("Correct target block number computation - example 2", func(t *testing.T) {
		stakerAddress := utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		epochInfo := validator.EpochInfo{
			StakerAddress:             validator.Address(*stakerAddress),
			Stake:                     uint128.From64(1000000000000000000),
			EpochLen:                  40,
			EpochId:                   1517,
			CurrentEpochStartingBlock: validator.BlockNumber(639310),
		}
		attestationWindow := uint64(16)

		blockNumber := validator.ComputeBlockNumberToAttestTo(&epochInfo, attestationWindow)
		require.Equal(t, validator.BlockNumber(639316), blockNumber)
	})

	t.Run("Correct target block number computation - example 3", func(t *testing.T) {
		stakerAddress := utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		epochInfo := validator.EpochInfo{
			StakerAddress:             validator.Address(*stakerAddress),
			Stake:                     uint128.From64(1000000000000000000),
			EpochLen:                  40,
			EpochId:                   1518,
			CurrentEpochStartingBlock: validator.BlockNumber(639350),
		}
		attestationWindow := uint64(16)

		blockNumber := validator.ComputeBlockNumberToAttestTo(&epochInfo, attestationWindow)

		require.Equal(t, validator.BlockNumber(639369), blockNumber)
	})
}
