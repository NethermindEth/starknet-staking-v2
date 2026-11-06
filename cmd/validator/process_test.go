package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/stretchr/testify/require"
)

const (
	runMainEnvVar  = "VALIDATOR_TEST_RUN_MAIN"
	processTimeout = 30 * time.Second
)

// When started by `newMainProcess`, the test binary runs the validator's main function
// instead of the tests, so the tests can check its output, exit code and signal handling
func TestMain(m *testing.M) {
	if os.Getenv(runMainEnvVar) != "" {
		// No waiting between retries
		validator.Sleep = func(time.Duration) {}
		os.Args = append([]string{"validator"}, os.Args[1:]...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Returns a command running the validator's main function in a new process with the given
// arguments, and the buffer collecting its combined output
func newMainProcess(t *testing.T, args ...string) (cmd *exec.Cmd, output *bytes.Buffer) {
	t.Helper()

	// Killed after a while, so a validator that keeps running can't hang the tests
	ctx, cancel := context.WithTimeout(t.Context(), processTimeout)
	t.Cleanup(cancel)
	cmd = exec.CommandContext(ctx, os.Args[0], args...)
	// Only the variable running main, so no config comes from the test environment
	cmd.Env = []string{runMainEnvVar + "=1"}
	output = new(bytes.Buffer)
	cmd.Stdout = output
	cmd.Stderr = output

	return cmd, output
}

// Returns the exit code of a process that ended with `err`
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
	// Answers the startup calls and refuses the websocket upgrade, so the validator stops
	// with an error once its retries are exhausted
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
