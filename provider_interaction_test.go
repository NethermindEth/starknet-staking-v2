package main_test

import (
	"fmt"
	"testing"

	main "github.com/NethermindEth/starknet-staking-v2"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNewProvider(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockLogger := mocks.NewMockLogger(mockCtrl)

	t.Run("Error creating provider", func(t *testing.T) {
		providerUrl := "wrong url"

		provider, err := main.NewProvider(providerUrl, mockLogger)

		require.Nil(t, provider)
		expectedErrorMsg := fmt.Sprintf(`Error creating RPC provider at %s: no known transport for URL scheme ""`, providerUrl)
		require.Equal(t, expectedErrorMsg, err.Error())
	})

	t.Run("Error connecting to provider", func(t *testing.T) {
		providerUrl := "http://localhost:1234"

		provider, err := main.NewProvider(providerUrl, mockLogger)

		require.Nil(t, provider)

		expectedErrorMsg := fmt.Sprintf(`Error connecting to RPC provider at %s`, providerUrl)
		require.ErrorContains(t, err, expectedErrorMsg)
	})

	t.Run("Successful provider creation", func(t *testing.T) {
		envVars := loadEnv(t)

		mockLogger.EXPECT().
			Infow("Successfully connected to RPC provider", "providerUrl", "*****")

		provider, err := main.NewProvider(envVars.httpProviderUrl, mockLogger)

		// Cannot deeply compare 2 providers (comparing channels does not works)
		require.NotNil(t, provider)
		require.Nil(t, err)
	})
}

func TestBlockHeaderSubscription(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockLogger := mocks.NewMockLogger(mockCtrl)

	t.Run("Error creating provider", func(t *testing.T) {
		wsProviderUrl := "wrong url"
		wsProvider, headerFeed, err := main.BlockHeaderSubscription(wsProviderUrl, mockLogger)

		require.Nil(t, wsProvider)
		require.Nil(t, headerFeed)
		expectedErrorMsg := fmt.Sprintf(`Error dialing the WS provider at %s: no known transport for URL scheme ""`, wsProviderUrl)
		require.Equal(t, expectedErrorMsg, err.Error())
	})

	// Cannot test error when subscribing to new block headers

	t.Run("Successfully subscribing to new block headers", func(t *testing.T) {
		envVars := loadEnv(t)

		mockLogger.EXPECT().Infow("Successfully subscribed to new block headers", "Subscription ID", gomock.Any())

		wsProvider, headerChannel, err := main.BlockHeaderSubscription(envVars.wsProviderUrl, mockLogger)

		require.NotNil(t, wsProvider)
		require.NotNil(t, headerChannel)
		require.Nil(t, err)

		wsProvider.Close()
		close(headerChannel)
	})
}
