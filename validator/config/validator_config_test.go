package config

import (
	"fmt"
	"math"
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
            },
            "reward": {
                "privateKey": "0x789",
                "address": "0xabc",
                "claimThreshold": 100,
                "claimMaxFee": 1000
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
			Reward: Reward{
				PrivKey:        "0x789",
				Address:        "0xabc",
				ClaimThreshold: 100,
				ClaimMaxFee:    1000,
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

	// Test Reward
	rewardURL := "hej"
	t.Setenv("REWARD_EXTERNAL_URL", rewardURL)
	rewardPrivateKey := "salut"
	t.Setenv("REWARD_PRIVATE_KEY", rewardPrivateKey)
	rewardAddress := "ahoj"
	t.Setenv("REWARD_ADDRESS", rewardAddress)
	t.Setenv("REWARD_CLAIM_THRESHOLD", "100")
	t.Setenv("REWARD_CLAIM_MAX_FEE", "1000")

	reward, err := RewardFromEnv()
	require.NoError(t, err)
	expectedReward := Reward{
		ExternalURL:    rewardURL,
		PrivKey:        rewardPrivateKey,
		Address:        rewardAddress,
		ClaimThreshold: 100,
		ClaimMaxFee:    1000,
	}
	require.Equal(
		t,
		expectedReward,
		reward,
	)

	// Test Config
	config, err := FromEnv()
	require.NoError(t, err)
	expectedConfig := Config{
		Provider: expectedProvider,
		Signer:   expectedSigner,
		Reward:   expectedReward,
	}
	require.Equal(t, expectedConfig, config)
}

func TestRewardFromEnvInvalidValues(t *testing.T) {
	const (
		thresholdEnv = "REWARD_CLAIM_THRESHOLD"
		maxFeeEnv    = "REWARD_CLAIM_MAX_FEE"
	)

	testCases := []struct {
		name   string
		envVar string
		value  string
	}{
		{name: "Non numeric claim threshold", envVar: thresholdEnv, value: "abc"},
		{name: "Negative claim threshold", envVar: thresholdEnv, value: "-1"},
		{name: "Decimal claim threshold", envVar: thresholdEnv, value: "1.5"},
		{name: "Non numeric claim max fee", envVar: maxFeeEnv, value: "abc"},
		{name: "Negative claim max fee", envVar: maxFeeEnv, value: "-1"},
		{name: "Decimal claim max fee", envVar: maxFeeEnv, value: "1.5"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.envVar, tc.value)

			_, err := RewardFromEnv()
			require.ErrorContains(t, err, tc.envVar)

			_, err = FromEnv()
			require.ErrorContains(t, err, tc.envVar)
		})
	}
}

func TestCorrectConfig(t *testing.T) {
	const (
		validProvider = `{
            "http": "http://localhost:1234",
            "ws": "ws://localhost:1235"
        }`
		validSigner = `{
            "url": "http://localhost:5678",
            "privateKey": "0x123",
            "operationalAddress": "0x456"
        }`
	)

	// Builds a config JSON. Empty sections default to valid values, except
	// reward which is omitted
	configJSON := func(provider, signer, reward string) []byte {
		if provider == "" {
			provider = validProvider
		}
		if signer == "" {
			signer = validSigner
		}
		if reward == "" {
			return fmt.Appendf(nil, `{"provider": %s, "signer": %s}`, provider, signer)
		}

		return fmt.Appendf(
			nil, `{"provider": %s, "signer": %s, "reward": %s}`, provider, signer, reward,
		)
	}

	testCases := []struct {
		name     string
		provider string
		signer   string
		reward   string
		errMsg   string
		verify   func(t *testing.T, config *Config)
	}{
		// Provider
		{
			name:     "With both provider http and ws urls",
			provider: `{"http": "http://localhost:1234", "ws": "ws://localhost:1235"}`,
		},
		{
			name:     "Missing provider http url",
			provider: `{"ws": "ws://localhost:1235"}`,
			errMsg:   "http provider url",
		},
		{
			name:     "Missing provider ws url",
			provider: `{"http": "http://localhost:1234"}`,
			errMsg:   "ws provider url",
		},
		// Signer
		{
			name:   "With private key and NO external signer URL",
			signer: `{"privateKey": "0x123", "operationalAddress": "0x456"}`,
		},
		{
			name:   "With external signer URL and NO private key",
			signer: `{"url": "http://localhost:5678", "operationalAddress": "0x456"}`,
		},
		{
			name:   "Missing operational address",
			signer: `{"url": "http://localhost:5678", "privateKey": "0x123"}`,
			errMsg: "operational address",
		},
		{
			name:   "Missing private key and external signer",
			signer: `{"operationalAddress": "0x456"}`,
			errMsg: "private key",
		},
		// Reward
		{
			name:   "Reward not set must be ignored",
			reward: `{}`,
		},
		{
			name:   "Reward with address, claim threshold and private key",
			reward: `{"address": "0x789", "privateKey": "0xabc", "claimThreshold": 10}`,
		},
		{
			name:   "Reward with address, claim threshold and external url",
			reward: `{"address": "0x789", "url": "http://localhost:9012", "claimThreshold": 10}`,
		},
		{
			name:   "Reward with claim max fee",
			reward: `{"address": "0x789", "privateKey": "0xabc", "claimThreshold": 10, "claimMaxFee": 1000}`,
		},
		{
			name:   "Reward missing address",
			reward: `{"privateKey": "0xabc"}`,
			errMsg: errRewardAddressNotSet.Error(),
		},
		{
			name:   "Reward with both private key and external url",
			reward: `{"address": "0x789", "privateKey": "0xabc", "url": "http://localhost:9012"}`,
			errMsg: errRewardBothKeysAndURL.Error(),
		},
		{
			name:   "Reward missing private key or external url",
			reward: `{"address": "0x789"}`,
			errMsg: errRewardNoKeysOrURL.Error(),
		},
		{
			name:   "Reward missing claim threshold",
			reward: `{"address": "0x789", "privateKey": "0xabc"}`,
			errMsg: errRewardClaimThresholdNotSet.Error(),
		},
		{
			name:   "Reward with only claim threshold",
			reward: `{"claimThreshold": 10}`,
			errMsg: errRewardAddressNotSet.Error(),
		},
		{
			name:   "Reward with only claim max fee",
			reward: `{"claimMaxFee": 1000}`,
			errMsg: errRewardAddressNotSet.Error(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config, err := FromData(configJSON(tc.provider, tc.signer, tc.reward))
			require.NoError(t, err)

			if tc.errMsg != "" {
				require.ErrorContains(t, config.Check(), tc.errMsg)

				return
			}
			require.NoError(t, config.Check())
			if tc.verify != nil {
				tc.verify(t, &config)
			}
		})
	}
}

func TestSignerExternal(t *testing.T) {
	const externalURL = "http://localhost:5678"

	t.Run("With external url", func(t *testing.T) {
		signer := Signer{ExternalURL: externalURL}
		require.True(t, signer.IsExternal())
	})

	t.Run("With private key only", func(t *testing.T) {
		signer := Signer{PrivKey: "0x123"}
		require.False(t, signer.IsExternal())
	})

	t.Run("External url takes priority over private key", func(t *testing.T) {
		signer := Signer{ExternalURL: externalURL, PrivKey: "0x123"}
		require.True(t, signer.IsExternal())
	})
}

func TestRewardExternal(t *testing.T) {
	t.Run("With external url", func(t *testing.T) {
		reward := Reward{ExternalURL: "http://localhost:9012"}
		require.True(t, reward.IsExternal())
	})

	t.Run("With private key only", func(t *testing.T) {
		reward := Reward{PrivKey: "0xabc"}
		require.False(t, reward.IsExternal())
	})
}

func TestRewardIsSet(t *testing.T) {
	t.Run("Empty reward", func(t *testing.T) {
		require.False(t, (&Reward{}).IsSet())
	})

	t.Run("With any field set", func(t *testing.T) {
		require.True(t, (&Reward{Address: "0x789"}).IsSet())
		require.True(t, (&Reward{PrivKey: "0xabc"}).IsSet())
		require.True(t, (&Reward{ExternalURL: "http://localhost:9012"}).IsSet())
		require.True(t, (&Reward{ClaimThreshold: 10}).IsSet())
		require.True(t, (&Reward{ClaimMaxFee: 1000}).IsSet())
	})
}

func TestRewardSetDefaults(t *testing.T) {
	t.Run("Reward not set stays empty", func(t *testing.T) {
		reward := Reward{}
		reward.SetDefaults()
		require.Equal(t, Reward{}, reward)
	})

	t.Run("Missing claim max fee defaults to max uint64", func(t *testing.T) {
		reward := Reward{Address: "0x789", PrivKey: "0xabc", ClaimThreshold: 10}
		reward.SetDefaults()
		require.Equal(t, uint64(math.MaxUint64), reward.ClaimMaxFee)
	})

	t.Run("Explicit claim max fee is kept", func(t *testing.T) {
		reward := Reward{Address: "0x789", PrivKey: "0xabc", ClaimThreshold: 10, ClaimMaxFee: 1000}
		reward.SetDefaults()
		require.Equal(t, uint64(1000), reward.ClaimMaxFee)
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
            },
            "reward": {
                "privateKey": "0x321",
                "claimThreshold": 10
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
            },
            "reward": {
                "privateKey": "0x888",
                "address": "0xabc",
                "claimThreshold": 99,
                "claimMaxFee": 1000
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
            },
            "reward": {
                "privateKey": "0x321",
                "address": "0xabc",
                "claimThreshold": 10,
                "claimMaxFee": 1000
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
