package types

import (
	"encoding/json"
	"fmt"

	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"lukechampine.com/uint128"
)

type AttestRequired struct {
	BlockHash BlockHash
}

type AttestInfo struct {
	TargetBlock     BlockNumber
	TargetBlockHash BlockHash
	WindowStart     BlockNumber
	WindowEnd       BlockNumber
}

type EpochInfo struct {
	StakerAddress             Address         `json:"staker_address"`
	Stake                     uint128.Uint128 `json:"stake"`
	EpochLen                  uint64          `json:"epoch_len"`
	EpochId                   uint64          `json:"epoch_id"`
	CurrentEpochStartingBlock BlockNumber     `json:"current_epoch_starting_block"`
}

func (e *EpochInfo) String() string {
	jsonData, err := json.Marshal(e)
	if err != nil {
		panic("cannot marshall epoch info")
	}

	return string(jsonData)
}

type ValidationContracts struct {
	Staking Address
	Attest  Address
}

func ValidationContractsFromAddresses(ca *config.ContractAddresses) ValidationContracts {
	return ValidationContracts{
		Attest:  AddressFromString(ca.Attest),
		Staking: AddressFromString(ca.Staking),
	}
}

func (c *ValidationContracts) String() string {
	return fmt.Sprintf(`{
        Staking contract address: %s,
        Attestation contract address: %s,
    }`,
		c.Staking.String(),
		c.Attest.String(),
	)
}
