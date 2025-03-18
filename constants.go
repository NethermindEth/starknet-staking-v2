package main

import (
	"errors"

	"github.com/NethermindEth/juno/core/felt"
)

const STAKING_CONTRACT_ADDRESS string = "0xETC"
const ATTESTATION_CONTRACT_ADDRESS string = "0xETC"

var stakingContractAddress, _ = new(felt.Felt).SetString(STAKING_CONTRACT_ADDRESS)
var attestationContractAddress, _ = new(felt.Felt).SetString(ATTESTATION_CONTRACT_ADDRESS)

const MIN_ATTESTATION_WINDOW uint8 = 10

type NetworkTokenAddress struct {
	Eth  string
	Strk string
}

var MAINNET_TOKENS NetworkTokenAddress = NetworkTokenAddress{
	Eth:  "0x049d36570d4e46f48e99674bd3fcc84644ddd6b96f7c741b1562b82f9e004dc7",
	Strk: "0x04718f5a0fc34cc1af16a1cdee98ffb20c31f5cd61d6ab07201858f4287c938d",
}

var SEPOLIA_TOKENS NetworkTokenAddress = NetworkTokenAddress{
	Eth:  "0x049d36570d4e46f48e99674bd3fcc84644ddd6b96f7c741b1562b82f9e004dc7",
	Strk: "0x04718f5a0fc34cc1af16a1cdee98ffb20c31f5cd61d6ab07201858f4287c938d",
}

func entrypointInternalError(entrypointName string, err error) error {
	return errors.New("Error when calling entrypoint `" + entrypointName + "`: " + err.Error())
}

func entrypointResponseError(entrypointName string) error {
	return errors.New("Invalid response from entrypoint `" + entrypointName + "`")
}
