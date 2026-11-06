package validator

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactURL(t *testing.T) {
	testCases := []struct {
		url      string
		expected string
	}{
		{url: "http://localhost:6060/rpc/v0_10", expected: "http://localhost:6060/rpc/v0_10"},
		{url: "wss://localhost:6061/rpc/v0_10", expected: "wss://localhost:6061/rpc/v0_10"},
		{url: "wrong url", expected: "wrong url"},
		{
			url:      "https://starknet-mainnet.infura.io/v3/0123456789abcdef0123456789abcdef",
			expected: "https://starknet-mainnet.infura.io/v3/REDACTED",
		},
		{
			url:      "wss://node.example/rpc/v0_10?apikey=secret&network=mainnet",
			expected: "wss://node.example/rpc/v0_10?apikey=REDACTED&network=REDACTED",
		},
		{
			url:      "https://user:secret@node.example/rpc",
			expected: "https://REDACTED@node.example/rpc",
		},
		{url: "http://[::1", expected: "<invalid URL>"},
	}

	for _, tc := range testCases {
		t.Run(tc.url, func(t *testing.T) {
			require.Equal(t, tc.expected, redactURL(tc.url))
		})
	}
}

func TestRedactURLsIn(t *testing.T) {
	testCases := []struct {
		text     string
		expected string
	}{
		{
			text: `-32603 The error is not a valid RPC error: Post ` +
				`"http://node:6060/v3/0123456789abcdef0123456789abcdef?apikey=topsecret": EOF`,
			expected: `-32603 The error is not a valid RPC error: Post ` +
				`"http://node:6060/v3/REDACTED?apikey=REDACTED": EOF`,
		},
		{
			text:     "cannot reach RPC provider at http://127.0.0.1:1: connection refused",
			expected: "cannot reach RPC provider at http://127.0.0.1:1: connection refused",
		},
		{
			text:     `WS provider URL must use ws:// or wss://, got "http://localhost:6061"`,
			expected: `WS provider URL must use ws:// or wss://, got "http://localhost:6061"`,
		},
		{
			text:     "dialling WS provider at wss://user:pw@node/ws: refused",
			expected: "dialling WS provider at wss://REDACTED@node/ws: refused",
		},
	}

	for _, tc := range testCases {
		require.Equal(t, tc.expected, redactURLsIn(tc.text))
	}
}

func TestRedactErrorURLsKeepsTheErrorChain(t *testing.T) {
	err := redactErrorURLs(transient(errors.New(
		`Post "http://node/v3/0123456789abcdef0123456789abcdef": EOF`,
	)))

	require.ErrorIs(t, err, ErrTransient)
	require.Equal(t, `Post "http://node/v3/REDACTED": EOF`, err.Error())
	require.NoError(t, redactErrorURLs(nil))
}
