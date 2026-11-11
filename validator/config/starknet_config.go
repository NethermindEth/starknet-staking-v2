package config

import (
	"fmt"
	"strings"

	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
)

type chainID int

const (
	mainnetChainID = "SN_MAIN"
	sepoliaChainID = "SN_SEPOLIA"
)

const (
	mainnet chainID = iota
	sepolia
	unknown
)

// An array containing the defaults values for Starknet networks
var defaults = [2]ContractAddresses{
	// Mainnet default values
	{
		Staking: constants.MainnetStakingContractAddress,
		Attest:  constants.MainnetAttestContractAddress,
		chainID: mainnetChainID,
	},
	// Sepolia default values
	{
		Staking: constants.SepoliaStakingContractAddress,
		Attest:  constants.SepoliaAttestContractAddress,
		chainID: sepoliaChainID,
	},
}

func chainIDFromStr(s string) chainID {
	switch s {
	case mainnetChainID:
		return mainnet
	case sepoliaChainID:
		return sepolia
	default:
		return unknown
	}
}

type ContractAddresses struct {
	Staking string
	Attest  string
	chainID string
}

func (ca *ContractAddresses) SetDefaults(chainIDStr string) *ContractAddresses {
	ca.chainID = chainIDStr
	chainID := chainIDFromStr(chainIDStr)
	if chainID == unknown {
		return ca
	}

	defaultConfig := defaults[int(chainID)]
	if isZero(ca.Staking) {
		ca.Staking = defaultConfig.Staking
	}
	if isZero(ca.Attest) {
		ca.Attest = defaultConfig.Attest
	}

	return ca
}

func (ca *ContractAddresses) ChainID() string {
	return ca.chainID
}

func (ca *ContractAddresses) Check() error {
	var missingFlags []string
	if isZero(ca.Staking) {
		missingFlags = append(missingFlags, "--staking-contract-address")
	}
	if isZero(ca.Attest) {
		missingFlags = append(missingFlags, "--attest-contract-address")
	}
	if len(missingFlags) == 0 {
		return nil
	}

	return fmt.Errorf(
		"no default contract addresses for chain id %q (defaults exist only for %s and %s): "+
			"set %s",
		ca.chainID,
		mainnetChainID,
		sepoliaChainID,
		strings.Join(missingFlags, " and "),
	)
}

func (ca *ContractAddresses) String() string {
	return fmt.Sprintf(`{
        staking contract address: %s,
        attestation contract address: %s,
    }`,
		ca.Staking,
		ca.Attest,
	)
}

type StarknetConfig struct {
	ContractAddresses ContractAddresses
	AttestOptions     string
}

func (c *StarknetConfig) SetDefaults(chainID string) *StarknetConfig {
	c.ContractAddresses.SetDefaults(chainID)

	return c
}

func (c *StarknetConfig) Check() error {
	return c.ContractAddresses.Check()
}
