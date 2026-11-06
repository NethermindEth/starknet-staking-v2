package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/stretchr/testify/require"
)

const (
	epochInfoEntryPoint  = "get_attestation_info_by_operational_address"
	stakerInfoEntryPoint = "get_staker_info_v1"
	supportedSpecVersion = "0.10.2"
)

type nodeFault func(method, entryPoint string) (status int, body string, ok bool)

func newFaultyNode(t *testing.T, specVersion string, fault nodeFault) string {
	t.Helper()

	backend := validator.MockRPCServer(t, validator.MockRPC{
		Results: map[string]string{"starknet_specVersion": `"` + specVersion + `"`},
		CallResults: map[string]string{
			epochInfoEntryPoint:  `["0x789", "0x1", "0xa", "0x1", "0x1"]`,
			stakerInfoEntryPoint: `["0x0", "0x789", "0x456", "0x1", "0x0", "0x0"]`,
		},
		OnWSUpgrade: nil,
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

	return front.URL
}

func failFirst(times int, method, entryPoint string, status int, body string) nodeFault {
	var mu sync.Mutex
	hits := 0

	return func(reqMethod, reqEntryPoint string) (int, string, bool) {
		mu.Lock()
		defer mu.Unlock()

		if reqMethod != method || (entryPoint != "" && reqEntryPoint != entryPoint) ||
			hits >= times {
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

func countRetries(t *testing.T) *int {
	t.Helper()

	retries := new(int)
	sleep := validator.Sleep
	validator.Sleep = func(time.Duration) { *retries++ }
	t.Cleanup(func() { validator.Sleep = sleep })

	return retries
}

func retriesOf(t *testing.T, value string) types.Retries {
	t.Helper()

	retries, err := types.RetriesFromString(value)
	require.NoError(t, err)

	return retries
}

func newConfig(httpURL string, withReward bool) *config.Config {
	conf := config.Config{
		Provider: config.Provider{HTTP: httpURL, WS: "ws://localhost:1"},
		Signer:   config.Signer{OperationalAddress: "0x456", PrivKey: "0x123"},
	}
	if withReward {
		conf.Reward = config.Reward{Address: "0x789", PrivKey: "0xabc", ClaimThreshold: 1}
	}

	return &conf
}

func TestTryNewValidator(t *testing.T) {
	logger := log.NewNopZapLogger()

	t.Run("Unreachable provider is retried up to max retries", func(t *testing.T) {
		closed := httptest.NewServer(http.NotFoundHandler())
		closed.Close()
		retries := countRetries(t)

		_, err := tryNewValidator(
			t.Context(), newConfig(closed.URL, false), new(config.StarknetConfig),
			retriesOf(t, "2"), logger, false,
		)

		require.ErrorContains(t, err, "cannot start validator, no retries left (--max-retries 2)")
		require.ErrorContains(t, err, "cannot reach RPC provider at "+closed.URL+
			": connection refused")
		require.Equal(t, 2, *retries)
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
				retries := countRetries(t)
				nodeURL := newFaultyNode(t, supportedSpecVersion, tc.fault)

				_, err := tryNewValidator(
					t.Context(), newConfig(nodeURL, false), new(config.StarknetConfig),
					types.NewRetries(), logger, false,
				)

				require.NoError(t, err)
				require.Equal(t, 2, *retries)
			})
		}
	})

	t.Run("Reward startup calls failing temporarily", func(t *testing.T) {
		for _, entryPoint := range []string{epochInfoEntryPoint, stakerInfoEntryPoint} {
			t.Run(entryPoint, func(t *testing.T) {
				retries := countRetries(t)
				nodeURL := newFaultyNode(t, supportedSpecVersion, failFirst(
					1, "starknet_call", entryPoint, http.StatusOK, internalErrorBody,
				))

				_, err := tryNewValidator(
					t.Context(), newConfig(nodeURL, true), new(config.StarknetConfig),
					retriesOf(t, "5"), logger, false,
				)

				require.NoError(t, err)
				require.Equal(t, 1, *retries)
			})
		}
	})

	t.Run("Provider URL credentials stay out of the logs", func(t *testing.T) {
		const apiKey = "0123456789abcdef0123456789abcdef"
		retries := countRetries(t)
		nodeURL := newFaultyNode(t, supportedSpecVersion, failFirst(
			1, "starknet_chainId", "", dropConnection, "",
		))
		var buf bytes.Buffer
		bufLogger, err := log.NewZapLogger(
			log.NewLevel(log.DEBUG), log.WithWriter(&buf), log.WithColour(false),
		)
		require.NoError(t, err)

		_, err = tryNewValidator(
			t.Context(), newConfig(nodeURL+"/v3/"+apiKey+"?apikey=topsecret", false),
			new(config.StarknetConfig), types.NewRetries(), bufLogger, false,
		)

		require.NoError(t, err)
		require.Equal(t, 1, *retries)
		require.Contains(t, buf.String(), "cannot start validator. Retrying in 3s")
		require.Contains(t, buf.String(), "/v3/REDACTED?apikey=REDACTED")
		require.NotContains(t, buf.String(), apiKey)
		require.NotContains(t, buf.String(), "topsecret")
	})

	t.Run("Errors that are not transient are not retried", func(t *testing.T) {
		contractErrorBody := `{"jsonrpc": "2.0", "id": 1, ` +
			`"error": {"code": 40, "message": "Contract error", ` +
			`"data": {"revert_error": "Staker does not exist"}}}`

		testCases := []struct {
			name        string
			specVersion string
			fault       nodeFault
			withReward  bool
			rewardAddr  string
			expectedErr string
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
				withReward:  true,
				expectedErr: "Staker does not exist",
			},
			{
				name:        "wrong reward address",
				specVersion: supportedSpecVersion,
				fault:       noFault,
				withReward:  true,
				rewardAddr:  "0x999",
				expectedErr: "reward address check",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				retries := countRetries(t)
				nodeURL := newFaultyNode(t, tc.specVersion, tc.fault)
				conf := newConfig(nodeURL, tc.withReward)
				if tc.rewardAddr != "" {
					conf.Reward.Address = tc.rewardAddr
				}

				_, err := tryNewValidator(
					t.Context(), conf, new(config.StarknetConfig), types.NewRetries(), logger,
					false,
				)

				require.ErrorContains(t, err, tc.expectedErr)
				require.True(t, strings.HasPrefix(err.Error(), "cannot start validator: "))
				require.Equal(t, 0, *retries)
			})
		}
	})
}
