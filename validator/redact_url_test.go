package validator

import (
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
