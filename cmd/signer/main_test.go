package main_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const processTimeout = 30 * time.Second

var signerBinary string

func TestMain(m *testing.M) {
	os.Exit(buildSignerAndRunTests(m))
}

func buildSignerAndRunTests(m *testing.M) int {
	buildDir, err := os.MkdirTemp("", "signer-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot create the signer build directory:", err)

		return 1
	}
	defer func() { _ = os.RemoveAll(buildDir) }()

	signerBinary = filepath.Join(buildDir, "signer")
	build := exec.CommandContext(context.Background(), "go", "build", "-o", signerBinary, ".")
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "cannot build the signer:", err)

		return 1
	}

	return m.Run()
}

func newMainProcess(
	t *testing.T, privKey string, args ...string,
) (cmd *exec.Cmd, output *syncBuffer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), processTimeout)
	t.Cleanup(cancel)
	if !slices.Contains(args, "--address") {
		args = append(args, "--address", "127.0.0.1:0")
	}
	cmd = exec.CommandContext(ctx, signerBinary, args...)
	cmd.Env = []string{}
	if privKey != "" {
		cmd.Env = append(cmd.Env, "SIGNER_PRIVATE_KEY="+privKey)
	}
	output = new(syncBuffer)
	cmd.Stdout = output
	cmd.Stderr = output

	return cmd, output
}

func runMain(t *testing.T, privKey string, args ...string) (output string, exitCode int) {
	t.Helper()

	cmd, outputBuffer := newMainProcess(
		t, privKey, append([]string{"--env", writeEnvFile(t, "")}, args...)...,
	)
	exitCode = processExitCode(t, cmd.Run())

	return outputBuffer.String(), exitCode
}

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()

	envFile := filepath.Join(t.TempDir(), "signer.env")
	require.NoError(t, os.WriteFile(envFile, []byte(content), 0o600))

	return envFile
}

func processExitCode(t *testing.T, err error) int {
	t.Helper()

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	require.NoError(t, err)

	return 0
}

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
			cmd, output := newMainProcess(
				t, "0x123", "--env", writeEnvFile(t, ""), "--address", "127.0.0.1:0",
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

func TestEnvFile(t *testing.T) {
	t.Run("Env file sets the private key", func(t *testing.T) {
		var listenConfig net.ListenConfig
		listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer func() { require.NoError(t, listener.Close()) }()

		envFile := writeEnvFile(t, "SIGNER_PRIVATE_KEY=0x123\n")
		cmd, output := newMainProcess(
			t, "", "--env", envFile, "--address", listener.Addr().String(),
		)

		require.Equal(t, 1, processExitCode(t, cmd.Run()))
		require.Contains(t, output.String(), "address already in use")
	})

	t.Run("Env file passed explicitly must exist", func(t *testing.T) {
		missingFile := filepath.Join(t.TempDir(), "missing.env")
		cmd, output := newMainProcess(t, "0x123", "--env", missingFile)

		require.Equal(t, 1, processExitCode(t, cmd.Run()))
		require.Contains(
			t, output.String(), "Error: cannot read the env var file: open "+missingFile,
		)
	})

	t.Run("Env file that isn't a dotenv file is an error without its content", func(t *testing.T) {
		const secret = "0xSECRETdummy"
		envFile := writeEnvFile(t, `{"SIGNER_PRIVATE_KEY": "`+secret+`"}`)
		cmd, output := newMainProcess(t, "", "--env", envFile)

		require.Equal(t, 1, processExitCode(t, cmd.Run()))
		require.Contains(
			t,
			output.String(),
			"Error: cannot parse the env var file "+envFile+": it must be a dotenv file",
		)
		require.NotContains(t, output.String(), secret)
	})

	t.Run("Missing private key", func(t *testing.T) {
		output, exitCode := runMain(t, "")

		require.Equal(t, 1, exitCode)
		require.Equal(
			t,
			"Error: SIGNER_PRIVATE_KEY env var is not set. Set it in the environment or in the "+
				"env var file (--env)\n",
			output,
		)
	})
}
