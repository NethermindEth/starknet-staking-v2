package validator_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator"
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

	logger := utils.NewNopZapLogger()

	t.Run("Error creating provider", func(t *testing.T) {
		providerURL := "wrong url"

		provider, err := validator.NewProvider(t.Context(), providerURL, logger)

		require.Nil(t, provider)
		expectedErrorMsg := "cannot create RPC provider at " + providerURL
		require.ErrorContains(t, err, expectedErrorMsg)
	})

	t.Run("Supported spec version", func(t *testing.T) {
		for _, specVersion := range []string{"0.10.3", "v0.10.3"} {
			t.Run(specVersion, func(t *testing.T) {
				server := mockNodeWithSpecVersion(t, specVersion)

				provider, err := validator.NewProvider(t.Context(), server.URL, logger)

				require.NoError(t, err)
				require.NotNil(t, provider)
			})
		}
	})

	t.Run("Unsupported spec version", func(t *testing.T) {
		for _, specVersion := range []string{"0.9.0", "0.10.1", "0.10.4", "0.11.0"} {
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

	logger := utils.NewNopZapLogger()

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
