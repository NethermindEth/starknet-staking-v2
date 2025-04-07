package main_test

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	main "github.com/NethermindEth/starknet-staking-v2"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet.go/account"
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
		OperationalAddress: address,
	}
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

	logger, err := utils.NewZapLogger(utils.DEBUG, true)
	envVars, err := loadEnv(t)
	loadedEnvVars := err == nil

	if loadedEnvVars {
		t.Run("Error: private key conversion", func(t *testing.T) {
			provider, providerErr := rpc.NewProvider(envVars.httpProviderUrl)
			require.NoError(t, providerErr)

			validatorAccount, err := main.NewValidatorAccount(
				provider, logger, &main.AccountData{},
			)

			require.Equal(t, main.ValidatorAccount{}, validatorAccount)
			expectedErrorMsg := fmt.Sprintf(
				"Cannot turn private key %s into a big int", (*big.Int)(nil),
			)
			require.Equal(t, expectedErrorMsg, err.Error())
		})
		t.Run("Successful account creation", func(t *testing.T) {
			provider, providerErr := rpc.NewProvider(envVars.httpProviderUrl)
			require.NoError(t, providerErr)

			privateKey := "0x123"
			address := "0x456"
			accountData := NewAccountData(privateKey, address)

			// Test
			validatorAccount, err := main.NewValidatorAccount(provider, logger, &accountData)

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
	} else {
		t.Logf("Ignoring tests that require env variables: %s", err)
	}

	t.Run("Error: cannot create validator account", func(t *testing.T) {
		provider, providerErr := rpc.NewProvider("http://localhost:1234")
		require.NoError(t, providerErr)

		privateKey := "0x123"
		address := "0x456"
		accountData := NewAccountData(privateKey, address)
		validatorAccount, err := main.NewValidatorAccount(provider, logger, &accountData)

		require.Equal(t, main.ValidatorAccount{}, validatorAccount)
		expectedErrorMsg := `Cannot create validator account: -32603 The error is not a valid RPC error: Post "http://localhost:1234": dial tcp 127.0.0.1:1234: connect: connection refused`
		require.Equal(t, expectedErrorMsg, err.Error())
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
	logger, err := utils.NewZapLogger(utils.DEBUG, true)
	require.NoError(t, err)

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

		epochInfo, attestInfo, err := main.FetchEpochAndAttestInfo(mockAccount, logger)

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

		expectedWindowFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, main.ATTEST_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("attestation_window"),
			Calldata:           []*felt.Felt{},
		}

		mockAccount.
			EXPECT().
			Call(context.Background(), expectedWindowFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New("some contract error"))

		epochInfo, attestInfo, err := main.FetchEpochAndAttestInfo(mockAccount, logger)

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

		// Test
		epochInfo, attestInfo, err := main.FetchEpochAndAttestInfo(mockAccount, logger)

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
