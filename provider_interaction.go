package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"

	"github.com/NethermindEth/juno/core/felt"
	rpcv8 "github.com/NethermindEth/juno/rpc/v8"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
	"github.com/gorilla/websocket"
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

// Given an account, returns it's nonce
func nonce(account *account.Account) (*felt.Felt, error) {
	return account.Nonce(context.Background(), rpc.BlockID{Tag: "latest"}, account.AccountAddress)
}

// Subscribe to block headers
func subscribeToBlockHeaders(providerUrl string, blockHeaderChan chan<- rpcv8.BlockHeader) {
	// Take the providerUrl parts (host & port) and build the ws url
	wsURL := "ws://" + "localhost" + ":" + "6061" + "/v0_8"

	// Connect to the WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn.Close()
	if err != nil {
		fmt.Println("Error connecting to WebSocket:", err)
		// TODO: implement a retry mechanism ?
	}

	// JSON RPC request to subscribe to new block headers
	subscribeMessage := `{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "starknet_subscribeNewHeads"
	}`

	// Send the subscription request
	if err := conn.WriteMessage(websocket.TextMessage, []byte(subscribeMessage)); err != nil {
		fmt.Println("Error sending subscription request:", err)
		// TODO: implement a retry mechanism ?
	}

	// TODO: implement a logger
	fmt.Println("Subscribed to new Starknet block headers...")

	// Listen for new block headers
	for {
		msgType, msgBytes, err := conn.ReadMessage()
		fmt.Println("Message type:", msgType)
		if err != nil {
			fmt.Println("Error reading message:", err)
			// TODO: implement a retry mechanism ?
		}

		// TODO: in logger
		// fmt.Println("New block header:", string(msgBytes))

		var response HeadersSubscriptionResponse
		if err = json.Unmarshal(msgBytes, &response); err != nil {
			fmt.Println("Error unmarshaling JSON:", err)
		} else {
			blockHeaderChan <- response.Params.Result
		}
	}
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
