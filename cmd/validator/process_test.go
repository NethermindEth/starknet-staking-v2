package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/stretchr/testify/require"
)

const (
	runMainEnvVar  = "VALIDATOR_TEST_RUN_MAIN"
	processTimeout = 30 * time.Second
)

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnvVar) != "" {
		validator.Sleep = func(time.Duration) {}
		os.Args = append([]string{"validator"}, os.Args[1:]...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

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

	cmd, output := newMainProcess(
		t,
		"--provider-http", server.URL,
		"--provider-ws", "ws"+strings.TrimPrefix(server.URL, "http"),
		"--signer-op-address", "0x456",
		"--signer-url", "http://localhost:5555",
		"--max-retries", "1",
	)

	require.Equal(t, 1, exitCode(t, cmd.Run()), output.String())
	require.Contains(t, output.String(), "validator stopped with error")
	require.NotContains(t, output.String(), "errorVerbose")
}

func TestShutdownSignalDuringStartup(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			requested := make(chan struct{}, 1)
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					select {
					case requested <- struct{}{}:
					default:
					}
					select {
					case <-release:
					case <-r.Context().Done():
					}
				},
			))
			t.Cleanup(server.Close)
			t.Cleanup(func() { close(release) })

			cmd, output := newMainProcess(
				t,
				"--provider-http", server.URL,
				"--provider-ws", "ws://127.0.0.1:1",
				"--signer-op-address", "0x456",
				"--signer-url", "http://localhost:5555",
			)
			require.NoError(t, cmd.Start())

			select {
			case <-requested:
			case <-time.After(processTimeout):
				require.FailNow(t, "the validator didn't call the node")
			}
			require.NoError(t, cmd.Process.Signal(signal))

			require.Equal(t, 0, exitCode(t, cmd.Wait()), output.String())
			require.Contains(t, output.String(), "Received shutdown signal")
		})
	}
}
