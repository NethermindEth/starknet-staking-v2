package metrics

import (
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
