package main_test

import (
	"encoding/json"
	"os"
	"testing"

	main "github.com/NethermindEth/starknet-staking-v2/cmd/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

const testProviderHTTP = "http://localhost:1234"

func TestNewCommand(t *testing.T) {
	t.Run("PreRunE returns an error: cannot load inexisting config", func(t *testing.T) {
		command := main.NewCommand()
		command.SetArgs([]string{"--config", "some inexisting file name"})

		err := command.ExecuteContext(t.Context())
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("PreRunE returns an error: config file verification fails", func(t *testing.T) {
		command := main.NewCommand()

		conf := config.Config{
			Provider: config.Provider{
				HTTP: testProviderHTTP,
				WS:   "ws://localhost:1235",
			},
			Signer: config.Signer{
				OperationalAddress: "0x456",
			},
		}
		filePath := createTemporaryConfigFile(t, &conf)
		defer deleteFile(t, filePath)

		command.SetArgs([]string{"--config", filePath})

		err := command.ExecuteContext(t.Context())
		require.ErrorContains(t, err, "private key")
	})

	t.Run("Full command setup works with config file", func(t *testing.T) {
		conf := config.Config{
			Provider: config.Provider{
				HTTP: testProviderHTTP,
				WS:   "ws://localhost:1235",
			},
			Signer: config.Signer{
				OperationalAddress: "0x456",
				PrivKey:            "0x123",
			},
		}
		filePath := createTemporaryConfigFile(t, &conf)
		defer deleteFile(t, filePath)

		command := newTestCommandWithArgs(t, "--config", filePath)
		err := command.ExecuteContext(t.Context())
		require.Nil(t, err)
	})

	t.Run("Full command setup works with config through flags", func(t *testing.T) {
		command := newTestCommandWithArgs(
			t,
			"--provider-http", testProviderHTTP,
			"--provider-ws", "ws://localhost:1234",
			"--signer-op-address", "0x456",
			"--signer-url", "http://localhost:5555",
		)
		err := command.ExecuteContext(t.Context())
		require.NoError(t, err)
	})

	t.Run("Full command setup works with config file and with flags", func(t *testing.T) {
		conf, err := config.FromData([]byte(`{
            "provider": {
                "http": "http://localhost:1234"
            },
            "signer": {
                "url": "http://localhost:5678"
            }
        }`),
		)
		require.NoError(t, err)
		filePath := createTemporaryConfigFile(t, &conf)
		defer deleteFile(t, filePath)

		command := newTestCommandWithArgs(
			t,
			"--config", filePath,
			"--provider-ws", "ws://localhost:1234",
			"--signer-op-address", "0x456",
		)
		err = command.ExecuteContext(t.Context())
		require.NoError(t, err)
	})
	t.Run("Priority order is flags -> env vars -> config file", func(t *testing.T) {
		// Configuration through file
		conf, err := config.FromData([]byte(`{
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
		filePath := createTemporaryConfigFile(t, &conf)
		defer deleteFile(t, filePath)

		// Configuration through env var
		t.Setenv("SIGNER_EXTERNAL_URL", "some other")

		command := newTestCommandWithArgs(
			t,
			"--config", filePath,
			"--provider-http", "12",
		)
		err = command.ExecuteContext(t.Context())
		// Very hard to test with the current architecture
		// return in the future to fix it
		require.NoError(t, err)
	})

	// Any reward flag given, even `--reward-braavos-account=false`, requires the rest
	// of the reward configuration
	t.Run("Reward flags require the full reward configuration", func(t *testing.T) {
		const (
			errAddressNotSet = "address is not set in reward configuration"
			errNoKeyOrURL    = "neither private key nor external url set in reward configuration"
		)

		testCases := []struct {
			name        string
			rewardFlags []string
			expectedErr string // empty if the configuration is valid
		}{
			{
				name:        "Only reward address",
				rewardFlags: []string{"--reward-address", "0x789"},
				expectedErr: errNoKeyOrURL,
			},
			{
				name:        "Only reward private key",
				rewardFlags: []string{"--reward-priv-key", "0xabc"},
				expectedErr: errAddressNotSet,
			},
			{
				name:        "Only reward signer url",
				rewardFlags: []string{"--reward-signer-url", "http://localhost:9012"},
				expectedErr: errAddressNotSet,
			},
			{
				name:        "Only reward claim threshold",
				rewardFlags: []string{"--reward-claim-threshold", "10"},
				expectedErr: errAddressNotSet,
			},
			{
				name:        "Only reward claim max fee",
				rewardFlags: []string{"--reward-claim-max-fee", "1000"},
				expectedErr: errAddressNotSet,
			},
			{
				name:        "Only reward braavos account",
				rewardFlags: []string{"--reward-braavos-account"},
				expectedErr: errAddressNotSet,
			},
			{
				name:        "Only reward braavos account explicitly false",
				rewardFlags: []string{"--reward-braavos-account=false"},
				expectedErr: errAddressNotSet,
			},
			{
				name: "Full reward configuration, without optional fields",
				rewardFlags: []string{
					"--reward-address", "0x789",
					"--reward-priv-key", "0xabc",
					"--reward-claim-threshold", "10",
				},
			},
			{
				name: "Full reward configuration, with claim max fee",
				rewardFlags: []string{
					"--reward-address", "0x789",
					"--reward-priv-key", "0xabc",
					"--reward-claim-threshold", "10",
					"--reward-claim-max-fee", "1000",
				},
			},
			{
				name: "Full reward configuration, with braavos account",
				rewardFlags: []string{
					"--reward-address", "0x789",
					"--reward-priv-key", "0xabc",
					"--reward-claim-threshold", "10",
					"--reward-braavos-account",
				},
			},
			{
				name: "Full reward configuration, with braavos account explicitly false",
				rewardFlags: []string{
					"--reward-address", "0x789",
					"--reward-priv-key", "0xabc",
					"--reward-claim-threshold", "10",
					"--reward-braavos-account=false",
				},
			},
			{
				name: "Full reward configuration",
				rewardFlags: []string{
					"--reward-address", "0x789",
					"--reward-priv-key", "0xabc",
					"--reward-claim-threshold", "10",
					"--reward-claim-max-fee", "1000",
					"--reward-braavos-account",
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				args := []string{
					"--provider-http", testProviderHTTP,
					"--provider-ws", "ws://localhost:1234",
					"--signer-op-address", "0x456",
					"--signer-url", "http://localhost:5555",
				}
				command := newTestCommandWithArgs(t, append(args, tc.rewardFlags...)...)

				err := command.ExecuteContext(t.Context())
				if tc.expectedErr == "" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, tc.expectedErr)
				}
			})
		}
	})
}

func createTemporaryConfigFile(t *testing.T, conf *config.Config) string {
	t.Helper()

	// Create a temporary file
	tmpFile, err := os.CreateTemp(t.TempDir(), "config-*.json")
	require.NoError(t, err)

	// Encode the mocked config to JSON and write to the file
	jsonData, err := json.Marshal(conf)
	require.NoError(t, err)
	_, err = tmpFile.Write(jsonData)
	require.NoError(t, err)
	err = tmpFile.Close()
	require.NoError(t, err)

	return tmpFile.Name()
}

func deleteFile(t *testing.T, filePath string) {
	t.Helper()
	require.NoError(t, os.Remove(filePath))
}

// Creates a new command with max retries set to 1
func newTestCommandWithArgs(t *testing.T, args ...string) cobra.Command {
	t.Helper()

	args = append(args, "--max-retries", "1")
	cmd := main.NewCommand()
	cmd.SetArgs(args)

	return cmd
}
