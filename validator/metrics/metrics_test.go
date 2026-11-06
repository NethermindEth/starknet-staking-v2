package metrics

import (
	"maps"
	"testing"

	"github.com/NethermindEth/juno/utils/log"
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
