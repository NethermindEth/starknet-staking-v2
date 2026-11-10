package metrics_test

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestBalanceSettersDebugLog(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	m := metrics.NewMetrics("127.0.0.1:0", "SN_SEPOLIA", log.NewZapLoggerWithCore(core))

	m.UpdateSignerBalance(1)
	m.UpdateRewardBalance(2)

	messages := make([]string, 0, logs.Len())
	for _, entry := range logs.All() {
		messages = append(messages, entry.Message)
	}
	require.Equal(t, []string{"UpdateSignerBalance", "UpdateRewardBalance"}, messages)
}

func TestRecordBalanceCheckFailure(t *testing.T) {
	address := freeAddress(t)
	m := metrics.NewMetrics(address, "SN_SEPOLIA", log.NewNopZapLogger())
	served := make(chan error, 1)
	go func() { served <- m.Start() }()
	defer func() {
		require.NoError(t, m.Stop(t.Context()))
		require.ErrorIs(t, <-served, http.ErrServerClosed)
	}()

	m.RecordBalanceCheckFailure("signer")
	m.RecordBalanceCheckFailure("signer")
	m.RecordBalanceCheckFailure("reward")

	const name = "validator_attestation_balance_check_failure_count"
	exposition := scrapeMetrics(t, address)
	require.Contains(t, exposition, name+`{account="signer",network="SN_SEPOLIA"} 2`+"\n")
	require.Contains(t, exposition, name+`{account="reward",network="SN_SEPOLIA"} 1`+"\n")
}

func freeAddress(t *testing.T) string {
	t.Helper()

	var config net.ListenConfig
	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	return address
}

func scrapeMetrics(t *testing.T, address string) string {
	t.Helper()

	var exposition string
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		request, err := http.NewRequestWithContext(
			t.Context(), http.MethodGet, "http://"+address+"/metrics", http.NoBody,
		)
		require.NoError(c, err)
		response, err := http.DefaultClient.Do(request)
		require.NoError(c, err)
		body, err := io.ReadAll(response.Body)
		require.NoError(c, response.Body.Close())
		require.NoError(c, err)
		require.Equal(c, http.StatusOK, response.StatusCode)
		exposition = string(body)
	}, 5*time.Second, 10*time.Millisecond)

	return exposition
}

func TestUpdateEpochInfoDebugLog(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	m := metrics.NewMetrics("127.0.0.1:0", "SN_SEPOLIA", log.NewZapLoggerWithCore(core))

	m.UpdateEpochInfo(&types.EpochInfo{
		EpochLen:      231,
		EpochID:       79314,
		StartingBlock: types.BlockNumber(17749000),
	}, 17749100)

	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, map[string]any{
		"epochID":            uint64(79314),
		"epochLength":        uint64(231),
		"epochStartingBlock": uint64(17749000),
		"targetBlock":        uint64(17749100),
	}, entries[0].ContextMap())
}

func TestRecordEpochInfoFetchFailure(t *testing.T) {
	address := freeAddress(t)
	m := metrics.NewMetrics(address, "SN_SEPOLIA", log.NewNopZapLogger())
	served := make(chan error, 1)
	go func() { served <- m.Start() }()
	defer func() {
		require.NoError(t, m.Stop(t.Context()))
		require.ErrorIs(t, <-served, http.ErrServerClosed)
	}()

	m.RecordEpochInfoFetchFailure()
	m.RecordEpochInfoFetchFailure()

	require.Contains(
		t,
		scrapeMetrics(t, address),
		`validator_attestation_epoch_info_fetch_failure_count{network="SN_SEPOLIA"} 2`,
	)
}
