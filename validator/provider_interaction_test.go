package validator_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
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
		for _, specVersion := range []string{"0.10.3", "v0.10.3", "0.10.4"} {
			t.Run(specVersion, func(t *testing.T) {
				server := mockNodeWithSpecVersion(t, specVersion)

				provider, err := validator.NewProvider(t.Context(), server.URL, logger)

				require.NoError(t, err)
				require.NotNil(t, provider)
			})
		}
	})

	t.Run("Unsupported spec version", func(t *testing.T) {
		for _, specVersion := range []string{"0.9.0", "0.10.1", "0.10.5", "0.11.0"} {
			t.Run(specVersion, func(t *testing.T) {
				server := mockNodeWithSpecVersion(t, specVersion)

				provider, err := validator.NewProvider(t.Context(), server.URL, logger)

				require.Nil(t, provider)
				require.ErrorContains(t, err, "implements JSON-RPC specification "+specVersion)
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
			require.Contains(
				t, buf.String(), `connected to RPC	{"providerURL": "`+server.URL+`"}`,
			)
			require.Contains(t, buf.String(), `"stakingContractAddress": "0x`)
			require.Contains(t, buf.String(), `"attestContractAddress": "0x`)
			require.NotContains(t, buf.String(), "validationContracts")
			require.Contains(t, buf.String(), `"rewardAddress": "0x789", "braavosAccount": true}`)
		})
	}
}
