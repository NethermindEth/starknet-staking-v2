package main

import (
	"log"

	"github.com/NethermindEth/juno/core/felt"
	"lukechampine.com/uint128"
)

type Address felt.Felt

func (a Address) ToFelt() felt.Felt {
	return felt.Felt(a)
}

func (a Address) SetString(addrStr string) Address {
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

func (b BlockHash) ToFelt() felt.Felt {
	return felt.Felt(b)
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
