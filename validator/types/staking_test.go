package types_test

import (
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/stretchr/testify/require"
)

func TestValidationContractsFromAddresses(t *testing.T) {
	const attestAddr = "0x123"
	const stakingAddr = "0x456"
	const malformedAddress = "0xZZZ"

	t.Run("Valid addresses", func(t *testing.T) {
		contracts, err := types.ValidationContractsFromAddresses(
			&config.ContractAddresses{Attest: attestAddr, Staking: stakingAddr},
		)
		require.NoError(t, err)
		require.Equal(t, felt.NewUnsafeFromString[felt.Felt](attestAddr), contracts.Attest.Felt())
		require.Equal(t, felt.NewUnsafeFromString[felt.Felt](stakingAddr), contracts.Staking.Felt())
	})

	t.Run("Malformed attest contract address returns an error", func(t *testing.T) {
		_, err := types.ValidationContractsFromAddresses(
			&config.ContractAddresses{Attest: malformedAddress, Staking: stakingAddr},
		)
		require.ErrorContains(t, err, "invalid attest contract address")
		require.ErrorContains(t, err, malformedAddress)
	})

	t.Run("Malformed staking contract address returns an error", func(t *testing.T) {
		_, err := types.ValidationContractsFromAddresses(
			&config.ContractAddresses{Attest: attestAddr, Staking: malformedAddress},
		)
		require.ErrorContains(t, err, "invalid staking contract address")
		require.ErrorContains(t, err, malformedAddress)
	})
}
