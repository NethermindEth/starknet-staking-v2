package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	main "github.com/NethermindEth/starknet-staking-v2/cmd/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

const (
	testProviderHTTP = "http://localhost:1234"
	testProviderWS   = "ws://localhost:1235"
	testSignerURL    = "http://localhost:5555"
	configFlag       = "--config"
)

func TestNewCommand(t *testing.T) {
	t.Run("PreRunE returns an error: cannot load inexisting config", func(t *testing.T) {
		command := main.NewCommand()
		command.SetArgs([]string{"--config", "some inexisting file name"})

		err := command.ExecuteContext(t.Context())
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("PreRunE returns an error: config file is not valid JSON", func(t *testing.T) {
		filePath := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(filePath, []byte(`{"provider": {}`), 0o600))

		command := main.NewCommand()
		command.SetArgs([]string{configFlag, filePath})

		err := command.ExecuteContext(t.Context())
		require.EqualError(
			t, err, "cannot parse config file "+filePath+": unexpected end of JSON input",
		)
	})

	t.Run("PreRunE returns an error naming the sources of a key and a URL", func(t *testing.T) {
		conf := config.Config{
			Provider: config.Provider{HTTP: testProviderHTTP, WS: testProviderWS},
			Signer: config.Signer{
				OperationalAddress: "0x456",
				ExternalURL:        testSignerURL,
			},
		}
		filePath := createTemporaryConfigFile(t, &conf)
		t.Setenv("SIGNER_PRIVATE_KEY", "0x123")

		command := main.NewCommand()
		command.SetArgs([]string{configFlag, filePath})

		err := command.ExecuteContext(t.Context())
		require.ErrorContains(
			t,
			err,
			"(private key set by env vars, external URL set by config file "+filePath+")",
		)
	})

	t.Run("PreRunE returns an error: unknown log level", func(t *testing.T) {
		command := main.NewCommand()
		command.SetArgs(validFlags(testProviderHTTP, testProviderWS, "--log-level", "verbose"))

		err := command.ExecuteContext(t.Context())
		require.EqualError(
			t, err, `unknown log level "verbose" (known: trace, debug, info, warn, error)`,
		)
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
		ctx, httpURL, wsURL := newTestNode(t)

		conf := config.Config{
			Provider: config.Provider{
				HTTP: httpURL,
				WS:   wsURL,
			},
			Signer: config.Signer{
				OperationalAddress: "0x456",
				PrivKey:            "0x123",
			},
		}
		filePath := createTemporaryConfigFile(t, &conf)
		defer deleteFile(t, filePath)

		command := newTestCommandWithArgs(t, "--config", filePath)
		err := command.ExecuteContext(ctx)
		require.NoError(t, err)
	})

	t.Run("Full command setup works with config through flags", func(t *testing.T) {
		ctx, httpURL, wsURL := newTestNode(t)

		command := newTestCommandWithArgs(
			t,
			"--provider-http", httpURL,
			"--provider-ws", wsURL,
			"--signer-op-address", "0x456",
			"--signer-url", "http://localhost:5555",
		)
		err := command.ExecuteContext(ctx)
		require.NoError(t, err)
	})

	t.Run("Full command setup works with config file and with flags", func(t *testing.T) {
		ctx, httpURL, wsURL := newTestNode(t)

		conf, err := config.FromData(fmt.Appendf(nil, `{
            "provider": {
                "http": %q
            },
            "signer": {
                "url": "http://localhost:5678"
            }
        }`, httpURL),
		)
		require.NoError(t, err)
		filePath := createTemporaryConfigFile(t, &conf)
		defer deleteFile(t, filePath)

		command := newTestCommandWithArgs(
			t,
			"--config", filePath,
			"--provider-ws", wsURL,
			"--signer-op-address", "0x456",
		)
		err = command.ExecuteContext(ctx)
		require.NoError(t, err)
	})
	t.Run("Priority order is flags -> env vars -> config file", func(t *testing.T) {
		ctx, httpURL, wsURL := newTestNode(t)

		// Configuration through file. Its http provider is unreachable, so the
		// command only succeeds if the flag value takes priority
		conf, err := config.FromData(fmt.Appendf(nil, `{
            "provider": {
                "http": "http://localhost:1234",
                "ws": %q
            },
            "signer": {
                "url": "http://localhost:5678",
                "operationalAddress": "0x456"
            }
        }`, wsURL),
		)
		require.NoError(t, err)
		filePath := createTemporaryConfigFile(t, &conf)
		defer deleteFile(t, filePath)

		// Configuration through env var
		t.Setenv("SIGNER_EXTERNAL_URL", "some other")

		command := newTestCommandWithArgs(
			t,
			"--config", filePath,
			"--provider-http", httpURL,
		)
		err = command.ExecuteContext(ctx)
		require.NoError(t, err)
	})

	// Any reward flag given, even `--reward-braavos-account=false`, requires the rest
	// of the reward configuration
	t.Run("Reward flags require the full reward configuration", func(t *testing.T) {
		const (
			errAddressNotSet = "address is not set in reward configuration"
			errNoKeyOrURL    = "neither private key nor external url set in reward configuration"

			rewardAddressFlag   = "--reward-address"
			rewardPrivKeyFlag   = "--reward-priv-key"
			rewardSignerURLFlag = "--reward-signer-url"
			rewardThresholdFlag = "--reward-claim-threshold"
			rewardMaxFeeFlag    = "--reward-claim-max-fee"
			rewardBraavosFlag   = "--reward-braavos-account"
		)

		// Returns the required reward flags followed by `optionalFlags`
		fullRewardFlags := func(optionalFlags ...string) []string {
			return append([]string{
				rewardAddressFlag, "0x789",
				rewardPrivKeyFlag, "0xabc",
				rewardThresholdFlag, "10",
			}, optionalFlags...)
		}

		testCases := []struct {
			name        string
			rewardFlags []string
			expectedErr string // empty if the configuration is valid
		}{
			{
				name:        "Only reward address",
				rewardFlags: []string{rewardAddressFlag, "0x789"},
				expectedErr: errNoKeyOrURL,
			},
			{
				name:        "Only reward private key",
				rewardFlags: []string{rewardPrivKeyFlag, "0xabc"},
				expectedErr: errAddressNotSet,
			},
			{
				name:        "Only reward signer url",
				rewardFlags: []string{rewardSignerURLFlag, "http://localhost:9012"},
				expectedErr: errAddressNotSet,
			},
			{
				name:        "Only reward claim threshold",
				rewardFlags: []string{rewardThresholdFlag, "10"},
				expectedErr: errAddressNotSet,
			},
			{
				name:        "Only reward claim max fee",
				rewardFlags: []string{rewardMaxFeeFlag, "1000"},
				expectedErr: errAddressNotSet,
			},
			{
				name:        "Only reward braavos account",
				rewardFlags: []string{rewardBraavosFlag},
				expectedErr: errAddressNotSet,
			},
			{
				name:        "Only reward braavos account explicitly false",
				rewardFlags: []string{rewardBraavosFlag + "=false"},
				expectedErr: errAddressNotSet,
			},
			{
				name:        "Full reward configuration, without optional fields",
				rewardFlags: fullRewardFlags(),
			},
			{
				name:        "Full reward configuration, with claim max fee",
				rewardFlags: fullRewardFlags(rewardMaxFeeFlag, "1000"),
			},
			{
				name:        "Full reward configuration, with braavos account",
				rewardFlags: fullRewardFlags(rewardBraavosFlag),
			},
			{
				name:        "Full reward configuration, with braavos account explicitly false",
				rewardFlags: fullRewardFlags(rewardBraavosFlag + "=false"),
			},
			{
				name:        "Full reward configuration",
				rewardFlags: fullRewardFlags(rewardMaxFeeFlag, "1000", rewardBraavosFlag),
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				ctx, httpURL, wsURL := newTestNode(t)

				command := newTestCommandWithArgs(
					t, validFlags(httpURL, wsURL, tc.rewardFlags...)...,
				)

				err := command.ExecuteContext(ctx)
				if tc.expectedErr == "" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, tc.expectedErr)
				}
			})
		}
	})
}

func TestHelpText(t *testing.T) {
	command := main.NewCommand()
	command.SetArgs([]string{"--help"})
	var output bytes.Buffer
	command.SetOut(&output)

	require.NoError(t, command.ExecuteContext(t.Context()))
	help := output.String()

	for i, line := range strings.Split(help, "\n") {
		require.Equal(
			t, strings.TrimRight(line, " \t"), line, "trailing whitespace on line %d", i+1,
		)
	}
	for _, defect := range []string{"the the", "Defaults values", "1 << 1e18", "Only applies for"} {
		require.NotContains(t, help, defect)
	}
	require.Contains(t, help, "Exclusive with --signer-url")
	require.Contains(t, help, "Exclusive with --signer-priv-key")
}

func validFlags(httpURL, wsURL string, extraFlags ...string) []string {
	return append([]string{
		"--provider-http", httpURL,
		"--provider-ws", wsURL,
		"--signer-op-address", "0x456",
		"--signer-url", testSignerURL,
	}, extraFlags...)
}

func TestMetricsFlags(t *testing.T) {
	t.Run("Invalid metrics port is rejected at startup", func(t *testing.T) {
		for _, port := range []string{"abc", "99999", "0", "-1", ""} {
			t.Run(port, func(t *testing.T) {
				command := newTestCommandWithArgs(
					t,
					validFlags(testProviderHTTP, testProviderWS,
						"--metrics", "--metrics-port", port)...,
				)
				err := command.ExecuteContext(t.Context())
				require.ErrorContains(t, err, fmt.Sprintf("invalid --metrics-port %q", port))
			})
		}
	})

	t.Run("Metrics port already in use is rejected at startup", func(t *testing.T) {
		var listenConfig net.ListenConfig
		listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer func() { require.NoError(t, listener.Close()) }()
		_, port, err := net.SplitHostPort(listener.Addr().String())
		require.NoError(t, err)

		command := newTestCommandWithArgs(
			t,
			validFlags(testProviderHTTP, testProviderWS,
				"--metrics", "--metrics-host", "127.0.0.1", "--metrics-port", port)...,
		)
		err = command.ExecuteContext(t.Context())
		require.ErrorContains(t, err, "cannot listen on 127.0.0.1:"+port)
		require.ErrorContains(t, err, "address already in use")
	})

	t.Run("Metrics port is not checked without --metrics", func(t *testing.T) {
		ctx, httpURL, wsURL := newTestNode(t)

		command := newTestCommandWithArgs(
			t, validFlags(httpURL, wsURL, "--metrics-port", "abc")...,
		)
		require.NoError(t, command.ExecuteContext(ctx))
	})

	t.Run("Metrics server on a free port starts", func(t *testing.T) {
		ctx, httpURL, wsURL := newTestNode(t)

		command := newTestCommandWithArgs(
			t,
			validFlags(httpURL, wsURL,
				"--metrics", "--metrics-host", "127.0.0.1", "--metrics-port", freePort(t))...,
		)
		require.NoError(t, command.ExecuteContext(ctx))
	})
}

func freePort(t *testing.T) string {
	t.Helper()

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	require.NoError(t, listener.Close())

	return port
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

// Starts a server answering the node calls the validator makes on startup, with the
// reward account "0x789" being the staker account. Once the validator subscribes to
// new block headers through the ws endpoint it is fully started, so the server cancels
// the returned context to stop it.
func newTestNode(t *testing.T) (ctx context.Context, httpURL, wsURL string) {
	t.Helper()

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(t.Context())

	server := validator.MockRPCServer(t, validator.MockRPC{
		Results: map[string]string{
			"starknet_getNonce":        `"0x0"`,
			"starknet_getBlockWithTxs": `{"block_hash": "0x1", "transactions": []}`,
		},
		CallResults: map[string]string{
			// staker address, stake, epoch length, epoch id and starting block
			"get_attestation_info_by_operational_address": `["0x789", "0x1", "0xa", "0x1", "0x1"]`,
			// Some(staker info) with the reward address, the operational address,
			// None unstake time, own amount and unclaimed rewards
			"get_staker_info_v1": `["0x0", "0x789", "0x456", "0x1", "0x0", "0x0"]`,
			"balance_of":         `["0x0", "0x0"]`,
		},
		OnWSUpgrade: cancel,
	})

	return ctx, server.URL, "ws" + strings.TrimPrefix(server.URL, "http")
}

const (
	runMainEnvVar  = "VALIDATOR_TEST_RUN_MAIN"
	processTimeout = 30 * time.Second
)

func newMainProcess(t *testing.T, args ...string) (cmd *exec.Cmd, output *bytes.Buffer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), processTimeout)
	t.Cleanup(cancel)
	cmd = exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Env = []string{runMainEnvVar + "=1"}
	output = new(bytes.Buffer)
	cmd.Stdout = output
	cmd.Stderr = output

	return cmd, output
}

func exitCode(t *testing.T, err error) int {
	t.Helper()

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	require.NoError(t, err)

	return 0
}

func TestFatalErrorIsLoggedWithoutStackTrace(t *testing.T) {
	server := validator.MockRPCServer(t, validator.MockRPC{})

	cmd, output := newMainProcess(t, validFlags(
		server.URL, "ws"+strings.TrimPrefix(server.URL, "http"), "--max-retries", "1",
	)...)

	require.Equal(t, 1, exitCode(t, cmd.Run()), output.String())
	require.Contains(t, output.String(), "validator stopped with error")
	require.NotContains(t, output.String(), "errorVerbose")
	require.NotContains(t, output.String(), "\n\t")
}

func TestStartMetricsServer(t *testing.T) {
	t.Run("A server that cannot listen reports the error", func(t *testing.T) {
		requested := make(chan struct{})
		release := make(chan struct{})
		var firstRequest sync.Once
		_, httpURL, _ := newFaultyNode(t, supportedSpecVersion,
			func(string, string) (int, string, bool) {
				firstRequest.Do(func() {
					close(requested)
					select {
					case <-release:
					case <-t.Context().Done():
					}
				})

				return 0, "", false
			},
		)
		port := freePort(t)

		cmd, output := newMainProcess(t, validFlags(httpURL, unresponsiveWSURL(t),
			"--metrics", "--metrics-host", "127.0.0.1", "--metrics-port", port)...)
		require.NoError(t, cmd.Start())

		select {
		case <-requested:
		case <-time.After(processTimeout):
			require.FailNow(t, "the validator didn't call the node")
		}
		var listenConfig net.ListenConfig
		listener, err := listenConfig.Listen(
			t.Context(), "tcp", net.JoinHostPort("127.0.0.1", port),
		)
		require.NoError(t, err)
		defer func() { require.NoError(t, listener.Close()) }()
		close(release)

		require.Equal(t, 1, exitCode(t, cmd.Wait()), output.String())
		require.Contains(t, output.String(), "metrics server failed, stopping the validator")
		require.Contains(t, output.String(), "address already in use")
		require.NotContains(t, output.String(), "Stopping metrics server")
	})

	t.Run("Stopping a running server reports no error", func(t *testing.T) {
		_, httpURL, _ := newTestNode(t)
		port := freePort(t)

		cmd, output := newMainProcess(t, validFlags(httpURL, unresponsiveWSURL(t),
			"--metrics", "--metrics-host", "127.0.0.1", "--metrics-port", port)...)
		require.NoError(t, cmd.Start())

		healthURL := "http://" + net.JoinHostPort("127.0.0.1", port) + "/health"
		health, err := http.NewRequestWithContext(
			t.Context(), http.MethodGet, healthURL, http.NoBody,
		)
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			resp, err := http.DefaultClient.Do(health)
			if err != nil {
				return false
			}

			return resp.Body.Close() == nil && resp.StatusCode == http.StatusOK
		}, processTimeout, 10*time.Millisecond)
		require.NoError(t, cmd.Process.Signal(syscall.SIGINT))

		require.Equal(t, 0, exitCode(t, cmd.Wait()), output.String())
		require.Equal(t, 1, strings.Count(output.String(), "Stopping metrics server"))
		require.NotContains(t, output.String(), "failed to stop metrics server")
	})
}

func unresponsiveWSURL(t *testing.T) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	return "ws" + strings.TrimPrefix(server.URL, "http")
}
