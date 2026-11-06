package metrics

import (
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestBalanceSettersDebugLog(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	m := NewMetrics("127.0.0.1:0", "SN_SEPOLIA", log.NewZapLoggerWithCore(core))

	m.UpdateSignerBalance(1)
	m.UpdateRewardBalance(2)

	messages := make([]string, 0, logs.Len())
	for _, entry := range logs.All() {
		messages = append(messages, entry.Message)
	}
	require.Equal(t, []string{"UpdateSignerBalance", "UpdateRewardBalance"}, messages)
}

func TestRecordBalanceCheckFailure(t *testing.T) {
	m := NewMetrics("127.0.0.1:0", "SN_SEPOLIA", log.NewNopZapLogger())

	m.RecordBalanceCheckFailure("signer")
	m.RecordBalanceCheckFailure("signer")
	m.RecordBalanceCheckFailure("reward")

	const name = "validator_attestation_balance_check_failure_count"
	require.InDelta(t, 2.0, counterValue(t, m, name, map[string]string{
		"network": "SN_SEPOLIA", "account": "signer",
	}), 0)
	require.InDelta(t, 1.0, counterValue(t, m, name, map[string]string{
		"network": "SN_SEPOLIA", "account": "reward",
	}), 0)
}

func counterValue(t *testing.T, m *Metrics, name string, labels map[string]string) float64 {
	t.Helper()

	families, err := m.registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			metricLabels := make(map[string]string)
			for _, label := range metric.GetLabel() {
				metricLabels[label.GetName()] = label.GetValue()
			}
			if maps.Equal(metricLabels, labels) {
				return metric.GetCounter().GetValue()
			}
		}
	}
	require.FailNow(t, "metric not found", "%s %v", name, labels)

	return 0
}

type failingWriter struct {
	httptest.ResponseRecorder
}

func (w *failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestHealthWriteErrorIsLoggedAsError(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	m := NewMetrics("127.0.0.1:0", "SN_SEPOLIA", log.NewZapLoggerWithCore(core))

	writer := &failingWriter{ResponseRecorder: *httptest.NewRecorder()}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", nil)
	m.server.Handler.ServeHTTP(writer, request)

	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, zapcore.ErrorLevel, entries[0].Level)
	require.Equal(t, "failed to write health check response", entries[0].Message)
	require.Equal(t, map[string]any{"error": "write failed"}, entries[0].ContextMap())
}

func TestUpdateEpochInfoDebugLog(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	m := NewMetrics("127.0.0.1:0", "SN_SEPOLIA", log.NewZapLoggerWithCore(core))

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
