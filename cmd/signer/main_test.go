package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const runMainEnvVar = "SIGNER_TEST_RUN_MAIN"

// When started by `runMain`, the test binary runs the signer's main function instead of
// the tests, so the tests can check its output and exit code
func TestMain(m *testing.M) {
	if os.Getenv(runMainEnvVar) != "" {
		os.Args = append([]string{"signer"}, os.Args[1:]...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Returns a command running the signer's main function in a new process with the given
// private key and arguments, and the buffer collecting its combined output
func newMainProcess(
	t *testing.T, privKey string, args ...string,
) (cmd *exec.Cmd, output *syncBuffer) {
	t.Helper()

	cmd = exec.CommandContext(t.Context(), os.Args[0], args...)
	// Only the variables running main and setting the key, so the test environment
	// can't change the result
	cmd.Env = []string{runMainEnvVar + "=1", "SIGNER_PRIVATE_KEY=" + privKey}
	output = new(syncBuffer)
	cmd.Stdout = output
	cmd.Stderr = output

	return cmd, output
}

// Runs the signer's main function in a new process with the given private key and
// arguments. Returns its combined output and exit code
func runMain(t *testing.T, privKey string, args ...string) (output string, exitCode int) {
	t.Helper()

	// A missing env file, so only the env var sets the private key
	envFile := filepath.Join(t.TempDir(), "missing.env")
	cmd, outputBuffer := newMainProcess(t, privKey, append([]string{"--env", envFile}, args...)...)
	exitCode = processExitCode(t, cmd.Run())

	return outputBuffer.String(), exitCode
}

// Returns the exit code of a process that ended with `err`
func processExitCode(t *testing.T, err error) int {
	t.Helper()

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	require.NoError(t, err)

	return 0
}

// A buffer safe to read while a process writes its output to it
type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buffer.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buffer.String()
}

func TestMalformedPrivateKey(t *testing.T) {
	const malformedKey = "0xNOT-A-KEY-dummy"

	output, exitCode := runMain(t, malformedKey, "--address", "127.0.0.1:0")

	require.Equal(t, 1, exitCode)
	require.Equal(t, 1, strings.Count(output, "invalid private key"), output)
	require.Contains(t, output, "Error: invalid private key: it must be a 0x-prefixed")
	require.NotContains(t, output, malformedKey)
}

func TestShutdownSignals(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			envFile := filepath.Join(t.TempDir(), "missing.env")
			cmd, output := newMainProcess(
				t, "0x123", "--env", envFile, "--address", "127.0.0.1:0",
			)
			require.NoError(t, cmd.Start())

			require.Eventually(t, func() bool {
				return strings.Contains(output.String(), "server running")
			}, 10*time.Second, 10*time.Millisecond, output.String())
			require.NoError(t, cmd.Process.Signal(signal))

			require.Equal(t, 0, processExitCode(t, cmd.Wait()), output.String())
			require.Contains(t, output.String(), "Shutting down the server")
		})
	}
}

func TestVersionFlag(t *testing.T) {
	output, exitCode := runMain(t, "", "--version")

	require.Equal(t, 0, exitCode)
	require.Equal(t, "signer version dev\n", output)
}
