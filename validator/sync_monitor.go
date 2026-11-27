package validator

import (
	"context"
	"time"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet.go/rpc"
	"go.uber.org/zap"
)

const (
	// Juno refreshes the chain head it knows of from the feeder gateway once a minute
	syncMonitorInterval       = time.Minute
	blocksBehindWarnThreshold = 10
	// While the node is behind, one warning every 5 polls (5 minutes)
	syncWarnEvery = 5
)

// Watches how far the node is behind the chain head reported by `starknet_syncing`
type SyncMonitor struct {
	provider rpc.RPCProvider
	logger   log.Logger
	tracer   metrics.Tracer
	// Consecutive polls with the node at least `blocksBehindWarnThreshold` blocks behind
	behindPolls int
}

func NewSyncMonitor(
	provider rpc.RPCProvider, logger log.Logger, tracer metrics.Tracer,
) *SyncMonitor {
	return &SyncMonitor{provider: provider, logger: logger, tracer: tracer, behindPolls: 0}
}

// Checks every minute, until ctx is done, how far the node is behind the chain head
func (m *SyncMonitor) Run(ctx context.Context) {
	ticker := time.NewTicker(syncMonitorInterval)
	defer ticker.Stop()

	for {
		m.Check(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Records how far the node is behind the chain head, warning when it is too far behind
func (m *SyncMonitor) Check(ctx context.Context) {
	status, err := m.provider.Syncing(ctx)
	if err != nil {
		if ctx.Err() == nil {
			m.logger.Warn(
				"failed to check if the node is synced",
				zap.Error(redactErrorURLs(err)),
			)
		}

		return
	}

	behind := blocksBehind(status.CurrentBlockNum, status.HighestBlockNum)
	m.tracer.UpdateNodeBlocksBehind(behind)

	if behind < blocksBehindWarnThreshold {
		if m.behindPolls > 0 {
			m.logger.Info("node caught up with the chain head", zap.Uint64("blocksBehind", behind))
		}
		m.behindPolls = 0

		return
	}

	m.behindPolls++
	if m.behindPolls%syncWarnEvery == 1 {
		m.logger.Warn(
			"node is behind the chain head. Attestations may be late or missed",
			zap.Uint64("currentBlock", status.CurrentBlockNum),
			zap.Uint64("highestBlock", status.HighestBlockNum),
			zap.Uint64("blocksBehind", behind),
		)
	}
}
