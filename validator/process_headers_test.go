package validator_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const (
	testEpochID    = 100
	testEpochLen   = 1000
	testEpochStart = 5000
	testWindowLen  = 40
)

func newEpochSigner(t *testing.T, epochOffsets ...uint64) *mocks.MockSigner {
	t.Helper()

	return newFailingEpochSigner(t, 0, epochOffsets...)
}

func newFailingEpochSigner(
	t *testing.T, failures int, epochOffsets ...uint64,
) *mocks.MockSigner {
	t.Helper()

	var epochCalls sync.Mutex
	epochOffset := func() (uint64, bool) {
		epochCalls.Lock()
		defer epochCalls.Unlock()

		if failures > 0 {
			failures--

			return 0, false
		}
		if len(epochOffsets) == 0 {
			return 0, true
		}
		offset := epochOffsets[0]
		if len(epochOffsets) > 1 {
			epochOffsets = epochOffsets[1:]
		}

		return offset, true
	}

	operationalAddress, err := types.AddressFromString("0x123")
	require.NoError(t, err)

	mockSigner := mocks.NewMockSigner(gomock.NewController(t))
	mockSigner.EXPECT().Address().Return(&operationalAddress).AnyTimes()
	mockSigner.EXPECT().
		ValidationContracts().
		Return(validator.SepoliaValidationContracts(t)).
		AnyTimes()
	mockSigner.EXPECT().
		Call(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context, call rpc.FunctionCall, _ rpc.BlockID,
		) ([]*felt.Felt, error) {
			isWindowCall := call.EntryPointSelector.Equal(
				snGoUtils.GetSelectorFromNameFelt("attestation_window"),
			)
			if isWindowCall {
				return []*felt.Felt{new(felt.Felt).SetUint64(testWindowLen)}, nil
			}

			offset, ok := epochOffset()
			if !ok {
				return nil, errors.New("503 Service Unavailable: injected outage")
			}

			return []*felt.Felt{
				new(felt.Felt).SetUint64(0x456),
				new(felt.Felt).SetUint64(1),
				new(felt.Felt).SetUint64(testEpochLen),
				new(felt.Felt).SetUint64(testEpochID + offset),
				new(felt.Felt).SetUint64(testEpochStart + offset*testEpochLen),
			}, nil
		}).
		AnyTimes()
	mockSigner.EXPECT().
		BlockWithTxHashes(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("block not found")).
		AnyTimes()

	return mockSigner
}

type eventCounter struct {
	wg          sync.WaitGroup
	doAttest    int
	endOfWindow int
}

func countEvents(dispatcher *validator.EventDispatcher[*mocks.MockSigner]) *eventCounter {
	counter := new(eventCounter)
	counter.wg.Go(func() {
		for range dispatcher.DoAttest {
			counter.doAttest++
		}
	})
	counter.wg.Go(func() {
		for range dispatcher.EndOfWindow {
			counter.endOfWindow++
		}
	})
	counter.wg.Go(func() {
		for range dispatcher.PrepareAttest {
		}
	})

	return counter
}

func headersFeed(blockNumbers ...uint64) chan *rpc.BlockHeader {
	feed := make(chan *rpc.BlockHeader, len(blockNumbers))
	for _, number := range blockNumbers {
		feed <- &rpc.BlockHeader{Number: number, Hash: new(felt.Felt).SetUint64(number)}
	}
	close(feed)

	return feed
}

func TestProcessBlockHeadersSkipsReplayedHeaders(t *testing.T) {
	mockSigner := newEpochSigner(t)
	logger := log.NewNopZapLogger()

	_, attestInfo, err := signerP.FetchEpochAndAttestInfo(t.Context(), mockSigner, logger)
	require.NoError(t, err)
	windowEnd := attestInfo.WindowEnd.Uint64()
	require.Less(t, windowEnd+1, uint64(testEpochStart+testEpochLen))

	dispatcher := validator.NewEventDispatcher[*mocks.MockSigner](testMaxTip)
	counter := countEvents(&dispatcher)

	err = validator.ProcessBlockHeaders(
		t.Context(), headersFeed(windowEnd-1, windowEnd), mockSigner, logger, &dispatcher,
		types.Retries{}, metrics.NewNoOpMetrics(),
	)
	require.NoError(t, err)
	err = validator.ProcessBlockHeaders(
		t.Context(), headersFeed(windowEnd, windowEnd-1, windowEnd+1), mockSigner, logger,
		&dispatcher, types.Retries{}, metrics.NewNoOpMetrics(),
	)
	require.NoError(t, err)

	close(dispatcher.DoAttest)
	close(dispatcher.EndOfWindow)
	close(dispatcher.PrepareAttest)
	counter.wg.Wait()

	require.Equal(t, 1, counter.doAttest)
	require.Equal(t, 1, counter.endOfWindow)
}

func TestProcessBlockHeadersLogsBlockAfterEpochSwitch(t *testing.T) {
	var buf bytes.Buffer
	logger, err := log.NewZapLogger(
		log.NewLevel(log.INFO), log.WithWriter(&buf), log.WithColour(false),
	)
	require.NoError(t, err)
	mockSigner := newEpochSigner(t, 0, 1)
	dispatcher := validator.NewEventDispatcher[*mocks.MockSigner](testMaxTip)
	counter := countEvents(&dispatcher)

	nextEpochStart := uint64(testEpochStart + testEpochLen)
	err = validator.ProcessBlockHeaders(
		t.Context(), headersFeed(nextEpochStart-1, nextEpochStart), mockSigner, logger,
		&dispatcher, types.Retries{}, metrics.NewNoOpMetrics(),
	)
	require.NoError(t, err)
	close(dispatcher.DoAttest)
	close(dispatcher.EndOfWindow)
	close(dispatcher.PrepareAttest)
	counter.wg.Wait()

	out := buf.String()
	blockLine := fmt.Sprintf("block %d received, ", nextEpochStart)
	require.Contains(t, out, blockLine)
	require.NotContains(t, out, blockLine+"0 blocks for the next epoch")
	newEpochLine := fmt.Sprintf(`"epochID": %d,`, testEpochID+1)
	require.Contains(t, out, newEpochLine)
	require.Less(t, strings.Index(out, newEpochLine), strings.Index(out, blockLine))
}

type epochFetchTracer struct {
	metrics.NoOpMetrics

	fetchFailures int
}

func (e *epochFetchTracer) RecordEpochInfoFetchFailure() { e.fetchFailures++ }

func TestFetchEpochAndAttestInfoWithRetryReportsFailures(t *testing.T) {
	sleep := validator.Sleep
	validator.Sleep = func(time.Duration) {}
	t.Cleanup(func() { validator.Sleep = sleep })

	countLevel := func(out, level string) int {
		return strings.Count(out, "\t"+level+"\t")
	}

	t.Run("Outage that ends", func(t *testing.T) {
		var buf bytes.Buffer
		logger, err := log.NewZapLogger(
			log.NewLevel(log.INFO), log.WithWriter(&buf), log.WithColour(false),
		)
		require.NoError(t, err)
		tracer := new(epochFetchTracer)

		epochInfo, _, err := validator.FetchEpochAndAttestInfoWithRetry(
			t.Context(), newFailingEpochSigner(t, 21), logger, nil,
			func(*types.EpochInfo, *types.EpochInfo) bool { return true },
			types.NewRetries(), "100", tracer,
		)

		require.NoError(t, err)
		require.Equal(t, uint64(testEpochID), epochInfo.EpochID)
		require.Equal(t, 21, tracer.fetchFailures)
		out := buf.String()
		require.Equal(t, 3, countLevel(out, "WARN"), out)
		require.Contains(t, out, "injected outage")
		require.Contains(t, out, `"retriesLeft": "infinite"`)
		require.Contains(t, out, `"epochID": 100,`)
		require.Contains(t, out, "Resuming block processing")
	})

	t.Run("Outage outlasting the retries", func(t *testing.T) {
		tracer := new(epochFetchTracer)
		retries, err := types.RetriesFromString("2")
		require.NoError(t, err)

		_, _, err = validator.FetchEpochAndAttestInfoWithRetry(
			t.Context(), newFailingEpochSigner(t, 3), log.NewNopZapLogger(), nil,
			func(*types.EpochInfo, *types.EpochInfo) bool { return true },
			retries, "100", tracer,
		)

		require.ErrorContains(t, err, "failed to fetch epoch info after 2 retries")
		require.Equal(t, 3, tracer.fetchFailures)
	})
}
