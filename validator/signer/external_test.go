package signer_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	s "github.com/NethermindEth/starknet-staking-v2/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	snUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNewExternalSigner(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	t.Run("Error getting chain ID", func(t *testing.T) {
		// Setup
		provider, providerErr := rpc.NewProvider("http://localhost:1234")
		require.NoError(t, providerErr)

		externalSigner, err := signer.NewExternalSigner(
			provider,
			utils.NewNopZapLogger(),
			&config.Signer{
				ExternalURL:        "http://localhost:1234",
				OperationalAddress: "0x123",
			},
			new(config.ContractAddresses).SetDefaults("SN_SEPOLIA"),
		)

		require.Zero(t, externalSigner)
		require.Error(t, err)
	})
}

func TestExternalSignerAddress(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	logger := utils.NewNopZapLogger()

	t.Run("Return signer address", func(t *testing.T) {
		operationalAddress := utils.HexToFelt(t, "0x123")

		mockRpc := validator.MockRPCServer(t, operationalAddress, "")
		defer mockRpc.Close()

		provider, err := rpc.NewProvider(mockRpc.URL)
		require.NoError(t, err)

		externalSigner, err := signer.NewExternalSigner(
			provider,
			logger,
			&config.Signer{
				ExternalURL:        "http://localhost:1234",
				OperationalAddress: "0x123",
			},
			new(config.ContractAddresses).SetDefaults("SN_SEPOLIA"),
		)
		require.NoError(t, err)

		require.Equal(t, *externalSigner.Address(), types.AddressFromString("0x123"))
	})
}

func TestBuildAndSendInvokeTxn(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	logger := utils.NewNopZapLogger()

	t.Run("Error getting nonce", func(t *testing.T) {
		env, err := validator.LoadEnv(t)
		if err != nil {
			t.Skipf("Ignoring tests that require env variables: %s", err)
		}

		provider, providerErr := rpc.NewProvider(env.HttpProviderUrl)
		require.NoError(t, providerErr)

		externalSigner, err := signer.NewExternalSigner(
			provider,
			logger,
			&config.Signer{
				ExternalURL:        "http://localhost:1234",
				OperationalAddress: "0x123",
			},
			new(config.ContractAddresses).SetDefaults("SN_SEPOLIA"),
		)
		require.NoError(t, err)

		addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(
			t.Context(), []rpc.InvokeFunctionCall{}, constants.FEE_ESTIMATION_MULTIPLIER,
		)

		require.Nil(t, addInvokeTxRes)
		expectedError := rpc.RPCError{Code: 20, Message: "Contract not found"}
		require.Equal(t, expectedError.Error(), err.Error())
	})

	t.Run("Error signing transaction the first time (for estimating fee)", func(t *testing.T) {
		env, err := validator.LoadEnv(t)
		if err != nil {
			t.Skipf("Ignoring tests that require env variables: %s", err)
		}

		provider, providerErr := rpc.NewProvider(env.HttpProviderUrl)
		require.NoError(t, providerErr)

		const serverError = "some internal error"
		// Create a mock server
		mockServer := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					// Simulate API response
					http.Error(w, serverError, http.StatusInternalServerError)
				}))
		defer mockServer.Close()

		externalSigner, err := signer.NewExternalSigner(
			provider,
			logger,
			&config.Signer{
				ExternalURL:        mockServer.URL,
				OperationalAddress: "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e",
			},
			new(config.ContractAddresses).SetDefaults("SN_SEPOLIA"),
		)
		require.NoError(t, err)

		addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(
			t.Context(), []rpc.InvokeFunctionCall{}, constants.FEE_ESTIMATION_MULTIPLIER,
		)

		require.Nil(t, addInvokeTxRes)
		expectedErrorMsg := fmt.Sprintf(
			"server error %d: %s", http.StatusInternalServerError, serverError,
		)
		require.EqualError(t, err, expectedErrorMsg)
	})

	t.Run("Error estimating fee", func(t *testing.T) {
		env, err := validator.LoadEnv(t)
		if err != nil {
			t.Skipf("Ignoring tests that require env variables: %s", err)
		}

		provider, providerErr := rpc.NewProvider(env.HttpProviderUrl)
		require.NoError(t, providerErr)

		// Create a mock server
		mockServer := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					// Simulate API response
					_, err := w.Write([]byte(`{"signature": ["0x123", "0x456"]}`))
					require.NoError(t, err)
				}))
		defer mockServer.Close()

		externalSigner, err := signer.NewExternalSigner(
			provider,
			logger,
			&config.Signer{
				ExternalURL:        mockServer.URL,
				OperationalAddress: "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e",
			},
			new(config.ContractAddresses).SetDefaults("SN_SEPOLIA"),
		)
		require.NoError(t, err)

		addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(
			t.Context(),
			[]rpc.InvokeFunctionCall{},
			constants.FEE_ESTIMATION_MULTIPLIER,
		)

		require.Nil(t, addInvokeTxRes)
		require.Contains(t, err.Error(), "Account: invalid signature")
	})

	t.Run(
		"Error signing transaction the second time (for the actual invoke tx)",
		func(t *testing.T) {
			mockRpc := createMockRPCServer(t, nil)
			defer mockRpc.Close()

			signerCalledCount := 0
			signerInternalError := "error when signing the 2nd time"
			mockSigner := httptest.NewServer(
				http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						signerCalledCount++
						if signerCalledCount == 1 {
							w.WriteHeader(http.StatusOK)
							_, err := w.Write([]byte(`{"signature": ["0x111", "0x222"]}`))
							require.NoError(t, err)
						} else {
							w.WriteHeader(http.StatusInternalServerError)
							_, err := w.Write([]byte(signerInternalError))
							require.NoError(t, err)
						}
					}))
			defer mockSigner.Close()

			provider, providerErr := rpc.NewProvider(mockRpc.URL)
			require.NoError(t, providerErr)

			externalSigner, err := signer.NewExternalSigner(
				provider,
				logger,
				&config.Signer{
					ExternalURL:        mockSigner.URL,
					OperationalAddress: "0xabc",
				},
				new(config.ContractAddresses).SetDefaults("SN_SEPOLIA"),
			)
			require.NoError(t, err)

			addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(
				t.Context(),
				[]rpc.InvokeFunctionCall{},
				constants.FEE_ESTIMATION_MULTIPLIER,
			)
			require.Nil(t, addInvokeTxRes)

			expectedErrorMsg := fmt.Sprintf(
				"server error %d: %s", http.StatusInternalServerError, signerInternalError,
			)
			require.EqualError(t, err, expectedErrorMsg)
			require.Equal(t, 2, signerCalledCount)
		})

	t.Run("Error invoking transaction", func(t *testing.T) {
		serverInternalError := "Error processing invoke transaction"

		addInvoke := func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, err := w.Write([]byte(serverInternalError))
			require.NoError(t, err)
		}
		mockRpc := createMockRPCServer(t, addInvoke)
		defer mockRpc.Close()

		mockSigner := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte(`{"signature": ["0x111", "0x222"]}`))
					require.NoError(t, err)
				}))
		defer mockSigner.Close()

		provider, providerErr := rpc.NewProvider(mockRpc.URL)
		require.NoError(t, providerErr)

		externalSigner, err := signer.NewExternalSigner(
			provider,
			logger,
			&config.Signer{
				ExternalURL:        mockSigner.URL,
				OperationalAddress: "0xabc",
			},
			new(config.ContractAddresses).SetDefaults("SN_SEPOLIA"),
		)
		require.NoError(t, err)

		addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(
			t.Context(),
			[]rpc.InvokeFunctionCall{},
			constants.FEE_ESTIMATION_MULTIPLIER,
		)

		require.Nil(t, addInvokeTxRes)
		expectedServerErr := fmt.Sprintf(
			"%d Internal Server Error: %s", http.StatusInternalServerError, serverInternalError,
		)
		expectedError := rpc.RPCError{
			Code:    rpc.InternalError,
			Message: "The error is not a valid RPC error",
			Data:    rpc.StringErrData(expectedServerErr),
		}
		require.EqualError(t, err, expectedError.Error())
	})

	t.Run("Successfully sent and received transaction hash", func(t *testing.T) {
		expectedInvokeTxHash := "0x789"

		addInvoke := func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, err := fmt.Fprintf(
				w,
				`{"jsonrpc": "2.0", "result": {"transaction_hash": "%s"}, "id": 1}`,
				expectedInvokeTxHash,
			)
			require.NoError(t, err)
		}
		mockRpc := createMockRPCServer(t, addInvoke)
		defer mockRpc.Close()

		mockSigner := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, err := w.Write([]byte(`{"signature": ["0x111", "0x222"]}`))
					require.NoError(t, err)
				}))
		defer mockSigner.Close()

		provider, providerErr := rpc.NewProvider(mockRpc.URL)
		require.NoError(t, providerErr)

		externalSigner, err := signer.NewExternalSigner(
			provider,
			logger,
			&config.Signer{
				ExternalURL:        mockSigner.URL,
				OperationalAddress: "0xabc",
			},
			new(config.ContractAddresses).SetDefaults("SN_SEPOLIA"),
		)
		require.NoError(t, err)

		addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(
			t.Context(),
			[]rpc.InvokeFunctionCall{},
			constants.FEE_ESTIMATION_MULTIPLIER,
		)

		require.Equal(t, &rpc.AddInvokeTransactionResponse{Hash: utils.HexToFelt(t, expectedInvokeTxHash)}, addInvokeTxRes)
		require.Nil(t, err)
	})
}

func TestHashAndSignTx(t *testing.T) {
	t.Run("Error making request", func(t *testing.T) {
		externalSignerURL := "http://localhost:1234"

		invokeTxnV3 := snUtils.BuildInvokeTxn(
			utils.HexToFelt(t, "0x123"),
			new(felt.Felt).SetUint64(1),
			[]*felt.Felt{},
			&rpc.ResourceBoundsMapping{},
		)
		chainID := new(felt.Felt).SetUint64(1)
		res, err := signer.HashAndSignTx(invokeTxnV3, chainID, externalSignerURL)

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

		invokeTxnV3 := snUtils.BuildInvokeTxn(
			utils.HexToFelt(t, "0x123"),
			new(felt.Felt).SetUint64(1),
			[]*felt.Felt{},
			&rpc.ResourceBoundsMapping{},
		)
		chainID := new(felt.Felt).SetUint64(1)
		res, err := signer.HashAndSignTx(invokeTxnV3, chainID, mockServer.URL)

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

		invokeTxnV3 := snUtils.BuildInvokeTxn(
			utils.HexToFelt(t, "0x123"),
			new(felt.Felt).SetUint64(1),
			[]*felt.Felt{},
			&rpc.ResourceBoundsMapping{},
		)
		chainID := new(felt.Felt).SetUint64(1)
		res, err := signer.HashAndSignTx(invokeTxnV3, chainID, mockServer.URL)

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

		invokeTxnV3 := snUtils.BuildInvokeTxn(
			utils.HexToFelt(t, "0x123"),
			new(felt.Felt).SetUint64(1),
			[]*felt.Felt{},
			&rpc.ResourceBoundsMapping{},
		)
		chainID := new(felt.Felt).SetUint64(1)
		res, err := signer.HashAndSignTx(invokeTxnV3, chainID, mockServer.URL)

		expectedResult := s.Response{
			Signature: [2]*felt.Felt{
				new(felt.Felt).SetUint64(0x123),
				new(felt.Felt).SetUint64(0x456),
			},
		}
		require.NoError(t, err)
		require.Equal(t, expectedResult, res)
	})
}
