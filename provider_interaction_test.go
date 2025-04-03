package main_test

import (
	"errors"
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

		mockLogger.EXPECT().
			Fatalf("Error connecting to RPC provider at %s: %s", providerUrl, errors.New(`no known transport for URL scheme ""`)).
			Do(func(_ string, _ ...interface{}) {
				panic("Fatalf called") // Simulate os.Exit
			})

		defer func() {
			if r := recover(); r == nil {
				require.FailNow(t, "The code did not panic when it should have")
			} else {
				// Just making sure the exec panicked for the right reason
				require.Equal(t, "Fatalf called", r)
			}
		}()

		main.NewProvider(providerUrl, mockLogger)
	})

	t.Run("Successful provider creation", func(t *testing.T) {
		providerUrl := "http://localhost:6060"

		mockLogger.EXPECT().
			Infow("Successfully connected to RPC provider", "providerUrl", "*****")

		provider := main.NewProvider(providerUrl, mockLogger)

		// Cannot deeply compare 2 providers (comparing channels does not works)
		require.NotNil(t, provider)
	})
}

func TestBlockHeaderSubscription(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockLogger := mocks.NewMockLogger(mockCtrl)

	t.Run("Error creating provider", func(t *testing.T) {
		wsProviderUrl := "wrong url"

		mockLogger.EXPECT().
			Fatalf("Error dialing the WS provider at %s: %s", wsProviderUrl, errors.New(`no known transport for URL scheme ""`)).
			Do(func(_ string, _ ...interface{}) {
				panic("Fatalf called") // Simulate os.Exit
			})

		defer func() {
			if r := recover(); r == nil {
				require.FailNow(t, "The code did not panic when it should have")
			} else {
				// Just making sure the exec panicked for the right reason
				require.Equal(t, "Fatalf called", r)
			}
		}()

		main.BlockHeaderSubscription(wsProviderUrl, mockLogger)
	})

	// TODO: Fix the strange error when trying to subscribe to blockHeaders `The error is not a valid RPC error: json: cannot unmarshal string into Go value of type uint64`
	// t.Run("Error subscribing to new block headers", func(t *testing.T) {
	// 	envVars := loadEnv(t)

	// 	mockLogger.EXPECT().
	// 		Fatalf("Error subscribing to new block headers: %s", errors.New("The error is not a valid RPC error: json: cannot unmarshal string into Go value of type uint64")).
	// 		Do(func(_ string, _ ...interface{}) {
	// 			panic("Fatalf called") // Simulate os.Exit
	// 		})

	// 	defer func() {
	// 		if r := recover(); r == nil {
	// 			require.FailNow(t, "The code did not panic when it should have")
	// 		} else {
	// 			// Just making sure the exec panicked for the right reason
	// 			require.Equal(t, "Fatalf called", r)
	// 		}
	// 	}()

	// 	main.BlockHeaderSubscription(envVars.wsProviderUrl, mockLogger)
	// })

	// TODO: make a successful test once the previous error-expecting test passes
}
