package validator

import (
	"log"

	"github.com/NethermindEth/juno/core/felt"
	"lukechampine.com/uint128"
)

type Address felt.Felt

func (a *Address) Felt() *felt.Felt {
	return (*felt.Felt)(a)
}

func AddressFromString(addrStr string) Address {
	adr, err := new(felt.Felt).SetString(addrStr)
	if err != nil {
		log.Fatalf("Could not create felt address from addr %s, error: %s", addrStr, err)
	}

	return Address(*adr)
}

func (a *Address) String() string {
	return (*felt.Felt)(a).String()
}

func (a *Address) UnmarshalJSON(data []byte) error {
	var f felt.Felt
	if err := f.UnmarshalJSON(data); err != nil {
		return err
	}
	*a = Address(f)
	return nil
}

func (a Address) MarshalJSON() ([]byte, error) {
	return (*felt.Felt)(&a).MarshalJSON()
}

type Balance felt.Felt

type BlockNumber uint64

func (b BlockNumber) Uint64() uint64 {
	return uint64(b)
}

type BlockHash felt.Felt

func (b *BlockHash) Felt() *felt.Felt {
	return (*felt.Felt)(b)
}

func (b *BlockHash) String() string {
	return (*felt.Felt)(b).String()
}

type EpochInfo struct {
	StakerAddress             Address         `json:"staker_address"`
	Stake                     uint128.Uint128 `json:"stake"`
	EpochLen                  uint64          `json:"epoch_len"`
	EpochId                   uint64          `json:"epoch_id"`
	CurrentEpochStartingBlock BlockNumber     `json:"current_epoch_starting_block"`
}

type AttestRequired struct {
	BlockHash BlockHash
}

type AttestInfo struct {
	TargetBlock     BlockNumber
	TargetBlockHash BlockHash
	WindowStart     BlockNumber
	WindowEnd       BlockNumber
}
