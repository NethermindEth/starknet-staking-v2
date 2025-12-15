package validator_test

import (
	"testing"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

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
