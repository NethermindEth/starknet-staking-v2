package types_test

import (
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/stretchr/testify/require"
)

func TestAddressFromString(t *testing.T) {
	t.Run("Valid address", func(t *testing.T) {
		address, err := types.AddressFromString("0x123")
		require.NoError(t, err)
		require.Equal(t, felt.NewUnsafeFromString[felt.Felt]("0x123"), address.Felt())
	})

	t.Run("Malformed address returns an error", func(t *testing.T) {
		_, err := types.AddressFromString("0xZZZ")
		require.ErrorContains(t, err, "cannot turn string `0xZZZ` into an address")
	})
}
