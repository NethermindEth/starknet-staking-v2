package signer_test

import (
	"testing"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/signer"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Run("Malformed private key is never part of the error", func(t *testing.T) {
		const malformedKey = "0xNOT-A-KEY-dummy"

		_, err := signer.New(malformedKey, log.NewNopZapLogger())
		require.ErrorContains(t, err, "invalid private key")
		require.NotContains(t, err.Error(), malformedKey)
	})

	t.Run("Private key out of range", func(t *testing.T) {
		_, err := signer.New("0x0", log.NewNopZapLogger())
		require.ErrorContains(t, err, "invalid private key")
	})

	t.Run("Valid private key", func(t *testing.T) {
		_, err := signer.New("0x123", log.NewNopZapLogger())
		require.NoError(t, err)
	})
}
