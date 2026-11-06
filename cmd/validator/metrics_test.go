package main

import (
	"net"
	"testing"
	"time"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/stretchr/testify/require"
)

func TestStartMetricsServer(t *testing.T) {
	t.Run("A server that cannot listen reports the error", func(t *testing.T) {
		var listenConfig net.ListenConfig
		listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer func() { require.NoError(t, listener.Close()) }()

		_, failed, stop := startMetricsServer(
			t.Context(), listener.Addr().String(), "SN_SEPOLIA", log.NewNopZapLogger(),
		)
		defer stop()

		select {
		case err := <-failed:
			require.ErrorContains(t, err, "address already in use")
		case <-time.After(5 * time.Second):
			require.FailNow(t, "the metrics server failure was not reported")
		}
	})

	t.Run("Stopping a running server reports no error", func(t *testing.T) {
		_, failed, stop := startMetricsServer(
			t.Context(), "127.0.0.1:0", "SN_SEPOLIA", log.NewNopZapLogger(),
		)
		time.Sleep(50 * time.Millisecond)
		stop()

		select {
		case err := <-failed:
			require.FailNow(t, "unexpected metrics server error", err)
		case <-time.After(100 * time.Millisecond):
		}
	})
}
