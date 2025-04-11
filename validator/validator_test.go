package validator_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/hash"
	"github.com/NethermindEth/starknet.go/rpc"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"lukechampine.com/uint128"
)

type envVariable struct {
	httpProviderUrl string
	wsProviderUrl   string
}

type Method struct {
	Name string `json:"method"`
}

func loadEnv(t *testing.T) (envVariable, error) {
	t.Helper()

	_, err := os.Stat(".env")
	if err == nil {
		if err = godotenv.Load(".env"); err != nil {
			return envVariable{}, errors.Join(errors.New("error loading '.env' file"), err)
		}
	}

	base := os.Getenv("HTTP_PROVIDER_URL")
	if base == "" {
		return envVariable{}, errors.New("Failed to load HTTP_PROVIDER_URL, empty string")
	}

	wsProviderUrl := os.Getenv("WS_PROVIDER_URL")
	if wsProviderUrl == "" {
		return envVariable{}, errors.New("Failed to load WS_PROVIDER_URL, empty string")
	}

	return envVariable{base, wsProviderUrl}, nil
}

func TestNewValidatorAccount(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	logger := utils.NewNopZapLogger()
	envVars, err := loadEnv(t)
	loadedEnvVars := err == nil

	if loadedEnvVars {
		t.Run("Error: private key conversion", func(t *testing.T) {
			provider, providerErr := rpc.NewProvider(envVars.httpProviderUrl)
			require.NoError(t, providerErr)

			validatorAccount, err := validator.NewInternalSigner(
				provider, logger, &validator.Signer{},
			)

			require.Equal(t, validator.InternalSigner{}, validatorAccount)
			expectedErrorMsg := fmt.Sprintf(
				"Cannot turn private key %s into a big int", (*big.Int)(nil),
			)
			require.Equal(t, expectedErrorMsg, err.Error())
		})
		t.Run("Successful account creation", func(t *testing.T) {
			provider, err := rpc.NewProvider(envVars.httpProviderUrl)
			require.NoError(t, err)

			signer := validator.Signer{
				PrivKey:            "0x123",
				OperationalAddress: "0x456",
			}

			// Test
			validatorAccount, err := validator.NewInternalSigner(provider, logger, &signer)
			require.NoError(t, err)

			// Assert
			accountAddr, err := new(felt.Felt).SetString(signer.OperationalAddress)
			require.NoError(t, err)

			privateKeyBigInt := big.NewInt(0x123)
			// This is the public key for private key "0x123"
			publicKey := "2443263864760624031255983690848140455871762770061978316256189704907682682390"
			ks := account.SetNewMemKeystore(publicKey, privateKeyBigInt)

			expectedValidatorAccount, accountErr := account.NewAccount(
				provider, accountAddr, publicKey, ks, 2,
			)
			require.NoError(t, accountErr)
			require.Equal(t, validator.InternalSigner(*expectedValidatorAccount), validatorAccount)

			require.Nil(t, err)
		})
	} else {
		t.Logf("Ignoring tests that require env variables: %s", err)
	}

	t.Run("Error: cannot create validator account", func(t *testing.T) {
		provider, providerErr := rpc.NewProvider("http://localhost:1234")
		require.NoError(t, providerErr)

		accountData := validator.Signer{
			PrivKey:            "0x123",
			OperationalAddress: "0x456",
		}
		validatorAccount, err := validator.NewInternalSigner(provider, logger, &accountData)

		require.Equal(t, validator.InternalSigner{}, validatorAccount)
		require.ErrorContains(t, err, "Cannot create validator account:")
	})
}

func TestNewExternalSigner(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	t.Run("Error getting chain ID", func(t *testing.T) {
		// Setup
		provider, providerErr := rpc.NewProvider("http://localhost:1234")
		require.NoError(t, providerErr)

		signer := validator.Signer{
			ExternalUrl:        "http://localhost:1234",
			OperationalAddress: "0x123",
		}
		externalSigner, err := validator.NewExternalSigner(provider, &signer)

		require.Zero(t, externalSigner)
		require.Error(t, err)
	})

	t.Run("Successful provider creation", func(t *testing.T) {
		// Setup
		env, err := loadEnv(t)
		if err != nil {
			t.Skipf("Ignoring tests that require env variables: %s", err)
		}

		provider, providerErr := rpc.NewProvider(env.httpProviderUrl)
		require.NoError(t, providerErr)

		signer := validator.Signer{
			ExternalUrl:        "http://localhost:1234",
			OperationalAddress: "0x123",
		}
		externalSigner, err := validator.NewExternalSigner(provider, &signer)

		// Expected chain ID from rpc provider at env.HTTP_PROVIDER_URL is "SN_SEPOLIA"
		expectedOpAddr := validator.AddressFromString(signer.OperationalAddress)
		expectedChainId := new(felt.Felt).SetBytes([]byte("SN_SEPOLIA"))
		expectedExternalSigner := validator.ExternalSigner{
			Provider:           provider,
			OperationalAddress: expectedOpAddr,
			Url:                signer.ExternalUrl,
			ChainId:            *expectedChainId,
		}
		require.Equal(t, expectedExternalSigner, externalSigner)
		require.Nil(t, err)
	})
}

func TestExternalSignerAddress(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	t.Run("Return signer address", func(t *testing.T) {
		address := "0x123"
		externalSigner := validator.ExternalSigner{
			OperationalAddress: validator.AddressFromString(address),
		}

		addrFelt := externalSigner.Address()

		require.Equal(t, address, addrFelt.String())
	})
}

func TestBuildAndSendInvokeTxn(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	t.Run("Error getting nonce", func(t *testing.T) {
		env, err := loadEnv(t)
		if err != nil {
			t.Skipf("Ignoring tests that require env variables: %s", err)
		}

		provider, providerErr := rpc.NewProvider(env.httpProviderUrl)
		require.NoError(t, providerErr)

		signer := validator.Signer{
			ExternalUrl:        "http://localhost:1234",
			OperationalAddress: "0x123",
		}
		externalSigner, err := validator.NewExternalSigner(provider, &signer)
		require.NoError(t, err)

		addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(
			context.Background(), []rpc.InvokeFunctionCall{}, validator.FEE_ESTIMATION_MULTIPLIER,
		)

		require.Nil(t, addInvokeTxRes)
		expectedError := rpc.RPCError{Code: 20, Message: "Contract not found"}
		require.Equal(t, expectedError.Error(), err.Error())
	})

	t.Run("Error signing transaction the first time (for estimating fee)", func(t *testing.T) {
		env, err := loadEnv(t)
		if err != nil {
			t.Skipf("Ignoring tests that require env variables: %s", err)
		}

		provider, providerErr := rpc.NewProvider(env.httpProviderUrl)
		require.NoError(t, providerErr)

		serverError := "some internal error"
		// Create a mock server
		mockServer := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					// Simulate API response
					http.Error(w, serverError, http.StatusInternalServerError)
				}))
		defer mockServer.Close()

		signer := validator.Signer{
			ExternalUrl:        mockServer.URL,
			OperationalAddress: "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e",
		}
		externalSigner, err := validator.NewExternalSigner(provider, &signer)
		require.NoError(t, err)

		addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(
			context.Background(), []rpc.InvokeFunctionCall{}, validator.FEE_ESTIMATION_MULTIPLIER,
		)

		require.Nil(t, addInvokeTxRes)
		expectedErrorMsg := fmt.Sprintf(
			"server error %d: %s", http.StatusInternalServerError, serverError,
		)
		require.EqualError(t, err, expectedErrorMsg)
	})

	t.Run("Error estimating fee", func(t *testing.T) {
		env, err := loadEnv(t)
		if err != nil {
			t.Skipf("Ignoring tests that require env variables: %s", err)
		}

		provider, providerErr := rpc.NewProvider(env.httpProviderUrl)
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

		signer := validator.Signer{
			ExternalUrl:        mockServer.URL,
			OperationalAddress: "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e",
		}
		externalSigner, err := validator.NewExternalSigner(provider, &signer)
		require.NoError(t, err)

		addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(context.Background(), []rpc.InvokeFunctionCall{}, validator.FEE_ESTIMATION_MULTIPLIER)

		require.Nil(t, addInvokeTxRes)
		require.Contains(t, err.Error(), "Account: invalid signature")
	})

	t.Run(
		"Error signing transaction the second time (for the actual invoke tx)",
		func(t *testing.T) {
			mockRpc := createMockRpcServer(t, nil)
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

			signer := validator.Signer{
				ExternalUrl:        mockSigner.URL,
				OperationalAddress: "0xabc",
			}
			externalSigner, err := validator.NewExternalSigner(provider, &signer)
			require.NoError(t, err)

			addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(
				context.Background(),
				[]rpc.InvokeFunctionCall{},
				validator.FEE_ESTIMATION_MULTIPLIER,
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
		mockRpc := createMockRpcServer(t, addInvoke)
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

		signer := validator.Signer{
			ExternalUrl:        mockSigner.URL,
			OperationalAddress: "0xabc",
		}
		externalSigner, err := validator.NewExternalSigner(provider, &signer)
		require.NoError(t, err)

		addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(context.Background(), []rpc.InvokeFunctionCall{}, validator.FEE_ESTIMATION_MULTIPLIER)

		require.Nil(t, addInvokeTxRes)
		expectedServerErr := fmt.Sprintf("%d Internal Server Error: %s", http.StatusInternalServerError, serverInternalError)
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
		mockRpc := createMockRpcServer(t, addInvoke)
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

		signer := validator.Signer{
			ExternalUrl:        mockSigner.URL,
			OperationalAddress: "0xabc",
		}
		externalSigner, err := validator.NewExternalSigner(provider, &signer)
		require.NoError(t, err)

		addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(context.Background(), []rpc.InvokeFunctionCall{}, validator.FEE_ESTIMATION_MULTIPLIER)

		require.Equal(t, &rpc.AddInvokeTransactionResponse{TransactionHash: utils.HexToFelt(t, expectedInvokeTxHash)}, addInvokeTxRes)
		require.Nil(t, err)
	})
}

func createMockRpcServer(
	t *testing.T, addInvoke func(w http.ResponseWriter, r *http.Request),
) *httptest.Server {
	t.Helper()

	mockRpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read and decode JSON body
		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		defer func() { require.NoError(t, r.Body.Close()) }()

		var req Method
		err = json.Unmarshal(bodyBytes, &req)
		require.NoError(t, err)

		switch req.Name {
		case "starknet_chainId":
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"jsonrpc": "2.0", "result": "0x1", "id": 1}`))
			require.NoError(t, err)
		case "starknet_getNonce":
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"jsonrpc": "2.0", "result": "0x2", "id": 1}`))
			require.NoError(t, err)
		case "starknet_estimateFee":
			mockFeeEstimate := []rpc.FeeEstimation{{
				L1GasConsumed:     utils.HexToFelt(t, "0x123"),
				L1GasPrice:        utils.HexToFelt(t, "0x456"),
				L2GasConsumed:     utils.HexToFelt(t, "0x123"),
				L2GasPrice:        utils.HexToFelt(t, "0x456"),
				L1DataGasConsumed: utils.HexToFelt(t, "0x123"),
				L1DataGasPrice:    utils.HexToFelt(t, "0x456"),
				OverallFee:        utils.HexToFelt(t, "0x123"),
				FeeUnit:           rpc.UnitStrk,
			}}
			feeEstBytes, err := json.Marshal(mockFeeEstimate)
			require.NoError(t, err)
			w.WriteHeader(http.StatusOK)
			_, err = fmt.Fprintf(
				w,
				`{"jsonrpc": "2.0", "result": %s, "id": 1}`,
				string(feeEstBytes),
			)
			require.NoError(t, err)
		case "starknet_addInvokeTransaction":
			addInvoke(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, err := w.Write([]byte(`Should not get here`))
			require.NoError(t, err)
		}
	}))

	return mockRpc
}

func TestSignInvokeTx(t *testing.T) {
	t.Run("Error hashing tx", func(t *testing.T) {
		invokeTx := rpc.InvokeTxnV3{}
		err := validator.SignInvokeTx(&invokeTx, &felt.Felt{}, "url not getting called anyway")

		require.Equal(t, ([]*felt.Felt)(nil), invokeTx.Signature)
		require.EqualError(t, err, "not all neccessary parameters have been set")
	})

	t.Run("Error signing tx", func(t *testing.T) {
		invokeTx := rpc.InvokeTxnV3{
			Type:          rpc.TransactionType_Invoke,
			SenderAddress: utils.HexToFelt(t, "0x123"),
			Calldata:      []*felt.Felt{utils.HexToFelt(t, "0x456")},
			Version:       rpc.TransactionV3,
			Signature:     []*felt.Felt{},
			Nonce:         utils.HexToFelt(t, "0x1"),
			ResourceBounds: rpc.ResourceBoundsMapping{
				L1Gas:     rpc.ResourceBounds{MaxAmount: "0x1", MaxPricePerUnit: "0x1"},
				L2Gas:     rpc.ResourceBounds{MaxAmount: "0x1", MaxPricePerUnit: "0x1"},
				L1DataGas: rpc.ResourceBounds{MaxAmount: "0x1", MaxPricePerUnit: "0x1"},
			},
			Tip:                   "0x0",
			PayMasterData:         []*felt.Felt{},
			AccountDeploymentData: []*felt.Felt{},
			NonceDataMode:         rpc.DAModeL1,
			FeeMode:               rpc.DAModeL1,
		}

		serverError := "some internal error"
		// Create a mock server
		mockServer := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					// Simulate API response
					http.Error(w, serverError, http.StatusInternalServerError)
				}))
		defer mockServer.Close()

		err := validator.SignInvokeTx(&invokeTx, &felt.Felt{}, mockServer.URL)

		require.Equal(t, []*felt.Felt{}, invokeTx.Signature)
		expectedErrorMsg := fmt.Sprintf(
			"server error %d: %s", http.StatusInternalServerError, serverError,
		)
		require.EqualError(t, err, expectedErrorMsg)
	})

	t.Run("Successful signing", func(t *testing.T) {
		invokeTx := rpc.InvokeTxnV3{
			Type:          rpc.TransactionType_Invoke,
			SenderAddress: new(felt.Felt).SetUint64(0xabc),
			Calldata: []*felt.Felt{
				new(felt.Felt).SetUint64(0xcba),
			},
			Version:   rpc.TransactionV3,
			Signature: []*felt.Felt{},
			Nonce:     utils.HexToFelt(t, "0x1"),
			ResourceBounds: rpc.ResourceBoundsMapping{
				L1Gas:     rpc.ResourceBounds{MaxAmount: "0x1", MaxPricePerUnit: "0x1"},
				L2Gas:     rpc.ResourceBounds{MaxAmount: "0x1", MaxPricePerUnit: "0x1"},
				L1DataGas: rpc.ResourceBounds{MaxAmount: "0x1", MaxPricePerUnit: "0x1"},
			},
			Tip:                   "0x0",
			PayMasterData:         []*felt.Felt{},
			AccountDeploymentData: []*felt.Felt{},
			NonceDataMode:         rpc.DAModeL1,
			FeeMode:               rpc.DAModeL1,
		}

		expectedTxHash, err := hash.TransactionHashInvokeV3(&invokeTx, &felt.Zero)
		require.NoError(t, err)

		sigR := new(felt.Felt).SetUint64(0x123)
		sigS := new(felt.Felt).SetUint64(0x456)

		mockServer := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					t.Helper()

					// Simulate API response
					w.WriteHeader(http.StatusOK)

					// Read and decode JSON body
					bodyBytes, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					defer require.NoError(t, r.Body.Close())

					var request signer.Request
					err = json.Unmarshal(bodyBytes, &request)
					require.NoError(t, err)

					// Making sure received hash is the expected one
					require.Equal(t, *expectedTxHash, request.Hash)

					_, err = fmt.Fprintf(w, `{"signature": ["%s", "%s"]}`, sigR, sigS)
					require.NoError(t, err)
				}))
		defer mockServer.Close()

		err = validator.SignInvokeTx(&invokeTx, &felt.Felt{}, mockServer.URL)
		require.NoError(t, err)

		expectedSignature := []*felt.Felt{sigR, sigS}
		require.Equal(t, expectedSignature, invokeTx.Signature)
	})
}

func TestFetchEpochInfo(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)

	// expected hash of `get_attestation_info_by_operational_address`
	expectedAttestInfoEntrypointHash := utils.HexToFelt(t, "0x172b481b04bae5fa5a77efcc44b1aca0a47c83397a952d3dd1c42357575db9f")

	t.Run("Return error: contract internal error", func(t *testing.T) {
		validatorOperationalAddress := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(validatorOperationalAddress)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestInfoEntrypointHash,
			Calldata:           []*felt.Felt{validatorOperationalAddress},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		epochInfo, err := validator.FetchEpochInfo(mockAccount)

		require.Equal(t, validator.EpochInfo{}, epochInfo)
		require.Equal(t, errors.New("Error when calling entrypoint `get_attestation_info_by_operational_address`: some contract error"), err)
	})

	t.Run("Return error: wrong contract response length", func(t *testing.T) {
		validatorOperationalAddress := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(validatorOperationalAddress)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestInfoEntrypointHash,
			Calldata:           []*felt.Felt{validatorOperationalAddress},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{new(felt.Felt).SetUint64(1)}, nil)

		epochInfo, err := validator.FetchEpochInfo(mockAccount)

		require.Equal(t, validator.EpochInfo{}, epochInfo)
		require.Equal(t, errors.New("Invalid response from entrypoint `get_attestation_info_by_operational_address`"), err)
	})

	t.Run("Successful contract call", func(t *testing.T) {
		validatorOperationalAddress := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(validatorOperationalAddress)
		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestInfoEntrypointHash,
			Calldata:           []*felt.Felt{validatorOperationalAddress},
		}

		// 18446744073709551616 is 1 above math.MaxUint64, which is equivalent to: 0x10000000000000000
		stakeBigInt, worked := new(big.Int).SetString("18446744073709551616", 10)
		require.True(t, worked)
		stake := new(felt.Felt).SetBigInt(stakeBigInt)

		stakerAddress := utils.HexToFelt(t, "0x456")
		epochLen := new(felt.Felt).SetUint64(40)
		epochId := new(felt.Felt).SetUint64(1516)
		currentEpochStartingBlock := new(felt.Felt).SetUint64(639270)

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(
				[]*felt.Felt{stakerAddress, stake, epochLen, epochId, currentEpochStartingBlock},
				nil,
			)

		epochInfo, err := validator.FetchEpochInfo(mockAccount)

		require.Equal(t, validator.EpochInfo{
			StakerAddress:             validator.Address(*stakerAddress),
			Stake:                     uint128.New(0, 1), // the 1st 64 bits are all 0 as it's MaxUint64 + 1
			EpochLen:                  40,
			EpochId:                   1516,
			CurrentEpochStartingBlock: validator.BlockNumber(639270),
		}, epochInfo)

		require.Nil(t, err)
	})
}

func TestFetchAttestWindow(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)

	// expected hash of `attestation_window`
	expectedAttestWindowEntrypointHash := utils.HexToFelt(t, "0x821e1f8dcf2ef7b00b980fd8f2e0761838cfd3b2328bd8494d6985fc3e910c")

	t.Run("Return error: contract internal error", func(t *testing.T) {
		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestWindowEntrypointHash,
			Calldata:           []*felt.Felt{},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		window, err := validator.FetchAttestWindow(mockAccount)

		require.Equal(t, uint64(0), window)
		require.Equal(t, errors.New("Error when calling entrypoint `attestation_window`: some contract error"), err)
	})

	t.Run("Return error: wrong contract response length", func(t *testing.T) {
		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestWindowEntrypointHash,
			Calldata:           []*felt.Felt{},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{}, nil)

		window, err := validator.FetchAttestWindow(mockAccount)

		require.Equal(t, uint64(0), window)
		require.Equal(t, errors.New("Invalid response from entrypoint `attestation_window`"), err)
	})

	t.Run("Successful contract call", func(t *testing.T) {
		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestWindowEntrypointHash,
			Calldata:           []*felt.Felt{},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{new(felt.Felt).SetUint64(16)}, nil)

		window, err := validator.FetchAttestWindow(mockAccount)

		require.Equal(t, uint64(16), window)
		require.Nil(t, err)
	})
}

func TestFetchValidatorBalance(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)

	// expected hash of `balanceOf`
	expectedBalanceOfEntrypointHash := utils.HexToFelt(t, "0x2e4263afad30923c891518314c3c95dbe830a16874e8abc5777a9a20b54c76e")

	t.Run("Return error: contract internal error", func(t *testing.T) {
		addr := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(addr)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.STRK_CONTRACT_ADDRESS),
			EntryPointSelector: expectedBalanceOfEntrypointHash,
			Calldata:           []*felt.Felt{addr},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		balance, err := validator.FetchValidatorBalance(mockAccount)

		require.Equal(t, validator.Balance(felt.Zero), balance)
		require.Equal(t, errors.New("Error when calling entrypoint `balanceOf`: some contract error"), err)
	})

	t.Run("Return error: wrong contract response length", func(t *testing.T) {
		addr := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(addr)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.STRK_CONTRACT_ADDRESS),
			EntryPointSelector: expectedBalanceOfEntrypointHash,
			Calldata:           []*felt.Felt{addr},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{}, nil)

		balance, err := validator.FetchValidatorBalance(mockAccount)

		require.Equal(t, validator.Balance(felt.Zero), balance)
		require.Equal(t, errors.New("Invalid response from entrypoint `balanceOf`"), err)
	})

	t.Run("Successful contract call", func(t *testing.T) {
		addr := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(addr)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.STRK_CONTRACT_ADDRESS),
			EntryPointSelector: expectedBalanceOfEntrypointHash,
			Calldata:           []*felt.Felt{addr},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{new(felt.Felt).SetUint64(1)}, nil)

		balance, err := validator.FetchValidatorBalance(mockAccount)

		require.Equal(t, validator.Balance(*new(felt.Felt).SetUint64(1)), balance)
		require.Nil(t, err)
	})
}

func TestFetchEpochAndAttestInfo(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)
	logger := utils.NewNopZapLogger()

	t.Run("Return error: fetching epoch info error", func(t *testing.T) {
		validatorOperationalAddress := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(validatorOperationalAddress)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("get_attestation_info_by_operational_address"),
			Calldata:           []*felt.Felt{validatorOperationalAddress},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		epochInfo, attestInfo, err := validator.FetchEpochAndAttestInfo(mockAccount, logger)

		require.Equal(t, validator.EpochInfo{}, epochInfo)
		require.Equal(t, validator.AttestInfo{}, attestInfo)
		require.Equal(t, errors.New("Error when calling entrypoint `get_attestation_info_by_operational_address`: some contract error"), err)
	})

	t.Run("Return error: fetching window error", func(t *testing.T) {
		validatorOperationalAddress := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(validatorOperationalAddress)

		expectedEpochInfoFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("get_attestation_info_by_operational_address"),
			Calldata:           []*felt.Felt{validatorOperationalAddress},
		}

		epochLength := uint64(3)
		epochId := uint64(4)
		epochStartingBlock := uint64(5)
		mockAccount.
			EXPECT().
			Call(context.Background(), expectedEpochInfoFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{
				new(felt.Felt).SetUint64(1),
				new(felt.Felt).SetUint64(2),
				new(felt.Felt).SetUint64(epochLength),
				new(felt.Felt).SetUint64(epochId),
				new(felt.Felt).SetUint64(epochStartingBlock),
			}, nil)

		expectedWindowFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("attestation_window"),
			Calldata:           []*felt.Felt{},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedWindowFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		epochInfo, attestInfo, err := validator.FetchEpochAndAttestInfo(mockAccount, logger)

		require.Equal(t, validator.EpochInfo{}, epochInfo)
		require.Equal(t, validator.AttestInfo{}, attestInfo)
		require.Equal(t, errors.New("Error when calling entrypoint `attestation_window`: some contract error"), err)
	})

	t.Run("Successfully fetch & compute info", func(t *testing.T) {
		// Setup

		// Mock fetchEpochInfo call
		validatorOperationalAddress := utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		mockAccount.EXPECT().Address().Return(validatorOperationalAddress)

		stakerAddress := utils.HexToFelt(t, "0x123") // does not matter, is not used anyway
		stake := uint64(1000000000000000000)
		epochLen := uint64(40)
		epochId := uint64(1516)
		epochStartingBlock := uint64(639270)

		expectedEpochInfoFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("get_attestation_info_by_operational_address"),
			Calldata:           []*felt.Felt{validatorOperationalAddress},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedEpochInfoFnCall, rpc.BlockID{Tag: "latest"}).
			Return(
				[]*felt.Felt{
					stakerAddress,
					new(felt.Felt).SetUint64(stake),
					new(felt.Felt).SetUint64(epochLen),
					new(felt.Felt).SetUint64(epochId),
					new(felt.Felt).SetUint64(epochStartingBlock),
				},
				nil,
			)

		// Mock fetchAttestWindow call
		expectedWindowFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, validator.ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("attestation_window"),
			Calldata:           []*felt.Felt{},
		}

		attestWindow := uint64(16)
		mockAccount.
			EXPECT().
			Call(context.Background(), expectedWindowFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{new(felt.Felt).SetUint64(attestWindow)}, nil)

		// Mock ComputeBlockNumberToAttestTo call
		mockAccount.EXPECT().Address().Return(validatorOperationalAddress)

		expectedTargetBlock := validator.BlockNumber(639291)
		expectedAttestInfo := validator.AttestInfo{
			TargetBlock: expectedTargetBlock,
			WindowStart: expectedTargetBlock + validator.BlockNumber(validator.MIN_ATTESTATION_WINDOW),
			WindowEnd:   expectedTargetBlock + validator.BlockNumber(attestWindow),
		}

		// Test
		epochInfo, attestInfo, err := validator.FetchEpochAndAttestInfo(mockAccount, logger)

		// Assert
		expectedEpochInfo := validator.EpochInfo{
			StakerAddress:             validator.Address(*stakerAddress),
			Stake:                     uint128.From64(stake),
			EpochLen:                  epochLen,
			EpochId:                   epochId,
			CurrentEpochStartingBlock: validator.BlockNumber(epochStartingBlock),
		}

		require.Equal(t, expectedEpochInfo, epochInfo)
		require.Equal(t, expectedAttestInfo, attestInfo)
		require.Nil(t, err)
	})
}

func TestInvokeAttest(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)

	t.Run("Return error", func(t *testing.T) {
		blockHash := new(felt.Felt).SetUint64(123)

		expectedFnCall := []rpc.InvokeFunctionCall{{
			ContractAddress: utils.HexToFelt(t, validator.ATTEST_CONTRACT_ADDRESS),
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHash},
		}}

		mockAccount.
			EXPECT().
			BuildAndSendInvokeTxn(context.Background(), expectedFnCall, validator.FEE_ESTIMATION_MULTIPLIER).
			Return(nil, errors.New("some sending error"))

		attestRequired := validator.AttestRequired{BlockHash: validator.BlockHash(*blockHash)}
		invokeRes, err := validator.InvokeAttest(mockAccount, &attestRequired)

		require.Nil(t, invokeRes)
		require.EqualError(t, err, "some sending error")
	})

	t.Run("Invoke tx successfully sent", func(t *testing.T) {
		blockHash := new(felt.Felt).SetUint64(123)

		expectedFnCall := []rpc.InvokeFunctionCall{{
			ContractAddress: utils.HexToFelt(t, validator.ATTEST_CONTRACT_ADDRESS),
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHash},
		}}

		response := rpc.AddInvokeTransactionResponse{
			TransactionHash: utils.HexToFelt(t, "0x123"),
		}
		mockAccount.
			EXPECT().
			BuildAndSendInvokeTxn(context.Background(), expectedFnCall, validator.FEE_ESTIMATION_MULTIPLIER).
			Return(&response, nil)

		attestRequired := validator.AttestRequired{BlockHash: validator.BlockHash(*blockHash)}
		invokeRes, err := validator.InvokeAttest(mockAccount, &attestRequired)

		require.Equal(t, &response, invokeRes)
		require.Nil(t, err)
	})
}
