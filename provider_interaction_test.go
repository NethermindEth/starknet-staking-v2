package main_test

import (
	"fmt"
	"testing"

	"github.com/NethermindEth/juno/utils"
	main "github.com/NethermindEth/starknet-staking-v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNewProvider(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	logger, err := utils.NewZapLogger(utils.DEBUG, true)
	require.NoError(t, err)

	t.Run("Error creating provider", func(t *testing.T) {
		providerUrl := "wrong url"

		provider, err := main.NewProvider(providerUrl, logger)

		require.Nil(t, provider)
		expectedErrorMsg := fmt.Sprintf(`Error creating RPC provider at %s: no known transport for URL scheme ""`, providerUrl)
		require.Equal(t, expectedErrorMsg, err.Error())
	})

	t.Run("Error connecting to provider", func(t *testing.T) {
		providerUrl := "http://localhost:1234"

		provider, err := main.NewProvider(providerUrl, logger)

		require.Nil(t, provider)

		expectedErrorMsg := fmt.Sprintf(`Error connecting to RPC provider at %s`, providerUrl)
		require.ErrorContains(t, err, expectedErrorMsg)
	})

	envVars, err := loadEnv(t)
	loadedEnvVars := err == nil
	if loadedEnvVars {
		t.Run("Successful provider creation", func(t *testing.T) {
			if err != nil {
				t.Skip(err)
			}

			provider, err := main.NewProvider(envVars.httpProviderUrl, logger)

			// Cannot deeply compare 2 providers (comparing channels does not works)
			require.NotNil(t, provider)
			require.Nil(t, err)
		})
	} else {
		t.Logf("Ignoring tests that require env variables: %s", err)
	}
}

func TestBlockHeaderSubscription(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	logger, err := utils.NewZapLogger(utils.DEBUG, true)
	require.NoError(t, err)

	t.Run("Error creating provider", func(t *testing.T) {
		wsProviderUrl := "wrong url"
		wsProvider, headerFeed, err := main.BlockHeaderSubscription(wsProviderUrl, logger)

		require.Nil(t, wsProvider)
		require.Nil(t, headerFeed)
		expectedErrorMsg := fmt.Sprintf(`Error dialing the WS provider at %s: no known transport for URL scheme ""`, wsProviderUrl)
		require.Equal(t, expectedErrorMsg, err.Error())
	})

	// Cannot test error when subscribing to new block headers

	envVars, err := loadEnv(t)
	loadedEnvVars := err == nil
	if loadedEnvVars {
		t.Run("Successfully subscribing to new block headers", func(t *testing.T) {

			wsProvider, headerChannel, err := main.BlockHeaderSubscription(envVars.wsProviderUrl, logger)

			require.NotNil(t, wsProvider)
			require.NotNil(t, headerChannel)
			require.Nil(t, err)

			wsProvider.Close()
			close(headerChannel)
		})
	} else {
		t.Logf("Ignoring tests that require env variables: %s", err)
	}
}
