package validator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// Records the blocks behind updates, ignoring the rest
type blocksBehindTracer struct {
	metrics.NoOpMetrics

	updates []uint64
}

func (b *blocksBehindTracer) UpdateNodeBlocksBehind(blocks uint64) {
	b.updates = append(b.updates, blocks)
}

func TestSyncMonitor(t *testing.T) {
	const (
		behindLine   = "node is behind the chain head. Attestations may be late or missed"
		caughtUpLine = "node caught up with the chain head"
	)
	synced := rpc.SyncStatus{IsSyncing: false}
	farBehind := rpc.SyncStatus{IsSyncing: true, CurrentBlockNum: 100, HighestBlockNum: 350}

	newMonitor := func(t *testing.T) (
		*validator.SyncMonitor, *mocks.MockRPCProvider, *blocksBehindTracer, *observer.ObservedLogs,
	) {
		t.Helper()

		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		tracer := new(blocksBehindTracer)
		core, logs := observer.New(zapcore.InfoLevel)
		monitor := validator.NewSyncMonitor(provider, log.NewZapLoggerWithCore(core), tracer)

		return monitor, provider, tracer, logs
	}

	t.Run("Node synced", func(t *testing.T) {
		monitor, provider, tracer, logs := newMonitor(t)
		provider.EXPECT().Syncing(gomock.Any()).Return(synced, nil)

		monitor.Check(t.Context())

		require.Equal(t, []uint64{0}, tracer.updates)
		require.Empty(t, logs.All())
	})

	t.Run("Node behind by less than the threshold", func(t *testing.T) {
		monitor, provider, tracer, logs := newMonitor(t)
		provider.EXPECT().Syncing(gomock.Any()).Return(rpc.SyncStatus{
			IsSyncing: true, CurrentBlockNum: 100, HighestBlockNum: 105,
		}, nil)

		monitor.Check(t.Context())

		require.Equal(t, []uint64{5}, tracer.updates)
		require.Empty(t, logs.All())
	})

	t.Run("Node behind for a while, then caught up", func(t *testing.T) {
		monitor, provider, tracer, logs := newMonitor(t)
		const behindPolls = 6
		gomock.InOrder(
			provider.EXPECT().Syncing(gomock.Any()).Return(farBehind, nil).Times(behindPolls),
			provider.EXPECT().Syncing(gomock.Any()).Return(synced, nil),
		)

		for range behindPolls + 1 {
			monitor.Check(t.Context())
		}

		require.Equal(t, []uint64{250, 250, 250, 250, 250, 250, 0}, tracer.updates)
		entries := logs.All()
		require.Len(t, entries, 3)
		for _, entry := range entries[:2] {
			require.Equal(t, zapcore.WarnLevel, entry.Level)
			require.Equal(t, behindLine, entry.Message)
			require.Equal(t, map[string]any{
				"currentBlock": uint64(100),
				"highestBlock": uint64(350),
				"blocksBehind": uint64(250),
			}, entry.ContextMap())
		}
		require.Equal(t, zapcore.InfoLevel, entries[2].Level)
		require.Equal(t, caughtUpLine, entries[2].Message)
		require.Equal(t, map[string]any{"blocksBehind": uint64(0)}, entries[2].ContextMap())
	})

	t.Run("Syncing request fails", func(t *testing.T) {
		monitor, provider, tracer, logs := newMonitor(t)
		provider.EXPECT().Syncing(gomock.Any()).Return(rpc.SyncStatus{}, errors.New("boom"))

		monitor.Check(t.Context())

		require.Empty(t, tracer.updates)
		entries := logs.All()
		require.Len(t, entries, 1)
		require.Equal(t, zapcore.WarnLevel, entries[0].Level)
		require.Equal(t, "failed to check if the node is synced", entries[0].Message)
		require.Equal(t, map[string]any{"error": "boom"}, entries[0].ContextMap())
	})

	t.Run("Syncing request fails after the context is cancelled", func(t *testing.T) {
		monitor, provider, tracer, logs := newMonitor(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		provider.EXPECT().Syncing(gomock.Any()).Return(rpc.SyncStatus{}, context.Canceled)

		monitor.Check(ctx)

		require.Empty(t, tracer.updates)
		require.Empty(t, logs.All())
	})

	t.Run("Run stops when the context is cancelled", func(t *testing.T) {
		provider := mocks.NewMockRPCProvider(gomock.NewController(t))
		provider.EXPECT().Syncing(gomock.Any()).Return(synced, nil).AnyTimes()
		tracer := new(blocksBehindTracer)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		validator.NewSyncMonitor(provider, log.NewNopZapLogger(), tracer).Run(ctx)

		require.Equal(t, []uint64{0}, tracer.updates)
	})
}
