package main_test

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
	main "github.com/NethermindEth/starknet-staking-v2"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
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

func NewAccountData(privKey string, address string) main.AccountData {
	return main.AccountData{
		PrivKey:            privKey,
		OperationalAddress: main.AddressFromString(address),
	}
}

func loadEnv(t *testing.T) envVariable {
	t.Helper()

	err := godotenv.Load(".env")
	if err != nil {
		panic(errors.Join(errors.New("error loading '.env' file"), err))
	}

	base := os.Getenv("HTTP_PROVIDER_URL")
	if base == "" {
		panic("Failed to load HTTP_PROVIDER_URL, empty string")
	}

	wsProviderUrl := os.Getenv("WS_PROVIDER_URL")
	if wsProviderUrl == "" {
		panic("Failed to load WS_PROVIDER_URL, empty string")
	}

	return envVariable{base, wsProviderUrl}
}

func TestNewValidatorAccount(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockLogger := mocks.NewMockLogger(mockCtrl)

	t.Run("Error: private key conversion", func(t *testing.T) {
		env := loadEnv(t)
		provider, providerErr := rpc.NewProvider(env.httpProviderUrl)
		require.NoError(t, providerErr)

		validatorAccount, err := main.NewValidatorAccount(provider, mockLogger, &main.AccountData{})

		require.Equal(t, main.ValidatorAccount{}, validatorAccount)
		expectedErrorMsg := fmt.Sprintf("Cannot turn private key %s into a big int", (*big.Int)(nil))
		require.Equal(t, expectedErrorMsg, err.Error())
	})

	t.Run("Error: cannot create validator account", func(t *testing.T) {
		provider, providerErr := rpc.NewProvider("http://localhost:1234")
		require.NoError(t, providerErr)

		privateKey := "0x123"
		address := "0x456"
		accountData := NewAccountData(privateKey, address)
		validatorAccount, err := main.NewValidatorAccount(provider, mockLogger, &accountData)

		require.Equal(t, main.ValidatorAccount{}, validatorAccount)
		require.ErrorContains(t, err, "Cannot create validator account:")
	})

	t.Run("Successful account creation", func(t *testing.T) {
		// Setup
		env := loadEnv(t)
		provider, providerErr := rpc.NewProvider(env.httpProviderUrl)
		require.NoError(t, providerErr)

		privateKey := "0x123"
		address := "0x456"
		accountData := NewAccountData(privateKey, address)

		mockLogger.EXPECT().Infow("Successfully created validator account", "address", address)

		// Test
		validatorAccount, err := main.NewValidatorAccount(provider, mockLogger, &accountData)

		// Assert
		accountAddrFelt, stringToFeltErr := new(felt.Felt).SetString(address)
		require.NoError(t, stringToFeltErr)

		privateKeyBigInt := big.NewInt(291) // 291 is "0x123" as int
		// This is the public key for private key "0x123"
		publicKey := "2443263864760624031255983690848140455871762770061978316256189704907682682390"
		ks := account.SetNewMemKeystore(publicKey, privateKeyBigInt)

		expectedValidatorAccount, accountErr := account.NewAccount(provider, accountAddrFelt, publicKey, ks, 2)
		require.NoError(t, accountErr)
		require.Equal(t, main.ValidatorAccount(*expectedValidatorAccount), validatorAccount)

		require.Nil(t, err)
	})
}

func TestNewExternalSigner(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	t.Run("Error getting chain ID", func(t *testing.T) {
		// Setup
		provider, providerErr := rpc.NewProvider("http://localhost:1234")
		require.NoError(t, providerErr)

		operationalAddress := main.AddressFromString("0x123")
		externalSignerUrl := "http://localhost:1234"
		externalSigner, err := main.NewExternalSigner(provider, operationalAddress, externalSignerUrl)

		require.Zero(t, externalSigner)
		require.Error(t, err)
	})

	t.Run("Successful provider creation", func(t *testing.T) {
		// Setup
		env := loadEnv(t)
		provider, providerErr := rpc.NewProvider(env.httpProviderUrl)
		require.NoError(t, providerErr)

		operationalAddress := main.AddressFromString("0x123")
		externalSignerUrl := "http://localhost:1234"
		externalSigner, err := main.NewExternalSigner(provider, operationalAddress, externalSignerUrl)

		// Expected chain ID from rpc provider at env.HTTP_PROVIDER_URL is "SN_SEPOLIA"
		expectedChainId := new(felt.Felt).SetBytes([]byte("SN_SEPOLIA"))
		expectedExternalSigner := main.ExternalSigner{
			Provider:           provider,
			OperationalAddress: operationalAddress,
			ExternalSignerUrl:  externalSignerUrl,
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
		externalSigner := main.ExternalSigner{
			OperationalAddress: main.AddressFromString(address),
		}

		addrFelt := externalSigner.Address()

		require.Equal(t, address, addrFelt.String())
	})
}

func TestBuildAndSendInvokeTxn(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	t.Run("Error getting nonce", func(t *testing.T) {
		env := loadEnv(t)

		provider, providerErr := rpc.NewProvider(env.httpProviderUrl)
		require.NoError(t, providerErr)

		operationalAddress := main.AddressFromString("0x123")
		externalSignerUrl := "http://localhost:1234"
		externalSigner, err := main.NewExternalSigner(provider, operationalAddress, externalSignerUrl)
		require.NoError(t, err)

		addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(context.Background(), []rpc.InvokeFunctionCall{}, main.FEE_ESTIMATION_MULTIPLIER)

		require.Nil(t, addInvokeTxRes)
		expectedError := rpc.RPCError{Code: 20, Message: "Contract not found"}
		require.Equal(t, expectedError.Error(), err.Error())
	})

	t.Run("Error signing transaction the first time (for estimating fee)", func(t *testing.T) {
		env := loadEnv(t)

		provider, providerErr := rpc.NewProvider(env.httpProviderUrl)
		require.NoError(t, providerErr)

		serverError := "some internal error"
		// Create a mock server
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate API response
			http.Error(w, serverError, http.StatusInternalServerError)
		}))
		defer mockServer.Close()

		operationalAddress := main.AddressFromString("0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		externalSigner, err := main.NewExternalSigner(provider, operationalAddress, mockServer.URL)
		require.NoError(t, err)

		addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(context.Background(), []rpc.InvokeFunctionCall{}, main.FEE_ESTIMATION_MULTIPLIER)

		require.Nil(t, addInvokeTxRes)
		expectedErrorMsg := fmt.Sprintf("Server error %d: %s", http.StatusInternalServerError, serverError)
		require.EqualError(t, err, expectedErrorMsg)
	})

	t.Run("Error estimating fee", func(t *testing.T) {
		env := loadEnv(t)

		provider, providerErr := rpc.NewProvider(env.httpProviderUrl)
		require.NoError(t, providerErr)

		// Create a mock server
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate API response
			w.Write([]byte(`{"signature": ["0x123", "0x456"]}`))
		}))
		defer mockServer.Close()

		operationalAddress := main.AddressFromString("0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		externalSigner, err := main.NewExternalSigner(provider, operationalAddress, mockServer.URL)
		require.NoError(t, err)

		addInvokeTxRes, err := externalSigner.BuildAndSendInvokeTxn(context.Background(), []rpc.InvokeFunctionCall{}, main.FEE_ESTIMATION_MULTIPLIER)

		require.Nil(t, addInvokeTxRes)
		require.Contains(t, err.Error(), "Account: invalid signature")
	})
}

func TestSignInvokeTx(t *testing.T) {

	t.Run("Error hashing tx", func(t *testing.T) {
		invokeTx := rpc.InvokeTxnV3{}
		err := main.SignInvokeTx(&invokeTx, &felt.Felt{}, "url not getting called anyway")

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
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate API response
			http.Error(w, serverError, http.StatusInternalServerError)
		}))
		defer mockServer.Close()

		err := main.SignInvokeTx(&invokeTx, &felt.Felt{}, mockServer.URL)

		require.Equal(t, []*felt.Felt{}, invokeTx.Signature)
		expectedErrorMsg := fmt.Sprintf("Server error %d: %s", http.StatusInternalServerError, serverError)
		require.EqualError(t, err, expectedErrorMsg)
	})

	t.Run("Successful signing", func(t *testing.T) {
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

		expectedTxHash, err := hash.TransactionHashInvokeV3(&invokeTx, &felt.Felt{})
		require.NoError(t, err)

		sigPart1 := "0x123"
		sigPart2 := "0x456"

		// Create a mock server
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate API response
			w.WriteHeader(http.StatusOK)

			// Read and decode JSON body
			bodyBytes, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			defer r.Body.Close()

			var req main.SignRequest
			err = json.Unmarshal(bodyBytes, &req)
			require.NoError(t, err)

			// Making sure received hash is the expected one
			require.Equal(t, expectedTxHash.String(), req.Hash)

			w.Write([]byte(fmt.Sprintf(`{"signature": ["%s", "%s"]}`, sigPart1, sigPart2)))
		}))
		defer mockServer.Close()

		err = main.SignInvokeTx(&invokeTx, &felt.Felt{}, mockServer.URL)

		expectedSignature := []*felt.Felt{utils.HexToFelt(t, sigPart1), utils.HexToFelt(t, sigPart2)}
		require.Equal(t, expectedSignature, invokeTx.Signature)
		require.NoError(t, err)
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
			ContractAddress:    utils.HexToFelt(t, main.STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestInfoEntrypointHash,
			Calldata:           []*felt.Felt{validatorOperationalAddress},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		epochInfo, err := main.FetchEpochInfo(mockAccount)

		require.Equal(t, main.EpochInfo{}, epochInfo)
		require.Equal(t, errors.New("Error when calling entrypoint `get_attestation_info_by_operational_address`: some contract error"), err)
	})

	t.Run("Return error: wrong contract response length", func(t *testing.T) {
		validatorOperationalAddress := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(validatorOperationalAddress)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, main.STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestInfoEntrypointHash,
			Calldata:           []*felt.Felt{validatorOperationalAddress},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{new(felt.Felt).SetUint64(1)}, nil)

		epochInfo, err := main.FetchEpochInfo(mockAccount)

		require.Equal(t, main.EpochInfo{}, epochInfo)
		require.Equal(t, errors.New("Invalid response from entrypoint `get_attestation_info_by_operational_address`"), err)
	})

	t.Run("Successful contract call", func(t *testing.T) {
		validatorOperationalAddress := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(validatorOperationalAddress)
		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, main.STAKING_CONTRACT_ADDRESS),
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

		epochInfo, err := main.FetchEpochInfo(mockAccount)

		require.Equal(t, main.EpochInfo{
			StakerAddress:             main.Address(*stakerAddress),
			Stake:                     uint128.New(0, 1), // the 1st 64 bits are all 0 as it's MaxUint64 + 1
			EpochLen:                  40,
			EpochId:                   1516,
			CurrentEpochStartingBlock: main.BlockNumber(639270),
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
			ContractAddress:    utils.HexToFelt(t, main.ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestWindowEntrypointHash,
			Calldata:           []*felt.Felt{},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		window, err := main.FetchAttestWindow(mockAccount)

		require.Equal(t, uint64(0), window)
		require.Equal(t, errors.New("Error when calling entrypoint `attestation_window`: some contract error"), err)
	})

	t.Run("Return error: wrong contract response length", func(t *testing.T) {
		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, main.ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestWindowEntrypointHash,
			Calldata:           []*felt.Felt{},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{}, nil)

		window, err := main.FetchAttestWindow(mockAccount)

		require.Equal(t, uint64(0), window)
		require.Equal(t, errors.New("Invalid response from entrypoint `attestation_window`"), err)
	})

	t.Run("Successful contract call", func(t *testing.T) {
		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, main.ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: expectedAttestWindowEntrypointHash,
			Calldata:           []*felt.Felt{},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{new(felt.Felt).SetUint64(16)}, nil)

		window, err := main.FetchAttestWindow(mockAccount)

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
			ContractAddress:    utils.HexToFelt(t, main.STRK_CONTRACT_ADDRESS),
			EntryPointSelector: expectedBalanceOfEntrypointHash,
			Calldata:           []*felt.Felt{addr},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		balance, err := main.FetchValidatorBalance(mockAccount)

		require.Equal(t, main.Balance(felt.Zero), balance)
		require.Equal(t, errors.New("Error when calling entrypoint `balanceOf`: some contract error"), err)
	})

	t.Run("Return error: wrong contract response length", func(t *testing.T) {
		addr := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(addr)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, main.STRK_CONTRACT_ADDRESS),
			EntryPointSelector: expectedBalanceOfEntrypointHash,
			Calldata:           []*felt.Felt{addr},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{}, nil)

		balance, err := main.FetchValidatorBalance(mockAccount)

		require.Equal(t, main.Balance(felt.Zero), balance)
		require.Equal(t, errors.New("Invalid response from entrypoint `balanceOf`"), err)
	})

	t.Run("Successful contract call", func(t *testing.T) {
		addr := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(addr)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, main.STRK_CONTRACT_ADDRESS),
			EntryPointSelector: expectedBalanceOfEntrypointHash,
			Calldata:           []*felt.Felt{addr},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return([]*felt.Felt{new(felt.Felt).SetUint64(1)}, nil)

		balance, err := main.FetchValidatorBalance(mockAccount)

		require.Equal(t, main.Balance(*new(felt.Felt).SetUint64(1)), balance)
		require.Nil(t, err)
	})
}

func TestFetchEpochAndAttestInfo(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)
	mockLogger := mocks.NewMockLogger(mockCtrl)

	t.Run("Return error: fetching epoch info error", func(t *testing.T) {
		validatorOperationalAddress := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(validatorOperationalAddress)

		expectedFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, main.STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("get_attestation_info_by_operational_address"),
			Calldata:           []*felt.Felt{validatorOperationalAddress},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		epochInfo, attestInfo, err := main.FetchEpochAndAttestInfo(mockAccount, mockLogger)

		require.Equal(t, main.EpochInfo{}, epochInfo)
		require.Equal(t, main.AttestInfo{}, attestInfo)
		require.Equal(t, errors.New("Error when calling entrypoint `get_attestation_info_by_operational_address`: some contract error"), err)
	})

	t.Run("Return error: fetching window error", func(t *testing.T) {
		validatorOperationalAddress := utils.HexToFelt(t, "0x123")

		mockAccount.EXPECT().Address().Return(validatorOperationalAddress)

		expectedEpochInfoFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, main.STAKING_CONTRACT_ADDRESS),
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

		mockLogger.
			EXPECT().
			Infow(
				"Successfully fetched epoch info",
				"epoch ID", epochId,
				"epoch starting block", main.BlockNumber(epochStartingBlock),
				"epoch ending block", main.BlockNumber(epochStartingBlock+epochLength),
			)

		expectedWindowFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, main.ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("attestation_window"),
			Calldata:           []*felt.Felt{},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedWindowFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		epochInfo, attestInfo, err := main.FetchEpochAndAttestInfo(mockAccount, mockLogger)

		require.Equal(t, main.EpochInfo{}, epochInfo)
		require.Equal(t, main.AttestInfo{}, attestInfo)
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
			ContractAddress:    utils.HexToFelt(t, main.STAKING_CONTRACT_ADDRESS),
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

		mockLogger.
			EXPECT().
			Infow(
				"Successfully fetched epoch info",
				"epoch ID", epochId,
				"epoch starting block", main.BlockNumber(epochStartingBlock),
				"epoch ending block", main.BlockNumber(epochStartingBlock+epochLen),
			)

		// Mock fetchAttestWindow call
		expectedWindowFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, main.ATTEST_CONTRACT_ADDRESS),
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

		expectedTargetBlock := main.BlockNumber(639291)
		expectedAttestInfo := main.AttestInfo{
			TargetBlock: expectedTargetBlock,
			WindowStart: expectedTargetBlock + main.BlockNumber(main.MIN_ATTESTATION_WINDOW),
			WindowEnd:   expectedTargetBlock + main.BlockNumber(attestWindow),
		}

		mockLogger.
			EXPECT().
			Infow(
				"Successfully computed target block to attest to",
				"epoch ID", epochId,
				"attestation info", expectedAttestInfo,
			)

		// Test
		epochInfo, attestInfo, err := main.FetchEpochAndAttestInfo(mockAccount, mockLogger)

		// Assert
		expectedEpochInfo := main.EpochInfo{
			StakerAddress:             main.Address(*stakerAddress),
			Stake:                     uint128.From64(stake),
			EpochLen:                  epochLen,
			EpochId:                   epochId,
			CurrentEpochStartingBlock: main.BlockNumber(epochStartingBlock),
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
			ContractAddress: utils.HexToFelt(t, main.ATTEST_CONTRACT_ADDRESS),
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHash},
		}}

		mockAccount.
			EXPECT().
			BuildAndSendInvokeTxn(context.Background(), expectedFnCall, main.FEE_ESTIMATION_MULTIPLIER).
			Return(nil, errors.New("some sending error"))

		attestRequired := main.AttestRequired{BlockHash: main.BlockHash(*blockHash)}
		invokeRes, err := main.InvokeAttest(mockAccount, &attestRequired)

		require.Nil(t, invokeRes)
		require.EqualError(t, err, "some sending error")
	})

	t.Run("Invoke tx successfully sent", func(t *testing.T) {
		blockHash := new(felt.Felt).SetUint64(123)

		expectedFnCall := []rpc.InvokeFunctionCall{{
			ContractAddress: utils.HexToFelt(t, main.ATTEST_CONTRACT_ADDRESS),
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHash},
		}}

		response := rpc.AddInvokeTransactionResponse{
			TransactionHash: utils.HexToFelt(t, "0x123"),
		}
		mockAccount.
			EXPECT().
			BuildAndSendInvokeTxn(context.Background(), expectedFnCall, main.FEE_ESTIMATION_MULTIPLIER).
			Return(&response, nil)

		attestRequired := main.AttestRequired{BlockHash: main.BlockHash(*blockHash)}
		invokeRes, err := main.InvokeAttest(mockAccount, &attestRequired)

		require.Equal(t, &response, invokeRes)
		require.Nil(t, err)
	})
}
