package validator_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/signer"
	main "github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/stretchr/testify/require"
)

func TestSignTxHash(t *testing.T) {
	t.Run("Error making request", func(t *testing.T) {
		hashToSign := utils.HexToFelt(t, "0x123")
		externalSignerUrl := "http://localhost:1234"

		res, err := main.SignTxHash(hashToSign, externalSignerUrl)

		require.Zero(t, res)
		require.ErrorContains(t, err, "connection refused")
	})

	t.Run("Request succeeded but server internal error", func(t *testing.T) {
		serverError := "some internal error"

		// Create a mock server
		mockServer := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					// Simulate API response
					w.WriteHeader(http.StatusInternalServerError)
					_, err := w.Write([]byte(serverError))
					require.NoError(t, err)
				}))
		defer mockServer.Close()

		hashToSign := utils.HexToFelt(t, "0x123")
		res, err := main.SignTxHash(hashToSign, mockServer.URL)

		require.Zero(t, res)
		expectedErrorMsg := fmt.Sprintf("server error %d: %s", http.StatusInternalServerError, serverError)
		require.EqualError(t, err, expectedErrorMsg)
	})

	t.Run("Request succeeded but error when decoding response body", func(t *testing.T) {
		// Create a mock server
		mockServer := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					// Simulate API response
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte("not a valid marshalled SignResponse object"))
					require.NoError(t, err)
				}))
		defer mockServer.Close()

		hashToSign := utils.HexToFelt(t, "0x123")
		res, err := main.SignTxHash(hashToSign, mockServer.URL)

		require.Zero(t, res)
		require.ErrorContains(t, err, "invalid character")
	})

	t.Run("Successful request and response", func(t *testing.T) {
		// Create a mock server
		mockServer := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					// Simulate API response
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte(`{"signature": ["0x123", "0x456"]}`))
					require.NoError(t, err)
				}))
		defer mockServer.Close()

		hashToSign := utils.HexToFelt(t, "0x123")
		res, err := main.SignTxHash(hashToSign, mockServer.URL)

		expectedResult := signer.Response{
			Signature: [2]felt.Felt{
				*new(felt.Felt).SetUint64(0x123),
				*new(felt.Felt).SetUint64(0x456),
			},
		}
		require.NoError(t, err)
		require.Equal(t, expectedResult, res)
	})
}
