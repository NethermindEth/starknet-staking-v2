package types_test

import (
	"math/big"
	"strings"
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

	t.Run("Address that isn't 0x-prefixed hexadecimal returns an error", func(t *testing.T) {
		for _, addrStr := range []string{
			"",
			"0x",
			"-1",
			"-0x1f",
			"+0x1f",
			"017",
			"0o17",
			"0b1010",
			"0x1_f",
			"123",
			"abc",
			" 0x1f",
			"0x1f ",
			"0x" + strings.Repeat("0", 65),
		} {
			_, err := types.AddressFromString(addrStr)
			require.ErrorContains(
				t,
				err,
				"it must be a 0x-prefixed hexadecimal number of at most 64 digits",
				addrStr,
			)
			require.ErrorContains(t, err, "`"+addrStr+"`")
		}
	})

	t.Run("Address too big for a felt returns an error", func(t *testing.T) {
		_, err := types.AddressFromString("0x" + strings.Repeat("f", 64))
		require.ErrorContains(t, err, "cannot turn string")
	})

	t.Run("Uppercase prefix and digits are accepted", func(t *testing.T) {
		address, err := types.AddressFromString("0X1F")
		require.NoError(t, err)
		require.Equal(t, felt.NewUnsafeFromString[felt.Felt]("0x1f"), address.Felt())
	})
}

func TestPrivateKeyFromString(t *testing.T) {
	// Order of the Stark curve
	const curveOrder = "0x800000000000010ffffffffffffffffb781126dcae7b2321e66a241adc64d2f"

	t.Run("Valid private keys", func(t *testing.T) {
		for keyStr, expected := range map[string]int64{
			"0x1":   1,
			"0x123": 0x123,
			"0XaBc": 0xabc,
			"0x00000000000000000000000000000000000000000000000000000000000000ff": 0xff,
		} {
			key, err := types.PrivateKeyFromString(keyStr)
			require.NoError(t, err, keyStr)
			require.Equal(t, big.NewInt(expected), key, keyStr)
		}

		// The largest valid key is the curve order minus one
		key, err := types.PrivateKeyFromString(
			"0x800000000000010ffffffffffffffffb781126dcae7b2321e66a241adc64d2e",
		)
		require.NoError(t, err)
		expected, ok := new(big.Int).SetString(curveOrder, 0)
		require.True(t, ok)
		require.Equal(t, expected.Sub(expected, big.NewInt(1)), key)
	})

	t.Run("Malformed private keys never appear in the error", func(t *testing.T) {
		for _, keyStr := range []string{
			"",
			"0x",
			"zzz-dummy",
			"0xZZdummy",
			"123456",
			"abcdef",
			"-0x123abc",
			"+0x123abc",
			"0o17777",
			"0b101010",
			"0x1_23abc",
			" 0x123abc",
			"0x123abc\n",
			`"0x123abc"`,
			"0x" + strings.Repeat("1", 65),
		} {
			key, err := types.PrivateKeyFromString(keyStr)
			require.Nil(t, key)
			require.ErrorContains(
				t, err, "it must be a 0x-prefixed hexadecimal number of at most 64 digits",
			)
			if len(keyStr) > len("0x") {
				require.NotContains(t, err.Error(), keyStr)
			}
		}
	})

	t.Run("Private keys out of the curve range", func(t *testing.T) {
		for _, keyStr := range []string{
			"0x0",
			"0x0000",
			curveOrder,
			"0x" + strings.Repeat("f", 64),
		} {
			key, err := types.PrivateKeyFromString(keyStr)
			require.Nil(t, key)
			require.EqualError(
				t, err, "it must be greater than 0 and lower than the Stark curve order",
			)
		}
	})
}
