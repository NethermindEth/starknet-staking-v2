package main_test

import (
	"context"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	main "github.com/NethermindEth/starknet-staking-v2"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet.go/rpc"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/sourcegraph/conc"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestProcessBlockHeaders(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)

	t.Run("Simple scenario: 1 epoch", func(t *testing.T) {
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter]()
		headersFeed := make(chan *rpc.BlockHeader)

		epochId := uint64(1516)
		epochStartingBlock := uint64(639270)
		mockFetchedEpochAndAttestInfo(t, mockAccount, epochId, epochStartingBlock)

		targetBlockHash := utils.HexToFelt(t, "0x123")

		// Headers feeder routine
		wgFeed := conc.NewWaitGroup()
		wgFeed.Go(func() {
			feedStart := uint64(639270)
			feedEnd := feedStart + 40
			expectedTargetBlock := uint64(639291)
			sendHeaders(t, headersFeed, feedStart, feedEnd, expectedTargetBlock, targetBlockHash)
			close(headersFeed) // close channel once headers are sent to terminate ProcessBlockHeaders
		})

		// Events receiver routine
		receivedEvents := make(map[main.AttestRequired]uint)
		wgDispatcher := conc.NewWaitGroup()
		wgDispatcher.Go(func() { registerReceivedEvents(t, &dispatcher, receivedEvents) })

		main.ProcessBlockHeaders(headersFeed, mockAccount, &dispatcher)

		// No need to wait for wgFeed routine as it'll be the 1st closed, causing ProcessBlockHeaders to have returned
		// Still calling it just in case.
		wgFeed.Wait()

		// Will terminate the registerReceivedEvents routine
		close(dispatcher.AttestRequired)
		wgDispatcher.Wait()

		// Assert
		require.Equal(t, 1, len(receivedEvents))

		actualCount, exists := receivedEvents[main.AttestRequired{BlockHash: main.BlockHash(*targetBlockHash)}]
		require.True(t, exists)
		require.Equal(t, uint(16-main.MIN_ATTESTATION_WINDOW+1), actualCount)
	})

	t.Run("Scenario: transition between 2 epochs", func(t *testing.T) {
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter]()
		headersFeed := make(chan *rpc.BlockHeader)

		epochId := uint64(1516)
		epochStartingBlock := uint64(639270)
		mockFetchedEpochAndAttestInfo(t, mockAccount, epochId, epochStartingBlock)

		epochId = uint64(1517)
		epochStartingBlock = uint64(639310)
		mockFetchedEpochAndAttestInfo(t, mockAccount, epochId, epochStartingBlock)

		targetBlockHashEpoch1 := utils.HexToFelt(t, "0x123")
		targetBlockHashEpoch2 := utils.HexToFelt(t, "0x456")

		// Headers feeder routine
		wgFeed := conc.NewWaitGroup()
		wgFeed.Go(func() {
			feedStart := uint64(639270)
			feedEnd := feedStart + 40
			expectedTargetBlock := uint64(639291)
			sendHeaders(t, headersFeed, feedStart, feedEnd, expectedTargetBlock, targetBlockHashEpoch1)

			feedStart = uint64(639310)
			feedEnd = feedStart + 40
			expectedTargetBlock = uint64(639316)
			sendHeaders(t, headersFeed, feedStart, feedEnd, expectedTargetBlock, targetBlockHashEpoch2)

			close(headersFeed) // close channel once headers are sent to terminate ProcessBlockHeaders
		})

		// Events receiver routine
		receivedEvents := make(map[main.AttestRequired]uint)
		wgDispatcher := conc.NewWaitGroup()
		wgDispatcher.Go(func() { registerReceivedEvents(t, &dispatcher, receivedEvents) })

		main.ProcessBlockHeaders(headersFeed, mockAccount, &dispatcher)

		// No need to wait for wgFeed routine as it'll be the 1st closed, causing ProcessBlockHeaders to have returned
		// Still calling it just in case.
		wgFeed.Wait()

		// Will terminate the registerReceivedEvents routine
		close(dispatcher.AttestRequired)
		wgDispatcher.Wait()

		// Assert
		require.Equal(t, 2, len(receivedEvents))

		countEpoch1, exists := receivedEvents[main.AttestRequired{BlockHash: main.BlockHash(*targetBlockHashEpoch1)}]
		require.True(t, exists)
		require.Equal(t, uint(16-main.MIN_ATTESTATION_WINDOW+1), countEpoch1)

		countEpoch2, exists := receivedEvents[main.AttestRequired{BlockHash: main.BlockHash(*targetBlockHashEpoch2)}]
		require.True(t, exists)
		require.Equal(t, uint(16-main.MIN_ATTESTATION_WINDOW+1), countEpoch2)
	})

	// TODO: Add test "Error: transition between 2 epochs" once logger is implemented
	// TODO: Add test with error when calling FetchEpochAndAttestInfo
}

// Test helper function to send headers
func sendHeaders(t *testing.T, headersFeed chan *rpc.BlockHeader, start, end, targetBlock uint64, targetBlockHash *felt.Felt) {
	t.Helper()

	for i := start; i < end; i++ {
		// Set (only) expected target block hash
		blockHash := new(felt.Felt).SetUint64(1)
		if i == targetBlock {
			blockHash = targetBlockHash
		}

		headersFeed <- &rpc.BlockHeader{
			BlockNumber: i,
			BlockHash:   blockHash,
		}
	}
}

// Test helper function to register received events to assert on them
// Note: to exit this function, just close the channel
func registerReceivedEvents[T main.Accounter](
	t *testing.T,
	dispatcher *main.EventDispatcher[T],
	receivedAttestRequired map[main.AttestRequired]uint,
) {
	t.Helper()

	for {
		select {
		case attestRequired, isOpen := <-dispatcher.AttestRequired:
			if !isOpen {
				return
			}
			// register attestRequired event
			count, _ := receivedAttestRequired[attestRequired]
			// even if the key does not exist, the count will be 0 by default
			receivedAttestRequired[attestRequired] = count + 1
		}
	}
}

// Test helper function to mock fetched epoch and attest info
func mockFetchedEpochAndAttestInfo(t *testing.T, mockAccount *mocks.MockAccounter, epochId, epochStartingBlock uint64) {
	t.Helper()

	// Mock fetchEpochInfo call
	validatorOperationalAddress := utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
	mockAccount.EXPECT().Address().Return(validatorOperationalAddress)

	stakerAddress := utils.HexToFelt(t, "0x123") // does not matter, is not used anyway
	stake := uint64(1000000000000000000)
	epochLen := uint64(40)

	expectedEpochInfoFnCall := rpc.FunctionCall{
		ContractAddress:    utils.HexToFelt(t, main.STAKING_CONTRACT_ADDRESS),
		EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("get_attestation_info_by_operational_address"),
		Calldata:           []*felt.Felt{validatorOperationalAddress},
	}

	mockAccount.
		EXPECT().
		Call(context.Background(), expectedEpochInfoFnCall, rpc.BlockID{Tag: "latest"}).
		Return(
			[]*felt.Felt{
				stakerAddress,
				new(felt.Felt).SetUint64(stake),
				new(felt.Felt).SetUint64(epochLen),
				new(felt.Felt).SetUint64(epochId),
				new(felt.Felt).SetUint64(epochStartingBlock),
			},
			nil,
		)

	// Mock fetchAttestWindow call
	expectedWindowFnCall := rpc.FunctionCall{
		ContractAddress:    utils.HexToFelt(t, main.ATTEST_CONTRACT_ADDRESS),
		EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("attestation_window"),
		Calldata:           []*felt.Felt{},
	}

	attestWindow := uint64(16)
	mockAccount.
		EXPECT().
		Call(context.Background(), expectedWindowFnCall, rpc.BlockID{Tag: "latest"}).
		Return([]*felt.Felt{new(felt.Felt).SetUint64(attestWindow)}, nil)

	// Mock ComputeBlockNumberToAttestTo call
	mockAccount.EXPECT().Address().Return(validatorOperationalAddress)
}
