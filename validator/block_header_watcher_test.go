package validator_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/gorilla/websocket"
	"github.com/sourcegraph/conc"
	"github.com/stretchr/testify/require"
)

func newWSNode(
	t *testing.T, maxSubscriptions int32, onSubscribed func(n int32, conn *websocket.Conn),
) (wsURL string, subscriptions *atomic.Int32) {
	t.Helper()

	subscriptions = new(atomic.Int32)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subscriptions.Load() >= maxSubscriptions {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		var request struct {
			ID json.RawMessage `json:"id"`
		}
		if conn.ReadJSON(&request) != nil {
			return
		}
		n := subscriptions.Add(1)
		reply := fmt.Sprintf(`{"jsonrpc": "2.0", "id": %s, "result": "%d"}`, request.ID, n)
		if conn.WriteMessage(websocket.TextMessage, []byte(reply)) != nil {
			return
		}
		onSubscribed(n, conn)
	}))
	t.Cleanup(server.Close)

	return "ws" + strings.TrimPrefix(server.URL, "http"), subscriptions
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func sendHeader(conn *websocket.Conn, subscriptionID int32, blockNumber uint64) {
	notification := fmt.Sprintf(
		`{"jsonrpc": "2.0", "method": "starknet_subscriptionNewHeads", "params": `+
			`{"subscription_id": "%d", "result": {"block_hash": "0x%x", "block_number": %d}}}`,
		subscriptionID, blockNumber, blockNumber,
	)
	_ = conn.WriteMessage(websocket.TextMessage, []byte(notification))
}

func recordSleeps(t *testing.T) func() []time.Duration {
	t.Helper()

	var mu sync.Mutex
	var sleeps []time.Duration
	sleep := validator.Sleep
	validator.Sleep = func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		sleeps = append(sleeps, d)
	}
	t.Cleanup(func() { validator.Sleep = sleep })

	return func() []time.Duration {
		mu.Lock()
		defer mu.Unlock()

		return append([]time.Duration(nil), sleeps...)
	}
}

func runWatcher(
	t *testing.T, wsURL string, logger log.Logger, retries string,
) error {
	t.Helper()

	maxRetries, err := types.RetriesFromString(retries)
	require.NoError(t, err)
	dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
	counter := countEvents(&dispatcher)
	wg := conc.NewWaitGroup()

	done := make(chan error, 1)
	go func() {
		done <- validator.RunBlockHeaderWatcher(
			t.Context(), wsURL, logger, newEpochSigner(t), &dispatcher, maxRetries, wg,
			metrics.NewNoOpMetrics(),
		)
	}()

	var watchErr error
	select {
	case watchErr = <-done:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the block header watcher did not stop")
	}
	wg.Wait()
	close(dispatcher.DoAttest)
	close(dispatcher.EndOfWindow)
	close(dispatcher.PrepareAttest)
	counter.wg.Wait()

	return watchErr
}

func TestRunBlockHeaderWatcherResubscriptions(t *testing.T) {
	t.Run("Subscriptions without new headers back off and use the retries", func(t *testing.T) {
		sleeps := recordSleeps(t)
		wsURL, subscriptions := newWSNode(t, 100, func(int32, *websocket.Conn) {})

		err := runWatcher(t, wsURL, log.NewNopZapLogger(), "3")

		require.ErrorContains(t, err, "ended 4 times in a row without a new block header")
		require.Equal(t, int32(4), subscriptions.Load())
		require.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}, sleeps())
	})

	t.Run("Subscriptions with new headers resubscribe at once", func(t *testing.T) {
		sleeps := recordSleeps(t)
		wsURL, subscriptions := newWSNode(t, 3, func(n int32, conn *websocket.Conn) {
			sendHeader(conn, n, testEpochStart+uint64(n))
			time.Sleep(50 * time.Millisecond)
		})
		buf := new(syncBuffer)
		logger, err := log.NewZapLogger(
			log.NewLevel(log.INFO), log.WithWriter(buf), log.WithColour(false),
		)
		require.NoError(t, err)

		err = runWatcher(t, wsURL, logger, "1")

		require.ErrorContains(t, err, "bad handshake")
		require.Equal(t, int32(3), subscriptions.Load())
		require.Equal(t, []time.Duration{5 * time.Second}, sleeps())
		out := buf.String()
		require.Equal(t, 1, strings.Count(out, "epoch started"), out)
	})
}
