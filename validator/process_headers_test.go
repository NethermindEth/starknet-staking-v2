package validator_test

import (
	"sync"
	"testing"

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

// Signer mock answering the epoch and attestation window calls with a fixed epoch
func newEpochSigner(t *testing.T) *mocks.MockSigner {
	t.Helper()

	operationalAddress, err := types.AddressFromString("0x123")
	require.NoError(t, err)

	mockSigner := mocks.NewMockSigner(gomock.NewController(t))
	mockSigner.EXPECT().Address().Return(&operationalAddress).AnyTimes()
	mockSigner.EXPECT().
		ValidationContracts().
		Return(validator.SepoliaValidationContracts(t)).
		AnyTimes()
	mockSigner.EXPECT().
		Call(gomock.Any(), gomock.Any()).
		DoAndReturn(func(call rpc.FunctionCall, _ rpc.BlockID) ([]*felt.Felt, error) {
			if call.EntryPointSelector.Equal(
				snGoUtils.GetSelectorFromNameFelt("attestation_window"),
			) {
				return []*felt.Felt{new(felt.Felt).SetUint64(testWindowLen)}, nil
			}

			return []*felt.Felt{
				new(felt.Felt).SetUint64(0x456), // staker address
				new(felt.Felt).SetUint64(1),     // stake
				new(felt.Felt).SetUint64(testEpochLen),
				new(felt.Felt).SetUint64(testEpochID),
				new(felt.Felt).SetUint64(testEpochStart),
			}, nil
		}).
		AnyTimes()
	// The target block is not known yet
	mockSigner.EXPECT().
		BlockWithTxHashes(gomock.Any()).
		Return(nil, errors.New("block not found")).
		AnyTimes()

	return mockSigner
}

// Counts the events sent to the dispatcher, until its channels are closed
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
		// Only draining the channel
		for range dispatcher.PrepareAttest {
		}
	})

	return counter
}

// Returns a closed feed with the headers of the given blocks
func headersFeed(blockNumbers ...uint64) chan *rpc.BlockHeader {
	feed := make(chan *rpc.BlockHeader, len(blockNumbers))
	for _, number := range blockNumbers {
		feed <- &rpc.BlockHeader{Number: number, Hash: new(felt.Felt).SetUint64(number)}
	}
	close(feed)

	return feed
}

// The node replays its latest header on every new subscription. A header already processed by
// the routine of a previous subscription must not dispatch its events again: a second
// EndOfWindow gave a false attestation failure and a second reward claim (QA report 03,
// finding AC-2).
func TestProcessBlockHeadersSkipsReplayedHeaders(t *testing.T) {
	mockSigner := newEpochSigner(t)
	logger := log.NewNopZapLogger()

	_, attestInfo, err := signerP.FetchEpochAndAttestInfo(mockSigner, logger)
	require.NoError(t, err)
	windowEnd := attestInfo.WindowEnd.Uint64()
	require.Less(t, windowEnd+1, uint64(testEpochStart+testEpochLen))

	dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
	counter := countEvents(&dispatcher)

	// 1st subscription: the last block of the window and the window end
	err = validator.ProcessBlockHeaders(
		t.Context(), headersFeed(windowEnd-1, windowEnd), mockSigner, logger, &dispatcher,
		types.Retries{}, metrics.NewNoOpMetrics(),
	)
	require.NoError(t, err)
	// 2nd subscription: the window end is replayed, older headers are skipped as well
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
