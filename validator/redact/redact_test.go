package redact_test

import (
	"errors"
	"testing"

	"github.com/NethermindEth/starknet-staking-v2/validator/redact"
	"github.com/stretchr/testify/require"
)

func TestURLsInReplacesURLsThatDoNotParse(t *testing.T) {
	signError := `failed to sign transaction: parse ` +
		`"http://signeruser:signerpass@127.0.0.1:1234/%zz/sign-path-0123456789abcdef/sign": ` +
		`invalid URL escape "%zz"`
	expected := `failed to sign transaction: parse "<invalid URL>": invalid URL escape "%zz"`

	require.Equal(t, expected, redact.URLsIn(signError))

	err := redact.ErrorURLs(errors.New(signError))
	require.EqualError(t, err, expected)
	require.NotContains(t, err.Error(), "signerpass")
}
