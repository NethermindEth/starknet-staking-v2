package signer_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	signerP "github.com/NethermindEth/starknet-staking-v2/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/rpc"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"lukechampine.com/uint128"
)

func TestNewInternalSigner(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	logger := utils.NewNopZapLogger()
	envVars, envVarsErr := validator.LoadEnv(t)
	loadedEnvVars := envVarsErr == nil

	contractAddresses := new(config.ContractAddresses).SetDefaults("SN_SEPOLIA")

	t.Run("Error: private key conversion", func(t *testing.T) {
		if !loadedEnvVars {
			t.Skipf("couldn't load env vars: %s", envVarsErr.Error())
		}

		provider, providerErr := rpc.NewProvider(envVars.HttpProviderUrl)
		require.NoError(t, providerErr)

		validatorAccount, err := signer.NewInternalSigner(
			provider, logger, &config.Signer{}, contractAddresses,
		)

		require.Equal(t, signer.InternalSigner{}, validatorAccount)
		expectedErrorMsg := fmt.Sprintf(
			"cannot turn private key %s into a big int", (*big.Int)(nil),
		)
		require.Equal(t, expectedErrorMsg, err.Error())
	})
	t.Run("Successful account creation", func(t *testing.T) {
		if !loadedEnvVars {
			t.Skipf("couldn't load env vars: %s", envVarsErr.Error())
		}

		provider, err := rpc.NewProvider(envVars.HttpProviderUrl)
		require.NoError(t, err)

		configSigner := config.Signer{
			PrivKey:            "0x123",
			OperationalAddress: "0x456",
		}

		// Test
		logger := utils.NewNopZapLogger()
		internalSigner, err := signer.NewInternalSigner(
			provider, logger, &configSigner, contractAddresses,
		)
		require.NoError(t, err)

		// Assert
		accountAddr, err := new(felt.Felt).SetString(configSigner.OperationalAddress)
		require.NoError(t, err)

		privateKeyBigInt := big.NewInt(0x123)
		// This is the public key for private key "0x123"
		publicKey := "2443263864760624031255983690848140455871762770061978316256189704907682682390"
		ks := account.SetNewMemKeystore(publicKey, privateKeyBigInt)

		expectedAccount, err := account.NewAccount(
			provider, accountAddr, publicKey, ks, 2,
		)

		require.NoError(t, err)
		require.Equal(t, expectedAccount, &internalSigner.Account)
		require.Equal(
			t, validator.SepoliaValidationContracts(t), internalSigner.ValidationContracts(),
		)
	})

	t.Run("Error: cannot create validator account", func(t *testing.T) {
		provider, providerErr := rpc.NewProvider("http://localhost:1234")
		require.NoError(t, providerErr)

		validatorAccount, err := signer.NewInternalSigner(
			provider,
			logger,
			&config.Signer{
				PrivKey:            "0x123",
				OperationalAddress: "0x456",
			},
			contractAddresses,
		)

		require.Equal(t, signer.InternalSigner{}, validatorAccount)
		require.ErrorContains(t, err, "cannot create validator account:")
	})
}

func createMockRPCServer(
	t *testing.T, addInvoke func(w http.ResponseWriter, r *http.Request),
) *httptest.Server {
	t.Helper()

	mockRpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read and decode JSON body
		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		defer func() { require.NoError(t, r.Body.Close()) }()

		var req validator.Method
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

		err := signer.SignInvokeTx(&invokeTx, &felt.Felt{}, mockServer.URL)

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

		chainID := new(felt.Felt).SetUint64(1)

		sigR := new(felt.Felt).SetUint64(0x123)
		sigS := new(felt.Felt).SetUint64(0x456)

		mockServer := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					t.Helper()

					// Simulate API response
					w.WriteHeader(http.StatusOK)

					bodyBytes, err := io.ReadAll(r.Body)
					require.NoError(t, err)

					var req signerP.Request
					err = json.Unmarshal(bodyBytes, &req)
					require.NoError(t, err)

					// Making sure received tx and chainId are the expected ones
					require.Equal(t, &invokeTx, req.InvokeTxnV3)
					require.Equal(t, chainID, req.ChainId)

					_, err = fmt.Fprintf(w, `{"signature": ["%s", "%s"]}`, sigR, sigS)
					require.NoError(t, err)
				}))
		defer mockServer.Close()

		err := signer.SignInvokeTx(&invokeTx, chainID, mockServer.URL)

		expectedSignature := []*felt.Felt{sigR, sigS}
		require.Equal(t, expectedSignature, invokeTx.Signature)

		require.NoError(t, err)
	})
}

func TestFetchEpochInfo(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockSigner := mocks.NewMockSigner(mockCtrl)

	// expected hash of `get_attestation_info_by_operational_address`
	expectedAttestInfoEntrypointHash := utils.HexToFelt(
		t, "0x172b481b04bae5fa5a77efcc44b1aca0a47c83397a952d3dd1c42357575db9f",
	)

	t.Run("Return error: contract internal error", func(t *testing.T) {
		validatorOperationalAddress := types.AddressFromString("0x123")

		mockSigner.EXPECT().Address().Return(&validatorOperationalAddress)

		mockSigner.EXPECT().ValidationContracts().Return(
			validator.SepoliaValidationContracts(t),
		).Times(1)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, constants.SEPOLIA_STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestInfoEntrypointHash,
			Calldata:           []*felt.Felt{validatorOperationalAddress.Felt()},
		}

		mockSigner.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		epochInfo, err := signer.FetchEpochInfo(mockSigner)

		require.Equal(t, validator.EpochInfo{}, epochInfo)
		require.ErrorContains(t, err, "some contract error")
	})

	t.Run("Return error: wrong contract response length", func(t *testing.T) {
		validatorOperationalAddress := types.AddressFromString("0x123")

		mockSigner.EXPECT().Address().Return(&validatorOperationalAddress)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, constants.SEPOLIA_STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestInfoEntrypointHash,
			Calldata:           []*felt.Felt{validatorOperationalAddress.Felt()},
		}
		mockSigner.EXPECT().ValidationContracts().Return(
			validator.SepoliaValidationContracts(t),
		).Times(1)

		mockSigner.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{new(felt.Felt).SetUint64(1)}, nil)

		epochInfo, err := signer.FetchEpochInfo(mockSigner)

		require.Equal(t, validator.EpochInfo{}, epochInfo)
		require.Equal(
			t,
			errors.New(
				"Invalid response from entrypoint `get_attestation_info_by_operational_address`",
			),
			err,
		)
	})

	t.Run("Successful contract call", func(t *testing.T) {
		validatorOperationalAddress := types.AddressFromString("0x123")

		mockSigner.EXPECT().Address().Return(&validatorOperationalAddress)
		mockSigner.EXPECT().ValidationContracts().Return(
			validator.SepoliaValidationContracts(t),
		).Times(1)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, constants.SEPOLIA_STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestInfoEntrypointHash,
			Calldata:           []*felt.Felt{validatorOperationalAddress.Felt()},
		}

		// 18446744073709551616 is 1 above math.MaxUint64 which is equivalent to:
		// 0x10000000000000000
		stakeBigInt, worked := new(big.Int).SetString("18446744073709551616", 10)
		require.True(t, worked)
		stake := new(felt.Felt).SetBigInt(stakeBigInt)

		stakerAddress := utils.HexToFelt(t, "0x456")
		epochLen := new(felt.Felt).SetUint64(40)
		epochID := new(felt.Felt).SetUint64(1516)
		currentEpochStartingBlock := new(felt.Felt).SetUint64(639270)

		mockSigner.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(
				[]*felt.Felt{stakerAddress, stake, epochLen, epochID, currentEpochStartingBlock},
				nil,
			)

		epochInfo, err := signer.FetchEpochInfo(mockSigner)

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

	mockSigner := mocks.NewMockSigner(mockCtrl)

	// expected hash of `attestation_window`
	expectedAttestWindowEntrypointHash := utils.HexToFelt(
		t, "0x821e1f8dcf2ef7b00b980fd8f2e0761838cfd3b2328bd8494d6985fc3e910c",
	)

	t.Run("Return error: contract internal error", func(t *testing.T) {
		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, constants.SEPOLIA_ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestWindowEntrypointHash,
			Calldata:           []*felt.Felt{},
		}

		mockSigner.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		mockSigner.EXPECT().ValidationContracts().Return(
			validator.SepoliaValidationContracts(t),
		).Times(1)

		window, err := signer.FetchAttestWindow(mockSigner)

		require.Equal(t, uint64(0), window)
		require.Equal(t, errors.New("Error when calling entrypoint `attestation_window`: some contract error"), err)
	})

	t.Run("Return error: wrong contract response length", func(t *testing.T) {
		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, constants.SEPOLIA_ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestWindowEntrypointHash,
			Calldata:           []*felt.Felt{},
		}

		mockSigner.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{}, nil)

		mockSigner.EXPECT().ValidationContracts().Return(
			validator.SepoliaValidationContracts(t),
		).Times(1)

		window, err := signer.FetchAttestWindow(mockSigner)

		require.Equal(t, uint64(0), window)
		require.Equal(t, errors.New("Invalid response from entrypoint `attestation_window`"), err)
	})

	t.Run("Successful contract call", func(t *testing.T) {
		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, constants.SEPOLIA_ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestWindowEntrypointHash,
			Calldata:           []*felt.Felt{},
		}

		mockSigner.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{new(felt.Felt).SetUint64(16)}, nil)

		mockSigner.EXPECT().ValidationContracts().Return(
			validator.SepoliaValidationContracts(t),
		).Times(1)

		window, err := signer.FetchAttestWindow(mockSigner)

		require.Equal(t, uint64(16), window)
		require.Nil(t, err)
	})
}

func TestFetchValidatorBalance(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockSigner := mocks.NewMockSigner(mockCtrl)

	// expected hash of `balanceOf`
	expectedBalanceOfEntrypointHash := utils.HexToFelt(
		t, "0x2e4263afad30923c891518314c3c95dbe830a16874e8abc5777a9a20b54c76e",
	)

	t.Run("Return error: contract internal error", func(t *testing.T) {
		addr := types.AddressFromString("0x123")

		mockSigner.EXPECT().Address().Return(&addr)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, constants.STRK_CONTRACT_ADDRESS),
			EntryPointSelector: expectedBalanceOfEntrypointHash,
			Calldata:           []*felt.Felt{addr.Felt()},
		}

		mockSigner.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		balance, err := signer.FetchValidatorBalance(mockSigner)

		require.Equal(t, types.Balance(felt.Zero), balance)
		require.Equal(t, errors.New("Error when calling entrypoint `balanceOf`: some contract error"), err)
	})

	t.Run("Return error: wrong contract response length", func(t *testing.T) {
		addr := types.AddressFromString("0x123")

		mockSigner.EXPECT().Address().Return(&addr)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, constants.STRK_CONTRACT_ADDRESS),
			EntryPointSelector: expectedBalanceOfEntrypointHash,
			Calldata:           []*felt.Felt{addr.Felt()},
		}

		mockSigner.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{}, nil)

		balance, err := signer.FetchValidatorBalance(mockSigner)

		require.Equal(t, validator.Balance(felt.Zero), balance)
		require.Equal(t, errors.New("Invalid response from entrypoint `balanceOf`"), err)
	})

	t.Run("Successful contract call", func(t *testing.T) {
		addr := types.AddressFromString("0x123")

		mockSigner.EXPECT().Address().Return(&addr)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, constants.STRK_CONTRACT_ADDRESS),
			EntryPointSelector: expectedBalanceOfEntrypointHash,
			Calldata:           []*felt.Felt{addr.Felt()},
		}

		mockSigner.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{new(felt.Felt).SetUint64(1)}, nil)

		balance, err := signer.FetchValidatorBalance(mockSigner)

		require.Equal(t, validator.Balance(*new(felt.Felt).SetUint64(1)), balance)
		require.Nil(t, err)
	})
}

func TestFetchEpochAndAttestInfo(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockSigner := mocks.NewMockSigner(mockCtrl)
	logger := utils.NewNopZapLogger()

	t.Run("Return error: fetching epoch info error", func(t *testing.T) {
		validatorOperationalAddress := types.AddressFromString("0x123")

		mockSigner.EXPECT().Address().Return(&validatorOperationalAddress)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress: utils.HexToFelt(t, constants.SEPOLIA_STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt(
				"get_attestation_info_by_operational_address",
			),
			Calldata: []*felt.Felt{validatorOperationalAddress.Felt()},
		}

		mockSigner.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		mockSigner.EXPECT().ValidationContracts().Return(
			validator.SepoliaValidationContracts(t),
		).Times(1)

		epochInfo, attestInfo, err := signer.FetchEpochAndAttestInfo(mockSigner, logger)

		require.Equal(t, signer.EpochInfo{}, epochInfo)
		require.Equal(t, signer.AttestInfo{}, attestInfo)
		require.ErrorContains(t, err, "some contract error")
	})

	t.Run("Return error: fetching window error", func(t *testing.T) {
		validatorOperationalAddress := types.AddressFromString("0x123")

		mockSigner.EXPECT().Address().Return(&validatorOperationalAddress)

		mockSigner.EXPECT().ValidationContracts().Return(
			validator.SepoliaValidationContracts(t),
		).Times(2)

		expectedEpochInfoFnCall := rpc.FunctionCall{
			ContractAddress: utils.HexToFelt(t, constants.SEPOLIA_STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt(
				"get_attestation_info_by_operational_address",
			),
			Calldata: []*felt.Felt{validatorOperationalAddress.Felt()},
		}

		epochLength := uint64(3)
		epochID := uint64(4)
		epochStartingBlock := uint64(5)
		mockSigner.
			EXPECT().
			Call(context.Background(), expectedEpochInfoFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{
				new(felt.Felt).SetUint64(1),
				new(felt.Felt).SetUint64(2),
				new(felt.Felt).SetUint64(epochLength),
				new(felt.Felt).SetUint64(epochID),
				new(felt.Felt).SetUint64(epochStartingBlock),
			}, nil)

		expectedWindowFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, constants.SEPOLIA_ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("attestation_window"),
			Calldata:           []*felt.Felt{},
		}

		mockSigner.
			EXPECT().
			Call(context.Background(), expectedWindowFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		epochInfo, attestInfo, err := signer.FetchEpochAndAttestInfo(mockSigner, logger)

		require.Equal(t, validator.EpochInfo{}, epochInfo)
		require.Equal(t, validator.AttestInfo{}, attestInfo)
		require.Equal(t, errors.New("Error when calling entrypoint `attestation_window`: some contract error"), err)
	})

	t.Run("Successfully fetch & compute info", func(t *testing.T) {
		// Setup

		// Mock fetchEpochInfo call
		validatorOperationalAddress := types.AddressFromString(
			"0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e",
		)
		mockSigner.EXPECT().Address().Return(&validatorOperationalAddress)

		mockSigner.EXPECT().ValidationContracts().Return(
			validator.SepoliaValidationContracts(t),
		).Times(2)

		stakerAddress := utils.HexToFelt(t, "0x123")
		stake := uint64(1000000000000000000)
		epochLen := uint64(40)
		epochID := uint64(1516)
		epochStartingBlock := uint64(639270)

		expectedEpochInfoFnCall := rpc.FunctionCall{
			ContractAddress: utils.HexToFelt(t, constants.SEPOLIA_STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt(
				"get_attestation_info_by_operational_address",
			),
			Calldata: []*felt.Felt{validatorOperationalAddress.Felt()},
		}

		mockSigner.
			EXPECT().
			Call(context.Background(), expectedEpochInfoFnCall, rpc.BlockID{Tag: "latest"}).
			Return(
				[]*felt.Felt{
					stakerAddress,
					new(felt.Felt).SetUint64(stake),
					new(felt.Felt).SetUint64(epochLen),
					new(felt.Felt).SetUint64(epochID),
					new(felt.Felt).SetUint64(epochStartingBlock),
				},
				nil,
			)

		// Mock fetchAttestWindow call
		expectedWindowFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, constants.SEPOLIA_ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("attestation_window"),
			Calldata:           []*felt.Felt{},
		}

		attestWindow := uint64(16)
		mockSigner.
			EXPECT().
			Call(context.Background(), expectedWindowFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{new(felt.Felt).SetUint64(attestWindow)}, nil)

		// Test
		epochInfo, attestInfo, err := signer.FetchEpochAndAttestInfo(mockSigner, logger)

		// Assert
		expectedEpochInfo := validator.EpochInfo{
			StakerAddress:             validator.Address(*stakerAddress),
			Stake:                     uint128.From64(stake),
			EpochLen:                  epochLen,
			EpochId:                   epochID,
			CurrentEpochStartingBlock: validator.BlockNumber(epochStartingBlock),
		}
		require.Equal(t, expectedEpochInfo, epochInfo)

		expectedTargetBlock := validator.BlockNumber(639276)
		expectedAttestInfo := validator.AttestInfo{
			TargetBlock: expectedTargetBlock,
			WindowStart: expectedTargetBlock + signer.BlockNumber(constants.MIN_ATTESTATION_WINDOW),
			WindowEnd:   expectedTargetBlock + signer.BlockNumber(attestWindow),
		}
		require.Equal(t, expectedAttestInfo, attestInfo)

		require.Nil(t, err)
	})
}

func TestInvokeAttest(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockSigner := mocks.NewMockSigner(mockCtrl)

	t.Run("Return error", func(t *testing.T) {
		blockHash := new(felt.Felt).SetUint64(123)

		expectedFnCall := []rpc.InvokeFunctionCall{{
			ContractAddress: utils.HexToFelt(t, constants.SEPOLIA_ATTEST_CONTRACT_ADDRESS),
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHash},
		}}

		mockSigner.
			EXPECT().
			BuildAndSendInvokeTxn(
				context.Background(), expectedFnCall, constants.FEE_ESTIMATION_MULTIPLIER,
			).
			Return(nil, errors.New("some sending error"))

		mockSigner.EXPECT().ValidationContracts().Return(
			validator.SepoliaValidationContracts(t),
		).Times(1)

		attestRequired := signer.AttestRequired{BlockHash: validator.BlockHash(*blockHash)}
		invokeRes, err := signer.InvokeAttest(mockSigner, &attestRequired)

		require.Nil(t, invokeRes)
		require.EqualError(t, err, "some sending error")
	})

	t.Run("Invoke tx successfully sent", func(t *testing.T) {
		blockHash := new(felt.Felt).SetUint64(123)

		expectedFnCall := []rpc.InvokeFunctionCall{{
			ContractAddress: utils.HexToFelt(t, constants.SEPOLIA_ATTEST_CONTRACT_ADDRESS),
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHash},
		}}

		response := rpc.AddInvokeTransactionResponse{
			TransactionHash: utils.HexToFelt(t, "0x123"),
		}
		mockSigner.
			EXPECT().
			BuildAndSendInvokeTxn(
				context.Background(),
				expectedFnCall,
				constants.FEE_ESTIMATION_MULTIPLIER,
			).
			Return(&response, nil)

		mockSigner.EXPECT().ValidationContracts().Return(
			validator.SepoliaValidationContracts(t),
		).Times(1)

		attestRequired := signer.AttestRequired{BlockHash: validator.BlockHash(*blockHash)}
		invokeRes, err := signer.InvokeAttest(mockSigner, &attestRequired)

		require.Equal(t, &response, invokeRes)
		require.Nil(t, err)
	})
}

func TestComputeBlockNumberToAttestTo(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	t.Run("Correct target block number computation - example 1", func(t *testing.T) {
		stakerAddress := utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		epochInfo := validator.EpochInfo{
			StakerAddress:             validator.Address(*stakerAddress),
			Stake:                     uint128.From64(1000000000000000000),
			EpochLen:                  40,
			EpochId:                   1516,
			CurrentEpochStartingBlock: validator.BlockNumber(639270),
		}
		attestationWindow := uint64(16)

		blockNumber := signer.ComputeBlockNumberToAttestTo(&epochInfo, attestationWindow)

		require.Equal(t, signer.BlockNumber(639291), blockNumber)
	})

	t.Run("Correct target block number computation - example 2", func(t *testing.T) {
		stakerAddress := utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		epochInfo := validator.EpochInfo{
			StakerAddress:             validator.Address(*stakerAddress),
			Stake:                     uint128.From64(1000000000000000000),
			EpochLen:                  40,
			EpochId:                   1517,
			CurrentEpochStartingBlock: validator.BlockNumber(639310),
		}
		attestationWindow := uint64(16)

		blockNumber := signer.ComputeBlockNumberToAttestTo(&epochInfo, attestationWindow)
		require.Equal(t, validator.BlockNumber(639316), blockNumber)
	})

	t.Run("Correct target block number computation - example 3", func(t *testing.T) {
		stakerAddress := utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		epochInfo := validator.EpochInfo{
			StakerAddress:             validator.Address(*stakerAddress),
			Stake:                     uint128.From64(1000000000000000000),
			EpochLen:                  40,
			EpochId:                   1518,
			CurrentEpochStartingBlock: validator.BlockNumber(639350),
		}
		attestationWindow := uint64(16)

		blockNumber := signer.ComputeBlockNumberToAttestTo(&epochInfo, attestationWindow)

		require.Equal(t, validator.BlockNumber(639369), blockNumber)
	})
}
