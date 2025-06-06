package config

import (
	"errors"
	"fmt"

	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
)

type chainID int

const (
	mainnet chainID = iota
	sepolia
	unknown
)

// An array containing the defaults values for Starknet networks
var defaults = [2]ContractAddresses{
	// Mainnet default values
	{
		Staking: constants.MAINNET_STAKING_CONTRACT_ADDRESS,
		Attest:  constants.MAINNET_ATTEST_CONTRACT_ADDRESS,
	},
	// Sepolia default values
	{
		Staking: constants.SEPOLIA_STAKING_CONTRACT_ADDRESS,
		Attest:  constants.SEPOLIA_ATTEST_CONTRACT_ADDRESS,
	},
}

func chainIDFromStr(s string) chainID {
	switch s {
	case "SN_MAINNET":
		return mainnet
	case "SN_SEPOLIA":
		return sepolia
	default:
		return unknown
	}
}

type ContractAddresses struct {
	Staking string
	Attest  string
}

func (ca *ContractAddresses) SetDefaults(chainIDStr string) *ContractAddresses {
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

func (ca *ContractAddresses) Check() error {
	if isZero(ca.Staking) {
		return errors.New("staking contract address is not set")
	}
	if isZero(ca.Attest) {
		return errors.New("attest contract address is not set")
	}
	return nil
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
