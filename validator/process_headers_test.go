package validator_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
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

func TestProcessBlockHeadersLogsBlockAfterEpochSwitch(t *testing.T) {
	var buf bytes.Buffer
	logger, err := log.NewZapLogger(
		log.NewLevel(log.INFO), log.WithWriter(&buf), log.WithColour(false),
	)
	require.NoError(t, err)
	mockSigner := newEpochSigner(t, 0, 1)
	dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
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
