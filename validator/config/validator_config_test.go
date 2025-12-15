package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromFile(t *testing.T) {
	t.Run("Error when reading from file", func(t *testing.T) {
		config, err := FromFile("some non existing file name hopefully")

		require.Equal(t, Config{}, config)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("Error when unmarshalling file data", func(t *testing.T) {
		// Create a temporary file
		tmpFile, err := os.CreateTemp(t.TempDir(), "config-*.json")
		require.NoError(t, err)

		// Remove temporary file at the end of test
		defer func() { require.NoError(t, os.Remove(tmpFile.Name())) }()

		// Invalid JSON content
		invalidJSON := `{"someField": 1,}` // Trailing comma makes it invalid

		// Write invalid JSON content to the file
		_, err = tmpFile.WriteString(invalidJSON)
		require.NoError(t, err)
		require.NoError(t, tmpFile.Close())

		config, err := FromFile(tmpFile.Name())

		require.Equal(t, Config{}, config)
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
		config, err := FromData(data)
		require.NoError(t, err)
		require.NoError(t, config.Check())

		expectedConfig := Config{
			Provider: Provider{
				HTTP: "http://localhost:1234",
				WS:   "ws://localhost:1235",
			},
			Signer: Signer{
				ExternalURL:        "http://localhost:5678",
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
	t.Setenv("PROVIDER_HTTP_URL", http)
	ws := "ola"
	t.Setenv("PROVIDER_WS_URL", ws)

	provider := ProviderFromEnv()
	expectedProvider := Provider{
		HTTP: http,
		WS:   ws,
	}
	require.Equal(
		t,
		expectedProvider,
		provider,
	)

	// Test Signer
	url := "ciao"
	t.Setenv("SIGNER_EXTERNAL_URL", url)
	privateKey := "bonjour"
	t.Setenv("SIGNER_PRIVATE_KEY", privateKey)
	operationalAddress := "hallo"
	t.Setenv("SIGNER_OPERATIONAL_ADDRESS", operationalAddress)

	signer := SignerFromEnv()
	expectedSigner := Signer{
		ExternalURL:        url,
		PrivKey:            privateKey,
		OperationalAddress: operationalAddress,
	}
	require.Equal(
		t,
		expectedSigner,
		signer,
	)

	// Test Config
	config := FromEnv()
	expectedConfig := Config{
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
		config, err := FromData(data)
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
		config, err := FromData(data)
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
		config, err := FromData(data)
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
		config, err := FromData(data)
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
		config, err := FromData(data)
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
		config, err := FromData(data)
		require.NoError(t, err)
		require.ErrorContains(t, config.Check(), "private key")
	})
}

func TestConfigFill(t *testing.T) {
	// Test data
	config1, err := FromData(
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
	config2, err := FromData([]byte(`{
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
	expectedConfig1, err := FromData(
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
