package validator_test

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func mockNodeWithSpecVersion(t *testing.T, specVersion string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, err := w.Write(
				[]byte(`{"jsonrpc": "2.0", "result": "` + specVersion + `", "id": 1}`),
			)
			require.NoError(t, err)
		}),
	)
	t.Cleanup(server.Close)

	return server
}

func mockNodeWithStatus(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, err := w.Write([]byte(body))
			require.NoError(t, err)
		}),
	)
	t.Cleanup(server.Close)

	return server
}

func notTLSServer(t *testing.T) string {
	t.Helper()

	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_, _ = conn.Write([]byte("not a TLS server\n"))
			_ = conn.Close()
		}
	}()

	return listener.Addr().String()
}

func mockNodeWithBody(t *testing.T, body string) *httptest.Server {
	t.Helper()

	return mockNodeWithStatus(t, http.StatusOK, body)
}

func TestNewProvider(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	logger := log.NewNopZapLogger()

	t.Run("Error creating provider", func(t *testing.T) {
		providerURL := "wrong url"

		provider, err := validator.NewProvider(t.Context(), providerURL, logger)

		require.Nil(t, provider)
		expectedErrorMsg := "cannot create RPC provider at " + providerURL
		require.ErrorContains(t, err, expectedErrorMsg)
	})

	t.Run("Supported spec version", func(t *testing.T) {
		for _, specVersion := range []string{
			"0.10.2", "0.10.3", "v0.10.3", "0.10.4",
			"0.10.2+juno", "0.10.3+juno", "v0.10.4+build.1",
			"0.10.3-rc.0", "0.10.3-rc1", "0.10.2-rc1", "v0.10.4-rc.2+build.1",
		} {
			t.Run(specVersion, func(t *testing.T) {
				server := mockNodeWithSpecVersion(t, specVersion)

				provider, err := validator.NewProvider(t.Context(), server.URL, logger)

				require.NoError(t, err)
				require.NotNil(t, provider)
			})
		}
	})

	t.Run("Node serving another RPC version", func(t *testing.T) {
		const v09SpecVersion = "0.9.0"
		apiKey := strings.Repeat("a1", 16)
		testCases := []struct {
			name        string
			specVersion string
			path        string
			expectedErr string
		}{
			{
				name:        "v0.9 at the root path",
				specVersion: v09SpecVersion,
				expectedErr: "the node at %[1]s serves RPC v0.9, but the validator needs " +
					"RPC v0.10: add /rpc/v0_10 to the end of both provider URLs (HTTP and WS), " +
					"e.g. %[1]s/rpc/v0_10",
			},
			{
				name:        "v0.9 at the root path with a trailing slash",
				specVersion: v09SpecVersion,
				path:        "/",
				expectedErr: "the node at %[1]s/ serves RPC v0.9, but the validator needs " +
					"RPC v0.10: add /rpc/v0_10 to the end of both provider URLs (HTTP and WS), " +
					"e.g. %[1]s/rpc/v0_10",
			},
			{
				name:        "v0.9 at the v0_9 path",
				specVersion: v09SpecVersion,
				path:        "/rpc/v0_9",
				expectedErr: "the node at %[1]s/rpc/v0_9 serves RPC v0.9, but the validator " +
					"needs RPC v0.10: change v0_9 to v0_10 in both provider URLs (HTTP and WS), " +
					"e.g. %[1]s/rpc/v0_10",
			},
			{
				name:        "v0.8 at the v0_8 path",
				specVersion: "0.8.1",
				path:        "/v0_8",
				expectedErr: "the node at %[1]s/v0_8 serves RPC v0.8, but the validator " +
					"needs RPC v0.10: change v0_8 to v0_10 in both provider URLs (HTTP and WS), " +
					"e.g. %[1]s/v0_10",
			},
			{
				name:        "v0.11 pre-release at the v0_11 path",
				specVersion: "0.11.0-rc.0",
				path:        "/rpc/v0_11",
				expectedErr: "the node at %[1]s/rpc/v0_11 serves RPC v0.11, but the validator " +
					"needs RPC v0.10: change v0_11 to v0_10 in both provider URLs (HTTP and WS), " +
					"e.g. %[1]s/rpc/v0_10",
			},
			{
				name:        "v1.10 at the v0_10 path",
				specVersion: "1.10.3",
				path:        "/rpc/v0_10",
				expectedErr: "the node at %[1]s/rpc/v0_10 serves RPC v1.10, but the validator " +
					"needs RPC v0.10",
			},
			{
				name:        "v0.9 at a path holding an API key",
				specVersion: v09SpecVersion,
				path:        "/v0_9/" + apiKey,
				expectedErr: "the node at %[1]s/v0_9/REDACTED serves RPC v0.9, but the " +
					"validator needs RPC v0.10: change v0_9 to v0_10 in both provider URLs " +
					"(HTTP and WS), e.g. %[1]s/v0_10/REDACTED",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				server := mockNodeWithSpecVersion(t, tc.specVersion)

				provider, err := validator.NewProvider(t.Context(), server.URL+tc.path, logger)

				require.Nil(t, provider)
				require.EqualError(t, err, fmt.Sprintf(tc.expectedErr, server.URL))
				require.NotContains(t, err.Error(), apiKey)
				require.NotErrorIs(t, err, validator.ErrTransient)
			})
		}
	})

	t.Run("Node serving an unsupported RPC v0.10 release", func(t *testing.T) {
		for _, specVersion := range []string{"0.10.1", "0.10.5", "0.10.5-rc.0", "v0.10.1-rc.1"} {
			t.Run(specVersion, func(t *testing.T) {
				server := mockNodeWithSpecVersion(t, specVersion)

				provider, err := validator.NewProvider(t.Context(), server.URL, logger)

				require.Nil(t, provider)
				require.EqualError(t, err, fmt.Sprintf(
					"the node at %s serves RPC v%s, which this validator does not support: "+
						"update the node and the validator to their latest releases",
					server.URL, strings.TrimPrefix(specVersion, "v"),
				))
				require.NotErrorIs(t, err, validator.ErrTransient)
			})
		}
	})

	t.Run("Node reporting a malformed RPC version", func(t *testing.T) {
		for _, specVersion := range []string{
			"0.10.3-", "0.10.3-rc.01", "0.10.03", "0.10", "V0.10.3", " 0.10.3", "garbage", "",
		} {
			t.Run(specVersion, func(t *testing.T) {
				server := mockNodeWithSpecVersion(t, specVersion)

				provider, err := validator.NewProvider(t.Context(), server.URL, logger)

				require.Nil(t, provider)
				require.EqualError(t, err, fmt.Sprintf(
					"the node at %s reports RPC version %q, but the validator needs RPC v0.10",
					server.URL, specVersion,
				))
				require.NotErrorIs(t, err, validator.ErrTransient)
			})
		}
	})

	t.Run("Transport failures", func(t *testing.T) {
		closedServer := httptest.NewServer(http.NotFoundHandler())
		closedServer.Close()
		plainServer := mockNodeWithSpecVersion(t, "0.10.3")
		tlsServer := httptest.NewTLSServer(http.NotFoundHandler())
		t.Cleanup(tlsServer.Close)
		notTLSAddress := notTLSServer(t)

		testCases := []struct {
			name        string
			url         string
			expectedErr string
			transient   bool
		}{
			{
				name:        "connection refused",
				url:         closedServer.URL,
				expectedErr: "cannot reach RPC provider at " + closedServer.URL + ": connection refused",
				transient:   true,
			},
			{
				name: "HTTP 404",
				url:  mockNodeWithStatus(t, http.StatusNotFound, "404 page not found").URL,
				expectedErr: "cannot reach RPC provider at %s: HTTP 404 Not Found " +
					"(check the URL path)",
			},
			{
				name:        "HTTP 503",
				url:         mockNodeWithStatus(t, http.StatusServiceUnavailable, "").URL,
				expectedErr: "cannot reach RPC provider at %s: HTTP 503 Service Unavailable",
				transient:   true,
			},
			{
				name: "JSON-RPC internal error",
				url: mockNodeWithBody(
					t, `{"jsonrpc": "2.0", "error": {"code": -32603, "message": "busy"}, "id": 1}`,
				).URL,
				expectedErr: "answered the spec version request with error -32603: busy",
				transient:   true,
			},
			{
				name: "JSON-RPC method not found",
				url: mockNodeWithBody(
					t, `{"jsonrpc": "2.0", "error": {"code": -32601, "message": "nope"}, "id": 1}`,
				).URL,
				expectedErr: "answered the spec version request with error -32601: nope",
			},
			{
				name:        "TLS to a plain HTTP server",
				url:         "https" + strings.TrimPrefix(plainServer.URL, "http"),
				expectedErr: "server gave HTTP response to HTTPS client",
			},
			{
				name:        "TLS to a server that does not speak TLS",
				url:         "https://" + notTLSAddress,
				expectedErr: "first record does not look like a TLS handshake",
			},
			{
				name:        "untrusted certificate",
				url:         tlsServer.URL,
				expectedErr: "certificate signed by unknown authority",
			},
			{
				name: "API key in the URL",
				url:  closedServer.URL + "/v3/0123456789abcdef0123456789abcdef?apikey=secret",
				expectedErr: "cannot reach RPC provider at " + closedServer.URL +
					"/v3/REDACTED?apikey=REDACTED: connection refused",
				transient: true,
			},
			{
				name:        "invalid URL",
				url:         "http://127.0.0.1:port",
				expectedErr: "invalid RPC provider URL",
			},
			{
				name:        "unknown URL scheme",
				url:         "ftp://localhost:6060",
				expectedErr: "cannot create RPC provider at ftp://localhost:6060",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				provider, err := validator.NewProvider(t.Context(), tc.url, logger)

				require.Nil(t, provider)
				expectedErr := tc.expectedErr
				if strings.Contains(expectedErr, "%s") {
					expectedErr = fmt.Sprintf(expectedErr, tc.url)
				}
				require.ErrorContains(t, err, expectedErr)
				require.NotContains(t, err.Error(), "-32603 Internal Error")
				if tc.transient {
					require.ErrorIs(t, err, validator.ErrTransient)
				} else {
					require.NotErrorIs(t, err, validator.ErrTransient)
				}
			})
		}
	})

	envVars, err := validator.LoadEnv(t)
	loadedEnvVars := err == nil
	if loadedEnvVars {
		t.Run("Successful provider creation", func(t *testing.T) {
			if err != nil {
				t.Skip(err)
			}

			provider, inErr := validator.NewProvider(t.Context(), envVars.HTTPProviderURL, logger)

			require.NoError(t, inErr)
			require.NotNil(t, provider)
		})
	} else {
		t.Logf("Ignoring tests that require env variables: %s", err)
	}
}

func TestBlockHeaderSubscription(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	logger := log.NewNopZapLogger()

	t.Run("Error creating provider", func(t *testing.T) {
		wsProviderURL := "wrong url"
		wsProvider, headerFeed, clientSubscription, err := validator.SubscribeToBlockHeaders(
			t.Context(), wsProviderURL, logger,
		)

		require.Nil(t, wsProvider)
		require.Nil(t, headerFeed)
		require.Nil(t, clientSubscription)
		expectedErrorMsg := "dialling WS provider at " + wsProviderURL
		require.ErrorContains(t, err, expectedErrorMsg)
	})

	// Cannot test error when subscribing to new block headers

	envVars, err := validator.LoadEnv(t)
	if loadedEnvVars := err == nil; loadedEnvVars {
		t.Run("Successfully subscribing to new block headers", func(t *testing.T) {
			wsProvider, headerChannel, clientSubscription, inErr := validator.SubscribeToBlockHeaders(
				t.Context(),
				envVars.WSProviderURL,
				logger,
			)

			require.NotNil(t, wsProvider)
			require.NotNil(t, headerChannel)
			require.NotNil(t, clientSubscription)
			require.Nil(t, inErr)

			wsProvider.Close()
			close(headerChannel)
		})
	} else {
		t.Logf("Ignoring tests that require env variables: %s", err)
	}
}

func TestNewRejectsNonWebsocketURL(t *testing.T) {
	for _, wsURL := range []string{"http://localhost:6061", "https://localhost:6061", "localhost"} {
		t.Run(wsURL, func(t *testing.T) {
			conf := config.Config{Provider: config.Provider{HTTP: "http://localhost:1", WS: wsURL}}

			_, err := validator.New(
				t.Context(), &conf, new(config.StarknetConfig), log.NewNopZapLogger(),
			)

			require.ErrorContains(t, err, "WS provider URL must use ws:// or wss://")
			require.NotErrorIs(t, err, validator.ErrTransient)
		})
	}
}

func TestNewStartupLogs(t *testing.T) {
	server := validator.MockRPCServer(t, validator.MockRPC{
		Results: nil,
		CallResults: map[string]string{
			"get_attestation_info_by_operational_address": `["0x789", "0x1", "0xa", "0x1", "0x1"]`,
			"get_staker_info_v1":                          `["0x0", "0x789", "0x456", "0x1", "0x0", "0x0"]`,
		},
		OnWSUpgrade: nil,
	})

	testCases := []struct {
		name         string
		signer       config.Signer
		expectedLine string
	}{
		{
			name:         "internal signer",
			signer:       config.Signer{OperationalAddress: "0x456", PrivKey: "0x123"},
			expectedLine: "using internal signer\n",
		},
		{
			name: "external signer",
			signer: config.Signer{
				OperationalAddress: "0x456", ExternalURL: "http://localhost:1",
			},
			expectedLine: `using external signer	{"externalSignerURL": "http://localhost:1"}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger, err := log.NewZapLogger(
				log.NewLevel(log.INFO), log.WithWriter(&buf), log.WithColour(false),
			)
			require.NoError(t, err)
			conf := config.Config{
				Provider: config.Provider{HTTP: server.URL, WS: "ws://localhost:1"},
				Signer:   tc.signer,
				Reward: config.Reward{
					Address: "0x789", PrivKey: "0xabc", ClaimThreshold: 1,
				},
			}

			_, err = validator.New(
				t.Context(), &conf, new(config.StarknetConfig), logger,
			)

			require.NoError(t, err)
			require.Contains(t, buf.String(), tc.expectedLine)
			require.Contains(
				t, buf.String(), `connected to RPC	{"providerURL": "`+server.URL+`"}`,
			)
			require.Contains(t, buf.String(), `"stakingContractAddress": "0x`)
			require.Contains(t, buf.String(), `"attestContractAddress": "0x`)
			require.NotContains(t, buf.String(), "validationContracts")
			require.Contains(t, buf.String(), `"rewardAddress": "0x789"}`)
		})
	}
}
