package validator_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	main "github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet.go/rpc"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/stretchr/testify/require"
)

func TestHashAndSignTx(t *testing.T) {
	t.Run("Error making request", func(t *testing.T) {
		externalSignerUrl := "http://localhost:1234"

		invokeTxnV3 := snGoUtils.BuildInvokeTxn(
			utils.HexToFelt(t, "0x123"),
			new(felt.Felt).SetUint64(1),
			[]*felt.Felt{},
			rpc.ResourceBoundsMapping{},
		)
		chainId := new(felt.Felt).SetUint64(1)
		res, err := main.HashAndSignTx(&invokeTxnV3.InvokeTxnV3, chainId, externalSignerUrl)

		require.Nil(t, res)
		require.ErrorContains(t, err, "connection refused")
	})

	t.Run("Request succeeded but server internal error", func(t *testing.T) {
		serverError := "some internal error"

		// Create a mock server
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate API response
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(serverError))
		}))
		defer mockServer.Close()

		invokeTxnV3 := snGoUtils.BuildInvokeTxn(
			utils.HexToFelt(t, "0x123"),
			new(felt.Felt).SetUint64(1),
			[]*felt.Felt{},
			rpc.ResourceBoundsMapping{},
		)
		chainId := new(felt.Felt).SetUint64(1)
		res, err := main.HashAndSignTx(&invokeTxnV3.InvokeTxnV3, chainId, mockServer.URL)

		require.Nil(t, res)
		expectedErrorMsg := fmt.Sprintf("Server error %d: %s", http.StatusInternalServerError, serverError)
		require.EqualError(t, err, expectedErrorMsg)
	})

	t.Run("Request succeeded but error when decoding response body", func(t *testing.T) {
		// Create a mock server
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate API response
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("not a valid marshalled SignResponse object"))
		}))
		defer mockServer.Close()

		invokeTxnV3 := snGoUtils.BuildInvokeTxn(
			utils.HexToFelt(t, "0x123"),
			new(felt.Felt).SetUint64(1),
			[]*felt.Felt{},
			rpc.ResourceBoundsMapping{},
		)
		chainId := new(felt.Felt).SetUint64(1)
		res, err := main.HashAndSignTx(&invokeTxnV3.InvokeTxnV3, chainId, mockServer.URL)

		require.Nil(t, res)
		require.ErrorContains(t, err, "invalid character")
	})

	t.Run("Successful request and response", func(t *testing.T) {
		// Create a mock server
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate API response
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"signature": ["0x123", "0x456"]}`))
		}))
		defer mockServer.Close()

		invokeTxnV3 := snGoUtils.BuildInvokeTxn(
			utils.HexToFelt(t, "0x123"),
			new(felt.Felt).SetUint64(1),
			[]*felt.Felt{},
			rpc.ResourceBoundsMapping{},
		)
		chainId := new(felt.Felt).SetUint64(1)
		res, err := main.HashAndSignTx(&invokeTxnV3.InvokeTxnV3, chainId, mockServer.URL)

		expectedResult := &main.SignResponse{
			Signature: []*felt.Felt{
				utils.HexToFelt(t, "0x123"),
				utils.HexToFelt(t, "0x456"),
			},
		}
		require.Equal(t, expectedResult, res)
		require.Nil(t, err)
	})
}
