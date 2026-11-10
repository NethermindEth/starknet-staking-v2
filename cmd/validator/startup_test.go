package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	main "github.com/NethermindEth/starknet-staking-v2/cmd/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/stretchr/testify/require"
)

const (
	epochInfoEntryPoint  = "get_attestation_info_by_operational_address"
	stakerInfoEntryPoint = "get_staker_info_v1"
	supportedSpecVersion = "0.10.2"
	startupRetryDelay    = 3 * time.Second
	startupRetryLog      = "cannot start validator. Retrying in 3s"
)

var startupRetries atomic.Int64

func TestMain(m *testing.M) {
	validator.Sleep = func(delay time.Duration) {
		if delay == startupRetryDelay {
			startupRetries.Add(1)

			return
		}
		time.Sleep(delay)
	}
	os.Exit(m.Run())
}

type nodeFault func(method, entryPoint string) (status int, body string, ok bool)

func newFaultyNode(
	t *testing.T, specVersion string, fault nodeFault,
) (ctx context.Context, httpURL, wsURL string) {
	t.Helper()

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(t.Context())
	t.Cleanup(cancel)

	backend := validator.MockRPCServer(t, validator.MockRPC{
		Results: map[string]string{"starknet_specVersion": `"` + specVersion + `"`},
		CallResults: map[string]string{
			epochInfoEntryPoint:  `["0x789", "0x1", "0xa", "0x1", "0x1"]`,
			stakerInfoEntryPoint: `["0x0", "0x789", "0x456", "0x1", "0x0", "0x0"]`,
		},
		OnWSUpgrade: cancel,
	})
	backendURL, err := url.Parse(backend.URL)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(backendURL)

	selectorNames := map[felt.Felt]string{}
	for _, name := range []string{epochInfoEntryPoint, stakerInfoEntryPoint} {
		selectorNames[*snGoUtils.GetSelectorFromNameFelt(name)] = name
	}

	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		var entryPoint string
		if req.Method == "starknet_call" && len(req.Params) > 0 {
			var call struct {
				Selector felt.Felt `json:"entry_point_selector"`
			}
			if json.Unmarshal(req.Params[0], &call) == nil {
				entryPoint = selectorNames[call.Selector]
			}
		}

		if status, faultBody, ok := fault(req.Method, entryPoint); ok {
			if status == dropConnection {
				hijackAndClose(w)

				return
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(faultBody))

			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)

	return ctx, front.URL, "ws" + strings.TrimPrefix(backend.URL, "http")
}

func failFirst(times int, method, entryPoint string, status int, body string) nodeFault {
	var mu sync.Mutex
	hits := 0

	return func(reqMethod, reqEntryPoint string) (int, string, bool) {
		mu.Lock()
		defer mu.Unlock()

		otherRequest := reqMethod != method || (entryPoint != "" && reqEntryPoint != entryPoint)
		if otherRequest || hits >= times {
			return 0, "", false
		}
		hits++

		return status, body, true
	}
}

const dropConnection = -1

func hijackAndClose(w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := hijacker.Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func noFault(string, string) (status int, body string, ok bool) { return 0, "", false }

const internalErrorBody = `{"jsonrpc": "2.0", "id": 1, ` +
	`"error": {"code": -32603, "message": "Internal error"}}`

func validatorArgs(httpURL, wsURL, rewardAddress string, extraArgs ...string) []string {
	args := []string{
		"--provider-http", httpURL,
		"--provider-ws", wsURL,
		"--signer-op-address", "0x456",
		"--signer-priv-key", "0x123",
	}
	if rewardAddress != "" {
		args = append(args,
			"--reward-address", rewardAddress,
			"--reward-priv-key", "0xabc",
			"--reward-claim-threshold", "1",
		)
	}

	return append(args, extraArgs...)
}

func runValidator(
	t *testing.T, ctx context.Context, args []string,
) (retries int, logs string, err error) {
	t.Helper()

	logFile, err := os.CreateTemp(t.TempDir(), "validator-*.log")
	require.NoError(t, err)
	stderr := os.Stderr
	os.Stderr = logFile
	startupRetries.Store(0)

	command := main.NewCommand()
	command.SetArgs(args)
	runErr := command.ExecuteContext(ctx)

	os.Stderr = stderr
	retries = int(startupRetries.Load())
	output, err := os.ReadFile(logFile.Name())
	require.NoError(t, err)
	require.NoError(t, logFile.Close())

	return retries, string(output), runErr
}

func TestStartupRetries(t *testing.T) {
	t.Run("Unreachable provider is retried up to max retries", func(t *testing.T) {
		closed := httptest.NewServer(http.NotFoundHandler())
		closed.Close()

		retries, logs, err := runValidator(t, t.Context(), validatorArgs(
			closed.URL, "ws://localhost:1", "", "--max-retries", "2",
		))

		require.ErrorContains(t, err, "cannot start validator, no retries left (--max-retries 2)")
		require.ErrorContains(t, err, "cannot reach RPC provider at "+closed.URL+
			": connection refused")
		require.Equal(t, 2, retries)
		require.Equal(t, 2, strings.Count(logs, startupRetryLog))
		require.Contains(t, logs, `"retriesLeft": "2"`)
		require.Contains(t, logs, `"retriesLeft": "1"`)
	})

	t.Run("Provider failing temporarily", func(t *testing.T) {
		testCases := []struct {
			name  string
			fault nodeFault
		}{
			{
				name: "HTTP 503",
				fault: failFirst(
					2, "starknet_specVersion", "", http.StatusServiceUnavailable, "starting",
				),
			},
			{
				name: "chain id request",
				fault: failFirst(
					2, "starknet_chainId", "", http.StatusBadGateway, "bad gateway",
				),
			},
		}
		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				ctx, httpURL, wsURL := newFaultyNode(t, supportedSpecVersion, tc.fault)

				retries, logs, err := runValidator(t, ctx, validatorArgs(httpURL, wsURL, ""))

				require.NoError(t, err)
				require.Equal(t, 2, retries)
				require.Equal(t, 2, strings.Count(logs, startupRetryLog))
			})
		}
	})

	t.Run("Reward startup calls failing temporarily", func(t *testing.T) {
		for _, entryPoint := range []string{epochInfoEntryPoint, stakerInfoEntryPoint} {
			t.Run(entryPoint, func(t *testing.T) {
				ctx, httpURL, wsURL := newFaultyNode(t, supportedSpecVersion, failFirst(
					1, "starknet_call", entryPoint, http.StatusOK, internalErrorBody,
				))

				retries, logs, err := runValidator(t, ctx, validatorArgs(
					httpURL, wsURL, "0x789", "--max-retries", "5",
				))

				require.NoError(t, err)
				require.Equal(t, 1, retries)
				require.Equal(t, 1, strings.Count(logs, startupRetryLog))
			})
		}
	})

	t.Run("Provider URL credentials stay out of the logs", func(t *testing.T) {
		const apiKey = "0123456789abcdef0123456789abcdef"
		ctx, httpURL, wsURL := newFaultyNode(t, supportedSpecVersion, failFirst(
			1, "starknet_chainId", "", dropConnection, "",
		))

		retries, logs, err := runValidator(t, ctx, validatorArgs(
			httpURL+"/v3/"+apiKey+"?apikey=topsecret", wsURL, "", "--log-level", "debug",
		))

		require.NoError(t, err)
		require.Equal(t, 1, retries)
		require.Contains(t, logs, startupRetryLog)
		require.Contains(t, logs, "/v3/REDACTED?apikey=REDACTED")
		require.NotContains(t, logs, apiKey)
		require.NotContains(t, logs, "topsecret")
	})

	t.Run("Errors that are not transient are not retried", func(t *testing.T) {
		contractErrorBody := `{"jsonrpc": "2.0", "id": 1, ` +
			`"error": {"code": 40, "message": "Contract error", ` +
			`"data": {"revert_error": "Staker does not exist"}}}`

		testCases := []struct {
			name          string
			specVersion   string
			fault         nodeFault
			rewardAddress string
			expectedErr   string
		}{
			{
				name:        "unsupported spec version",
				specVersion: "0.9.0",
				fault:       noFault,
				expectedErr: "implements JSON-RPC specification 0.9.0",
			},
			{
				name:        "wrong URL path",
				specVersion: supportedSpecVersion,
				fault: failFirst(
					1, "starknet_specVersion", "", http.StatusNotFound, "404 page not found",
				),
				expectedErr: "HTTP 404 Not Found",
			},
			{
				name:        "contract error",
				specVersion: supportedSpecVersion,
				fault: failFirst(
					1, "starknet_call", epochInfoEntryPoint,
					http.StatusOK, contractErrorBody,
				),
				rewardAddress: "0x789",
				expectedErr:   "Staker does not exist",
			},
			{
				name:          "wrong reward address",
				specVersion:   supportedSpecVersion,
				fault:         noFault,
				rewardAddress: "0x999",
				expectedErr:   "reward address check",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				ctx, httpURL, wsURL := newFaultyNode(t, tc.specVersion, tc.fault)

				retries, logs, err := runValidator(
					t, ctx, validatorArgs(httpURL, wsURL, tc.rewardAddress),
				)

				require.ErrorContains(t, err, tc.expectedErr)
				require.True(t, strings.HasPrefix(err.Error(), "cannot start validator: "))
				require.Equal(t, 0, retries)
				require.NotContains(t, logs, startupRetryLog)
			})
		}
	})
}
