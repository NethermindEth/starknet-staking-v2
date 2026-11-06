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
		} {
			t.Run(specVersion, func(t *testing.T) {
				server := mockNodeWithSpecVersion(t, specVersion)

				provider, err := validator.NewProvider(t.Context(), server.URL, logger)

				require.NoError(t, err)
				require.NotNil(t, provider)
			})
		}
	})

	t.Run("Unsupported spec version", func(t *testing.T) {
		for _, specVersion := range []string{
			"0.9.0", "0.10.1", "0.10.5", "0.11.0", "1.10.3",
			"0.10.3-rc1", "0.10.2-rc1", "0.10.03", "0.10", "V0.10.3", " 0.10.3", "garbage", "",
		} {
			t.Run(specVersion, func(t *testing.T) {
				server := mockNodeWithSpecVersion(t, specVersion)

				provider, err := validator.NewProvider(t.Context(), server.URL, logger)

				require.Nil(t, provider)
				require.ErrorContains(t, err, "implements JSON-RPC specification "+specVersion)
				require.ErrorContains(t, err, "this tool requires 0.10.2 or 0.10.3 or 0.10.4")
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
				t.Context(), &conf, new(config.StarknetConfig), log.NewNopZapLogger(), false,
			)

			require.ErrorContains(t, err, "WS provider URL must use ws:// or wss://")
			require.NotErrorIs(t, err, validator.ErrTransient)
		})
	}
}

func TestNewLogsBraavosMode(t *testing.T) {
	server := validator.MockRPCServer(t, validator.MockRPC{
		Results: nil,
		CallResults: map[string]string{
			"get_attestation_info_by_operational_address": `["0x789", "0x1", "0xa", "0x1", "0x1"]`,
			"get_staker_info_v1":                          `["0x0", "0x789", "0x456", "0x1", "0x0", "0x0"]`,
		},
		OnWSUpgrade: nil,
	})
	rewardBraavos := true

	testCases := []struct {
		name         string
		signer       config.Signer
		braavos      bool
		expectedLine string
	}{
		{
			name:         "internal signer",
			signer:       config.Signer{OperationalAddress: "0x456", PrivKey: "0x123"},
			braavos:      true,
			expectedLine: `using internal signer	{"braavosAccount": true}`,
		},
		{
			name: "external signer",
			signer: config.Signer{
				OperationalAddress: "0x456", ExternalURL: "http://localhost:1",
			},
			expectedLine: `using external signer	{"externalSignerURL": ` +
				`"http://localhost:1", "braavosAccount": false}`,
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
					Address: "0x789", PrivKey: "0xabc", ClaimThreshold: 1, Braavos: &rewardBraavos,
				},
			}

			_, err = validator.New(
				t.Context(), &conf, new(config.StarknetConfig), logger, tc.braavos,
			)

			require.NoError(t, err)
			require.Contains(t, buf.String(), tc.expectedLine)
			require.Contains(t, buf.String(), `"rewardAddress": "0x789", "braavosAccount": true}`)
		})
	}
}
