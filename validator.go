package main

import (
	"context"
	"math/big"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
	"lukechampine.com/uint128"
)

//go:generate mockgen -destination=./mocks/mock_account.go -package=mocks github.com/NethermindEth/starknet-staking-v2 Accounter
type Accounter interface {
	// Methods from account.Account
	GetTransactionStatus(ctx context.Context, transactionHash *felt.Felt) (*rpc.TxnStatusResp, error)
	BuildAndSendInvokeTxn(ctx context.Context, functionCalls []rpc.InvokeFunctionCall, multiplier float64) (*rpc.AddInvokeTransactionResponse, error)
	Call(ctx context.Context, call rpc.FunctionCall, blockId rpc.BlockID) ([]*felt.Felt, error)
	BlockWithTxHashes(ctx context.Context, blockID rpc.BlockID) (interface{}, error)

	// Custom Methods
	//
	// Want to return `Address` type here but it means creating a separate pkg
	// because otherwise mockgen tries to import this "main" pkg in its mock file
	// which is not allowed.
	// I think we should put this "types" file into a different pkg to be able to:
	// 1. Return `Address` type here
	// 2. Use "go generate" mock for this interface (only generating mock using `mockgen` cmd works now)
	Address() *felt.Felt
}

type ValidatorAccount account.Account

func NewValidatorAccount[Log Logger](provider *rpc.Provider, logger Log, accountData *AccountData) ValidatorAccount {
	publicKey := accountData.pubKey
	privateKey, ok := new(big.Int).SetString(accountData.privKey, 0)
	if !ok {
		logger.Fatalf("Cannot turn private key %s into a big int", privateKey)
	}
	accountAddr := AddressFromString(accountData.address)

	ks := account.SetNewMemKeystore(publicKey, privateKey)

	accountAddrFelt := accountAddr.ToFelt()
	account, err := account.NewAccount(provider, &accountAddrFelt, publicKey, ks, 2)
	if err != nil {
		logger.Fatalf("Cannot create validator account: %s", err)
	}

	logger.Infow("Successfully created validator account", "address", accountAddrFelt.String())
	return ValidatorAccount(*account)
}

func (v *ValidatorAccount) GetTransactionStatus(ctx context.Context, transactionHash *felt.Felt) (*rpc.TxnStatusResp, error) {
	return ((*account.Account)(v)).Provider.GetTransactionStatus(ctx, transactionHash)
}

func (v *ValidatorAccount) BuildAndSendInvokeTxn(ctx context.Context, functionCalls []rpc.InvokeFunctionCall, multiplier float64) (*rpc.AddInvokeTransactionResponse, error) {
	return ((*account.Account)(v)).BuildAndSendInvokeTxn(ctx, functionCalls, multiplier)
}

func (v *ValidatorAccount) Call(ctx context.Context, call rpc.FunctionCall, blockId rpc.BlockID) ([]*felt.Felt, error) {
	return ((*account.Account)(v)).Provider.Call(ctx, call, blockId)
}

func (v *ValidatorAccount) BlockWithTxHashes(ctx context.Context, blockID rpc.BlockID) (interface{}, error) {
	return ((*account.Account)(v)).Provider.BlockWithTxHashes(ctx, blockID)
}

func (v *ValidatorAccount) Address() *felt.Felt {
	return v.AccountAddress
}

// I believe all these functions down here should be methods
// Postponing for now to not affect test code

func FetchEpochInfo[Account Accounter](account Account) (EpochInfo, error) {
	contractAddrFelt := StakingContract.ToFelt()

	functionCall := rpc.FunctionCall{
		ContractAddress:    &contractAddrFelt,
		EntryPointSelector: utils.GetSelectorFromNameFelt("get_attestation_info_by_operational_address"),
		Calldata:           []*felt.Felt{account.Address()},
	}

	result, err := account.Call(context.Background(), functionCall, rpc.BlockID{Tag: "latest"})
	if err != nil {
		return EpochInfo{}, entrypointInternalError("get_attestation_info_by_operational_address", err)
	}

	if len(result) != 5 {
		return EpochInfo{}, entrypointResponseError("get_attestation_info_by_operational_address")
	}

	stake := result[1].Bits()
	return EpochInfo{
		StakerAddress:             Address(*result[0]),
		Stake:                     uint128.New(stake[0], stake[1]),
		EpochLen:                  result[2].Uint64(),
		EpochId:                   result[3].Uint64(),
		CurrentEpochStartingBlock: BlockNumber(result[4].Uint64()),
	}, nil
}

func FetchAttestWindow[Account Accounter](account Account) (uint64, error) {
	contractAddrFelt := AttestContract.ToFelt()

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
func FetchValidatorBalance[Account Accounter](account Account) (Balance, error) {
	contractAddrFelt := StrkTokenContract.ToFelt()

	result, err := account.Call(
		context.Background(),
		rpc.FunctionCall{
			ContractAddress:    &contractAddrFelt,
			EntryPointSelector: utils.GetSelectorFromNameFelt("balanceOf"),
			Calldata:           []*felt.Felt{account.Address()},
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

// I believe this functions returns many things, it should probably be grouped under
// a unique type
func FetchEpochAndAttestInfo[Account Accounter, Log Logger](account Account, logger Log) (EpochInfo, AttestInfo, error) {
	epochInfo, err := FetchEpochInfo(account)
	if err != nil {
		return EpochInfo{}, AttestInfo{}, err
	}
	logger.Infow(
		"Successfully fetched epoch info",
		"epoch ID", epochInfo.EpochId,
		"epoch starting block", epochInfo.CurrentEpochStartingBlock,
		"epoch ending block", epochInfo.CurrentEpochStartingBlock+BlockNumber(epochInfo.EpochLen),
	)

	attestWindow, windowErr := FetchAttestWindow(account)
	if windowErr != nil {
		return EpochInfo{}, AttestInfo{}, windowErr
	}

	blockNum := ComputeBlockNumberToAttestTo(account, epochInfo, attestWindow)

	attestInfo := AttestInfo{
		TargetBlock: blockNum,
		WindowStart: blockNum + BlockNumber(MIN_ATTESTATION_WINDOW),
		WindowEnd:   blockNum + BlockNumber(attestWindow),
	}

	logger.Infow("Successfully computed target block to attest to", "epoch ID", epochInfo.EpochId, "attestation info", attestInfo)
	return epochInfo, attestInfo, nil
}

func InvokeAttest[Account Accounter](account Account, attest *AttestRequired) (
	*rpc.AddInvokeTransactionResponse, error,
) {
	contractAddrFelt := AttestContract.ToFelt()
	blockHashFelt := attest.BlockHash.ToFelt()

	calls := []rpc.InvokeFunctionCall{{
		ContractAddress: &contractAddrFelt,
		FunctionName:    "attest",
		CallData:        []*felt.Felt{&blockHashFelt},
	}}

	return account.BuildAndSendInvokeTxn(context.Background(), calls, FEE_ESTIMATION_MULTIPLIER)
}
