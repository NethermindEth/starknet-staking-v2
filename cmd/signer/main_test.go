package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

// Runs the signer's main function in a new process with the given private key and
// arguments. Returns its combined output and exit code
func runMain(t *testing.T, privKey string, args ...string) (output string, exitCode int) {
	t.Helper()

	// A missing env file, so only the env var sets the private key
	envFile := filepath.Join(t.TempDir(), "missing.env")
	cmd := exec.CommandContext(
		t.Context(), os.Args[0], append([]string{"--env", envFile}, args...)...,
	)
	cmd.Env = append(os.Environ(), runMainEnvVar+"=1", "SIGNER_PRIVATE_KEY="+privKey)
	var outputBuffer bytes.Buffer
	cmd.Stdout = &outputBuffer
	cmd.Stderr = &outputBuffer

	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return outputBuffer.String(), exitErr.ExitCode()
	}
	require.NoError(t, err)

	return outputBuffer.String(), 0
}

func TestMalformedPrivateKey(t *testing.T) {
	const malformedKey = "0xNOT-A-KEY-dummy"

	output, exitCode := runMain(t, malformedKey, "--address", "127.0.0.1:0")

	require.Equal(t, 1, exitCode)
	require.Equal(t, 1, strings.Count(output, "invalid private key"), output)
	require.Contains(t, output, "Error: invalid private key: it must be a 0x-prefixed")
	require.NotContains(t, output, malformedKey)
}
