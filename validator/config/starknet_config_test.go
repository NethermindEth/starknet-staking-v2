package config_test

import (
	"testing"

	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/stretchr/testify/require"
)

func TestContractAddressesCheck(t *testing.T) {
	const (
		unknownChainID = "SN_GOERLI"
		stakingFlag    = "--staking-contract-address"
		attestFlag     = "--attest-contract-address"
	)

	t.Run("Known chain gets both default addresses", func(t *testing.T) {
		addresses := new(config.ContractAddresses).SetDefaults("SN_SEPOLIA")
		require.NoError(t, addresses.Check())
		require.Equal(t, constants.SepoliaStakingContractAddress, addresses.Staking)
		require.Equal(t, constants.SepoliaAttestContractAddress, addresses.Attest)
	})

	t.Run("Unknown chain without addresses names the chain and both flags", func(t *testing.T) {
		err := new(config.ContractAddresses).SetDefaults(unknownChainID).Check()
		require.EqualError(
			t,
			err,
			`no default contract addresses for chain id "SN_GOERLI" (defaults exist only for `+
				`SN_MAIN and SN_SEPOLIA): set --staking-contract-address and --attest-contract-address`,
		)
	})

	t.Run("Unknown chain with only the attest address names the staking flag", func(t *testing.T) {
		addresses := config.ContractAddresses{Attest: "0x123"}
		err := addresses.SetDefaults(unknownChainID).Check()
		require.ErrorContains(t, err, unknownChainID)
		require.ErrorContains(t, err, stakingFlag)
		require.NotContains(t, err.Error(), attestFlag)
	})

	t.Run("Unknown chain with only the staking address names the attest flag", func(t *testing.T) {
		addresses := config.ContractAddresses{Staking: "0x123"}
		err := addresses.SetDefaults(unknownChainID).Check()
		require.ErrorContains(t, err, unknownChainID)
		require.ErrorContains(t, err, attestFlag)
		require.NotContains(t, err.Error(), stakingFlag)
	})

	t.Run("Unknown chain with both addresses is valid", func(t *testing.T) {
		addresses := config.ContractAddresses{Staking: "0x123", Attest: "0x456"}
		require.NoError(t, addresses.SetDefaults(unknownChainID).Check())
		require.Equal(t, "0x123", addresses.Staking)
		require.Equal(t, "0x456", addresses.Attest)
	})
}
