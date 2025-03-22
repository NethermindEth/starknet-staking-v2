package main

import (
	"context"
	"fmt"
	"log"
	"math/big"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
	"lukechampine.com/uint128"
)

func NewProvider(providerUrl string) *rpc.Provider {
	provider, err := rpc.NewProvider(providerUrl)
	if err != nil {
		log.Fatalf("Error connecting to RPC provider at %s: %s", providerUrl, err)
	}
	return provider
}

func NewAccount(provider *rpc.Provider, accountData *AccountData) *account.Account {
	// accountData should contain the information required:
	//  * staker operational address
	//  * public key
	//  * private key

	// place holder need to change
	var publicKey string
	var privateKey big.Int
	var accountAddr Address
	// ----------------------------

	ks := account.NewMemKeystore()
	ks.Put(publicKey, &privateKey)

	accountAddrFelt := accountAddr.ToFelt()
	account, err := account.NewAccount(provider, &accountAddrFelt, publicKey, ks, 2)
	if err != nil {
		log.Fatalf("Cannot create new account: %s", err)
	}
	return account
}

// TODO: might not need those 2 endpoints if we get info directly from staking contract

// Given a validator address returns the staked amount
func staked(address *Address) Balance {
	return Balance{}
}

// Given an address returns it's balance
func balance(address *Address) Balance {
	return Balance{}
}

func subscribeToBlockHeader(providerUrl string, blockHeaderFeed chan<- *rpc.BlockHeader) {
	fmt.Println("Starting websocket connection...")

	// Take the providerUrl parts (host & port) and build the ws url
	wsProviderUrl := "ws://" + "localhost" + ":" + "6061" + "/v0_8"

	// Initialize connection to WS provider
	wsClient, err := rpc.NewWebsocketProvider(wsProviderUrl)
	if err != nil {
		panic(fmt.Sprintf("Error dialing the WS provider: %s", err))
	}
	defer wsClient.Close()       // Close the WS client when the program finishes
	defer close(blockHeaderFeed) // Close the headers channel

	fmt.Println("Established connection with the client")

	sub, err := wsClient.SubscribeNewHeads(context.Background(), blockHeaderFeed, nil)
	if err != nil {
		panic(fmt.Sprintf("Error subscribing to new block headers: %s", err))
	}

	fmt.Println("Successfully subscribed to the node. Subscription ID:", sub.ID())
}

func fetchAttestationInfo(account *account.Account) (AttestationInfo, error) {
	contractAddrFelt := attestationContractAddress.ToFelt()

	functionCall := rpc.FunctionCall{
		ContractAddress:    &contractAddrFelt,
		EntryPointSelector: utils.GetSelectorFromNameFelt("get_attestation_info_by_operational_address"),
		Calldata:           []*felt.Felt{account.AccountAddress},
	}

	result, err := account.Call(context.Background(), functionCall, rpc.BlockID{Tag: "latest"})
	if err != nil {
		return AttestationInfo{}, entrypointInternalError("get_attestation_info_by_operational_address", err)
	}

	if len(result) != 5 {
		return AttestationInfo{}, entrypointResponseError("get_attestation_info_by_operational_address")
	}

	// TODO: verify once endpoint is available
	stake := result[1].Bits()
	return AttestationInfo{
		StakerAddress:             Address(*result[0]),
		Stake:                     uint128.Uint128{Lo: stake[0], Hi: stake[1]},
		EpochLen:                  result[2].Uint64(),
		EpochId:                   result[3].Uint64(),
		CurrentEpochStartingBlock: BlockNumber(result[4].Uint64()),
	}, nil
}

func fetchAttestationWindow(account *account.Account) (uint64, error) {
	contractAddrFelt := attestationContractAddress.ToFelt()

	result, err := account.Call(
		context.Background(),
		rpc.FunctionCall{
			ContractAddress:    &contractAddrFelt,
			EntryPointSelector: utils.GetSelectorFromNameFelt("attestation_window"),
			Calldata:           []*felt.Felt{},
		},
		rpc.BlockID{Tag: "latest"},
	)

	if err != nil {
		return 0, entrypointInternalError("attestation_window", err)
	}

	if len(result) != 1 {
		return 0, entrypointResponseError("attestation_window")
	}

	return result[0].Uint64(), nil
}

func fetchValidatorBalance(account *account.Account) (Balance, error) {
	contractAddrFelt := sepoliaStrkTokenAddress.ToFelt()

	result, err := account.Call(
		context.Background(),
		rpc.FunctionCall{
			ContractAddress:    &contractAddrFelt,
			EntryPointSelector: utils.GetSelectorFromNameFelt("balanceOf"),
			Calldata:           []*felt.Felt{account.AccountAddress},
		},
		rpc.BlockID{Tag: "latest"},
	)

	if err != nil {
		return Balance{}, entrypointInternalError("balanceOf", err)
	}

	if len(result) != 1 {
		return Balance{}, entrypointResponseError("balanceOf")
	}

	return Balance(*result[0]), nil
}

func invokeAttest(
	account *account.Account, attest *AttestRequired,
) (*rpc.AddInvokeTransactionResponse, error) {
	contractAddrFelt := attestationContractAddress.ToFelt()
	blockHashFelt := attest.blockHash.ToFelt()

	calls := []rpc.InvokeFunctionCall{{
		ContractAddress: &contractAddrFelt,
		FunctionName:    "attest",
		CallData:        []*felt.Felt{&blockHashFelt},
	}}

	return account.BuildAndSendInvokeTxn(context.Background(), calls, FEE_ESTIMATION_MULTIPLIER)
}
