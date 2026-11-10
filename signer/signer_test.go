package signer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/signer"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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

func listenAddress(t *testing.T, logs *observer.ObservedLogs) string {
	t.Helper()

	require.Eventually(t, func() bool {
		return logs.FilterMessage("server running").Len() == 1
	}, 5*time.Second, 10*time.Millisecond)
	address, ok := logs.FilterMessage("server running").AllUntimed()[0].ContextMap()["URL"].(string)
	require.True(t, ok)

	return address
}

func validSignRequest(t *testing.T) string {
	t.Helper()

	zeroBounds := rpc.ResourceBounds{MaxAmount: "0x0", MaxPricePerUnit: "0x0"}
	request := signer.Request{
		BroadcastInvokeTxnV3: &rpc.BroadcastInvokeTxnV3{
			Type:          rpc.TransactionTypeInvoke,
			SenderAddress: felt.NewUnsafeFromString[felt.Felt]("0x456"),
			Calldata:      []*felt.Felt{felt.NewUnsafeFromString[felt.Felt]("0x1")},
			Version:       rpc.TransactionV3,
			Signature:     []*felt.Felt{},
			Nonce:         felt.NewUnsafeFromString[felt.Felt]("0x1"),
			ResourceBounds: &rpc.ResourceBoundsMapping{
				L1Gas:     zeroBounds,
				L1DataGas: zeroBounds,
				L2Gas:     zeroBounds,
			},
			Tip:                   "0x0",
			PayMasterData:         []*felt.Felt{},
			AccountDeploymentData: []*felt.Felt{},
			NonceDataMode:         rpc.DAModeL1,
			FeeMode:               rpc.DAModeL1,
		},
		ChainID: new(felt.Felt).SetBytes([]byte("SN_SEPOLIA")),
	}
	body, err := json.Marshal(request)
	require.NoError(t, err)

	return string(body)
}

func TestListen(t *testing.T) {
	newSigner := func(t *testing.T) (signer.Signer, *observer.ObservedLogs) {
		t.Helper()

		core, logs := observer.New(zapcore.InfoLevel)
		testSigner, err := signer.New("0x123", log.NewZapLoggerWithCore(core))
		require.NoError(t, err)

		return testSigner, logs
	}

	t.Run("Bind failure is returned without logging that the server runs", func(t *testing.T) {
		var listenConfig net.ListenConfig
		listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer func() { require.NoError(t, listener.Close()) }()

		testSigner, logs := newSigner(t)
		err = testSigner.Listen(t.Context(), listener.Addr().String())
		require.ErrorContains(t, err, "address already in use")
		require.Zero(t, logs.FilterMessage("server running").Len())
	})

	t.Run("Server serves until the context is done, then stops", func(t *testing.T) {
		testSigner, logs := newSigner(t)
		ctx, cancel := context.WithCancel(t.Context())
		listenErr := make(chan error, 1)
		go func() {
			listenErr <- testSigner.Listen(ctx, "127.0.0.1:0")
		}()

		address := listenAddress(t, logs)

		req, err := http.NewRequestWithContext(
			t.Context(), http.MethodPost, "http://"+address+signer.SignEndpoint,
			bytes.NewBufferString(validSignRequest(t)),
		)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.NoError(t, resp.Body.Close())

		cancel()
		select {
		case err := <-listenErr:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			require.FailNow(t, "the server didn't stop")
		}
		require.Equal(t, 1, logs.FilterMessage("Shutting down the server").Len())
	})
}
