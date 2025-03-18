package main

import (
	"github.com/NethermindEth/juno/core/felt"
	rpcv8 "github.com/NethermindEth/juno/rpc/v8"
	"lukechampine.com/uint128"
)

type Address felt.Felt

type Balance felt.Felt

type BlockNumber = uint64
type BlockHash = felt.Felt

// HeadersSubscriptionResponse is the response from the subscription to new block headers
type HeadersSubscriptionResponse struct {
	JsonRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  struct {
		Result         rpcv8.BlockHeader `json:"result"`
		SubscriptionID uint64            `json:"subscription_id"`
	} `json:"params"`
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
	blockHash *BlockHash
}
