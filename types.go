package main

import (
	"context"
	"log"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/rpc"
	"lukechampine.com/uint128"
)

type Address felt.Felt

func (a *Address) ToFelt() felt.Felt {
	return felt.Felt(*a)
}

func AddressFromString(addrStr string) Address {
	adr, err := new(felt.Felt).SetString(addrStr)
	if err != nil {
		log.Fatalf("Could not create felt address from addr %s, error: %s", addrStr, err)
	}

	return Address(*adr)
}

type Balance felt.Felt

type BlockNumber uint64

func (b BlockNumber) ToUint64() uint64 {
	return uint64(b)
}

type BlockHash felt.Felt

func (b *BlockHash) ToFelt() felt.Felt {
	return felt.Felt(*b)
}

// AttestationInfo is the response from the get_attestation_info_by_operational_address
type AttestationInfo struct {
	StakerAddress             Address         `json:"staker_address"`
	Stake                     uint128.Uint128 `json:"stake"`
	EpochLen                  uint64          `json:"epoch_len"`
	EpochId                   uint64          `json:"epoch_id"`
	CurrentEpochStartingBlock BlockNumber     `json:"current_epoch_starting_block"`
}

type AttestRequiredWithValidity struct {
	AttestRequired
	untilBlockNumber BlockNumber
}

type AttestRequired struct {
	BlockHash BlockHash
}

//go:generate mockgen -destination=./mocks/mock_account.go -package=mocks github.com/NethermindEth/starknet-staking-v2 Account
type Accounter interface {
	// Methods from account.Account
	GetTransactionStatus(ctx context.Context, transactionHash *felt.Felt) (*rpc.TxnStatusResp, error)
	BuildAndSendInvokeTxn(ctx context.Context, functionCalls []rpc.InvokeFunctionCall, multiplier float64) (*rpc.AddInvokeTransactionResponse, error)
	Call(ctx context.Context, call rpc.FunctionCall, blockId rpc.BlockID) ([]*felt.Felt, error)

	// Custom Methods
	//
	// Want to return `Address` type here but it means creating a separate pkg
	// because otherwise mockgen tries to import this "main" pkg in its mock file
	// which is not allowed.
	// I think we should put this "types" file into a different pkg to be able to:
	// 1. Return `Address` type here
	// 2. Use "go generate" mock for this interface (only generating mock using `mockgen` cmd works now)
	Address() felt.Felt
}
