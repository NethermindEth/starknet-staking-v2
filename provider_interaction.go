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

// We create a type (implementing an interface) to be able to mock the account for testing purposes
type ValidatorAccount account.Account

func NewValidatorAccount(provider *rpc.Provider, accountData *AccountData) ValidatorAccount {
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

	return ValidatorAccount(*account)
}

func (v *ValidatorAccount) GetTransactionStatus(ctx context.Context, transactionHash *felt.Felt) (*rpc.TxnStatusResp, error) {
	return ((*account.Account)(v)).GetTransactionStatus(ctx, transactionHash)
}

func (v *ValidatorAccount) BuildAndSendInvokeTxn(ctx context.Context, functionCalls []rpc.InvokeFunctionCall, multiplier float64) (*rpc.AddInvokeTransactionResponse, error) {
	return ((*account.Account)(v)).BuildAndSendInvokeTxn(ctx, functionCalls, multiplier)
}

func (v *ValidatorAccount) Call(ctx context.Context, call rpc.FunctionCall, blockId rpc.BlockID) ([]*felt.Felt, error) {
	return ((*account.Account)(v)).Call(ctx, call, blockId)
}

func (v *ValidatorAccount) Address() felt.Felt {
	return *v.AccountAddress
}

func subscribeToBlockHeader(providerUrl string, blockHeaderFeed chan<- *rpc.BlockHeader) {
	fmt.Println("Starting websocket connection...")

	// Take the providerUrl parts (host & port) and build the ws url
	wsProviderUrl := "ws://" + "localhost" + ":" + "6061" + "/v0_8"

	// Initialize connection to WS provider
	wsClient, err := rpc.NewWebsocketProvider(wsProviderUrl)
	if err != nil {
		log.Fatalf("Error dialing the WS provider: %s", err)
	}
	defer wsClient.Close()       // Close the WS client when the program finishes
	defer close(blockHeaderFeed) // Close the headers channel

	fmt.Println("Established connection with the client")

	sub, err := wsClient.SubscribeNewHeads(context.Background(), blockHeaderFeed, nil)
	if err != nil {
		log.Fatalf("Error subscribing to new block headers: %s", err)
	}

	fmt.Println("Successfully subscribed to the node. Subscription ID:", sub.ID())
}

func fetchAttestationInfo(account Accounter) (AttestationInfo, error) {
	contractAddrFelt := AttestationContractAddress.ToFelt()
	accountAddress := account.Address()

	functionCall := rpc.FunctionCall{
		ContractAddress:    &contractAddrFelt,
		EntryPointSelector: utils.GetSelectorFromNameFelt("get_attestation_info_by_operational_address"),
		Calldata:           []*felt.Felt{&accountAddress},
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

func fetchAttestationWindow(account Accounter) (uint64, error) {
	contractAddrFelt := AttestationContractAddress.ToFelt()

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

// For near future when tracking validator's balance
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

func invokeAttest[Account Accounter](
	account Account, attest *AttestRequired,
) (*rpc.AddInvokeTransactionResponse, error) {
	contractAddrFelt := AttestationContractAddress.ToFelt()
	blockHashFelt := attest.BlockHash.ToFelt()

	calls := []rpc.InvokeFunctionCall{{
		ContractAddress: &contractAddrFelt,
		FunctionName:    "attest",
		CallData:        []*felt.Felt{&blockHashFelt},
	}}

	return account.BuildAndSendInvokeTxn(context.Background(), calls, FEE_ESTIMATION_MULTIPLIER)
}
