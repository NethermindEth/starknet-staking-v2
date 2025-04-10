package main_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	main "github.com/NethermindEth/starknet-staking-v2"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet.go/rpc"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/cockroachdb/errors"
	"github.com/sourcegraph/conc"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"lukechampine.com/uint128"
)

func TestProcessBlockHeaders(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)
	mockLogger := mocks.NewMockLogger(mockCtrl)

	t.Run("Simple scenario: 1 epoch", func(t *testing.T) {
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter, *mocks.MockLogger]()
		headersFeed := make(chan *rpc.BlockHeader)

		epochId := uint64(1516)
		epochLength := uint64(40)
		attestWindow := uint64(16)
		epochStartingBlock := main.BlockNumber(639270)
		expectedTargetBlock := main.BlockNumber(639291)
		mockFetchedEpochAndAttestInfo(t, mockAccount, mockLogger, epochId, epochLength, attestWindow, epochStartingBlock, expectedTargetBlock, 1)

		targetBlockHash := main.BlockHash(*utils.HexToFelt(t, "0x6d8dc0a8bdf98854b6bc146cb7cab6cddda85619c6ae2948ee65da25815e045"))
		blockHeaders := mockHeaderFeedWithLogger(t, mockLogger, epochStartingBlock, expectedTargetBlock, &targetBlockHash, epochLength, attestWindow)

		// Mock SetTargetBlockHashIfExists call
		targetBlockUint64 := expectedTargetBlock.Uint64()
		mockAccount.
			EXPECT().
			BlockWithTxHashes(context.Background(), rpc.BlockID{Number: &targetBlockUint64}).
			Return(nil, errors.New("Block not found")) // Let's say block does not exist yet

		// Headers feeder routine
		wgFeed := conc.NewWaitGroup()
		wgFeed.Go(func() {
			sendHeaders(t, headersFeed, blockHeaders)
			close(headersFeed) // close channel once headers are sent to terminate ProcessBlockHeaders
		})

		// Events receiver routine
		receivedAttestEvents := make(map[main.AttestRequired]uint)
		receivedEndOfWindowEvents := uint8(0)
		wgDispatcher := conc.NewWaitGroup()
		wgDispatcher.Go(func() { registerReceivedEvents(t, &dispatcher, receivedAttestEvents, &receivedEndOfWindowEvents) })

		main.ProcessBlockHeaders(headersFeed, mockAccount, mockLogger, &dispatcher)

		// No need to wait for wgFeed routine as it'll be the 1st closed, causing ProcessBlockHeaders to have returned
		// Still calling it just in case.
		wgFeed.Wait()

		// Will terminate the registerReceivedEvents routine
		close(dispatcher.AttestRequired)
		wgDispatcher.Wait()

		// Assert
		require.Equal(t, 1, len(receivedAttestEvents))

		actualCount, exists := receivedAttestEvents[main.AttestRequired{BlockHash: targetBlockHash}]
		require.True(t, exists)
		require.Equal(t, uint(attestWindow-main.MIN_ATTESTATION_WINDOW+1), actualCount)

		require.Equal(t, uint8(1), receivedEndOfWindowEvents)
	})

	t.Run("Scenario: transition between 2 epochs", func(t *testing.T) {
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter, *mocks.MockLogger]()
		headersFeed := make(chan *rpc.BlockHeader)

		epochLength := uint64(40)
		attestWindow := uint64(16)

		epochId1 := uint64(1516)
		epochStartingBlock1 := main.BlockNumber(639270)
		expectedTargetBlock1 := main.BlockNumber(639291)
		mockFetchedEpochAndAttestInfo(t, mockAccount, mockLogger, epochId1, epochLength, attestWindow, epochStartingBlock1, expectedTargetBlock1, 1)

		epochId2 := uint64(1517)
		epochStartingBlock2 := main.BlockNumber(639310)
		expectedTargetBlock2 := main.BlockNumber(639316)
		mockFetchedEpochAndAttestInfo(t, mockAccount, mockLogger, epochId2, epochLength, attestWindow, epochStartingBlock2, expectedTargetBlock2, 1)

		targetBlockHashEpoch1 := main.BlockHash(*utils.HexToFelt(t, "0x6d8dc0a8bdf98854b6bc146cb7cab6cddda85619c6ae2948ee65da25815e045"))
		blockHeaders1 := mockHeaderFeedWithLogger(t, mockLogger, epochStartingBlock1, expectedTargetBlock1, &targetBlockHashEpoch1, epochLength, attestWindow)

		targetBlockHashEpoch2 := main.BlockHash(*utils.HexToFelt(t, "0x2124ae375432a16ef644f539c3b148f63c706067bf576088f32033fe59c345e"))
		blockHeaders2 := mockHeaderFeedWithLogger(t, mockLogger, epochStartingBlock2, expectedTargetBlock2, &targetBlockHashEpoch2, epochLength, attestWindow)

		// Mock SetTargetBlockHashIfExists call
		targetBlockUint64 := expectedTargetBlock1.Uint64()
		mockAccount.
			EXPECT().
			BlockWithTxHashes(context.Background(), rpc.BlockID{Number: &targetBlockUint64}).
			Return(nil, errors.New("Block not found")) // Let's say block does not exist yet

		// Mock epoch switch log
		mockLogger.EXPECT().Infow("New epoch start", "epoch id", epochId2)

		// Headers feeder routine
		wgFeed := conc.NewWaitGroup()
		wgFeed.Go(func() {
			sendHeaders(t, headersFeed, blockHeaders1)
			sendHeaders(t, headersFeed, blockHeaders2)
			close(headersFeed) // close channel once headers are sent to terminate ProcessBlockHeaders
		})

		// Events receiver routine
		receivedAttestEvents := make(map[main.AttestRequired]uint)
		receivedEndOfWindowEvents := uint8(0)
		wgDispatcher := conc.NewWaitGroup()
		wgDispatcher.Go(func() { registerReceivedEvents(t, &dispatcher, receivedAttestEvents, &receivedEndOfWindowEvents) })

		main.ProcessBlockHeaders(headersFeed, mockAccount, mockLogger, &dispatcher)

		// No need to wait for wgFeed routine as it'll be the 1st closed, causing ProcessBlockHeaders to have returned
		// Still calling it just in case.
		wgFeed.Wait()

		// Will terminate the registerReceivedEvents routine
		close(dispatcher.AttestRequired)
		wgDispatcher.Wait()

		// Assert
		require.Equal(t, 2, len(receivedAttestEvents))

		countEpoch1, exists := receivedAttestEvents[main.AttestRequired{BlockHash: targetBlockHashEpoch1}]
		require.True(t, exists)
		require.Equal(t, uint(16-main.MIN_ATTESTATION_WINDOW+1), countEpoch1)

		countEpoch2, exists := receivedAttestEvents[main.AttestRequired{BlockHash: targetBlockHashEpoch2}]
		require.True(t, exists)
		require.Equal(t, uint(16-main.MIN_ATTESTATION_WINDOW+1), countEpoch2)

		require.Equal(t, uint8(2), receivedEndOfWindowEvents)
	})

	// Add those 2 once my way of managing those 2 errors has been confirmed
	// TODO: Add test "Error: transition between 2 epochs" once logger is implemented
	// TODO: Add test with error when calling FetchEpochAndAttestInfo
}

// Test helper function to send headers
func sendHeaders(t *testing.T, headersFeed chan *rpc.BlockHeader, blockHeaders []rpc.BlockHeader) {
	t.Helper()

	for i := range blockHeaders {
		headersFeed <- &blockHeaders[i]
	}
}

// Test helper function to register received events to assert on them
// Note: to exit this function, close the AttestRequired channel
func registerReceivedEvents[T main.Accounter, Log main.Logger](
	t *testing.T,
	dispatcher *main.EventDispatcher[T, Log],
	receivedAttestRequired map[main.AttestRequired]uint,
	receivedEndOfWindowCount *uint8,
) {
	t.Helper()

	for {
		select {
		case attestRequired, isOpen := <-dispatcher.AttestRequired:
			if !isOpen {
				return
			}
			// register attestRequired event
			// even if the key does not exist, the count will be 0 by default
			receivedAttestRequired[attestRequired]++
		case <-dispatcher.EndOfWindow:
			*receivedEndOfWindowCount++
		}
	}
}

// Test helper function to mock fetched epoch and attest info
func mockFetchedEpochAndAttestInfo(
	t *testing.T,
	mockAccount *mocks.MockAccounter,
	mockLogger *mocks.MockLogger,
	epochId,
	epochLength,
	attestWindow uint64,
	epochStartingBlock,
	targetBlockNumber main.BlockNumber,
	howManyTimes int,
) {
	t.Helper()

	// Mock fetchEpochInfo call
	validatorOperationalAddress := utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
	mockAccount.EXPECT().Address().Return(validatorOperationalAddress).Times(howManyTimes)

	stake := uint64(1000000000000000000)

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
				validatorOperationalAddress,
				new(felt.Felt).SetUint64(stake),
				new(felt.Felt).SetUint64(epochLength),
				new(felt.Felt).SetUint64(epochId),
				new(felt.Felt).SetUint64(epochStartingBlock.Uint64()),
			},
			nil,
		).
		Times(howManyTimes)

	// Mock logger following epoch info fetching
	mockLogger.EXPECT().Infow(
		"Fetched epoch info",
		"epoch ID", epochId,
		"epoch starting block", epochStartingBlock,
		"epoch ending block", epochStartingBlock+main.BlockNumber(epochLength),
	).Times(howManyTimes)

	// Mock fetchAttestWindow call
	expectedWindowFnCall := rpc.FunctionCall{
		ContractAddress:    utils.HexToFelt(t, main.ATTEST_CONTRACT_ADDRESS),
		EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("attestation_window"),
		Calldata:           []*felt.Felt{},
	}

	mockAccount.
		EXPECT().
		Call(context.Background(), expectedWindowFnCall, rpc.BlockID{Tag: "latest"}).
		Return([]*felt.Felt{new(felt.Felt).SetUint64(attestWindow)}, nil).
		Times(howManyTimes)

	// Mock ComputeBlockNumberToAttestTo call
	mockAccount.EXPECT().Address().Return(validatorOperationalAddress).Times(howManyTimes)

	// Mock logger following target block computation
	mockLogger.EXPECT().Infow(
		"Computed target block to attest to",
		"epoch ID", epochId,
		"attestation info", main.AttestInfo{
			TargetBlock: targetBlockNumber,
			WindowStart: targetBlockNumber + main.BlockNumber(main.MIN_ATTESTATION_WINDOW),
			WindowEnd:   targetBlockNumber + main.BlockNumber(attestWindow),
		},
	).Times(howManyTimes)
}

func mockHeaderFeedWithLogger(
	t *testing.T,
	mockLogger *mocks.MockLogger,
	startingBlock,
	targetBlock main.BlockNumber,
	targetBlockHash *main.BlockHash,
	epochLength,
	attestWindow uint64,
) []rpc.BlockHeader {
	t.Helper()

	blockHash := *new(felt.Felt).SetUint64(1)

	blockHeaders := make([]rpc.BlockHeader, epochLength)
	for i := uint64(0); i < epochLength; i++ {
		blockNumber := main.BlockNumber(i) + startingBlock

		// All block hashes are set to 0x1 except for the target block
		if blockNumber == targetBlock {
			blockHash = targetBlockHash.ToFelt()
			mockLogger.
				EXPECT().
				Infow("Target block reached", "block number", blockNumber.Uint64(), "block hash", &blockHash)
			mockLogger.
				EXPECT().
				Infof(
					"Will attest to target block in window [%d, %d]",
					targetBlock+main.BlockNumber(main.MIN_ATTESTATION_WINDOW),
					targetBlock+main.BlockNumber(attestWindow),
				)
		}

		blockHeaders[i] = rpc.BlockHeader{
			BlockNumber: blockNumber.Uint64(),
			BlockHash:   &blockHash,
		}
		mockLogger.EXPECT().Infow("Block header received", "blockHeader", &blockHeaders[i])
	}

	return blockHeaders
}

func TestSetTargetBlockHashIfExists(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)
	mockLogger := mocks.NewMockLogger(mockCtrl)

	t.Run("Target block does not already exist", func(t *testing.T) {
		targetBlockNumber := uint64(1)
		mockAccount.
			EXPECT().
			BlockWithTxHashes(context.Background(), rpc.BlockID{Number: &targetBlockNumber}).
			Return(nil, errors.New("Block not found"))

		attestInfo := main.AttestInfo{
			TargetBlock: main.BlockNumber(targetBlockNumber),
		}
		main.SetTargetBlockHashIfExists(mockAccount, mockLogger, &attestInfo)

		require.Equal(t, main.BlockHash{}, attestInfo.TargetBlockHash)
	})

	t.Run("Target block already exists but is pending", func(t *testing.T) {
		targetBlockNumber := uint64(1)
		mockAccount.
			EXPECT().
			BlockWithTxHashes(context.Background(), rpc.BlockID{Number: &targetBlockNumber}).
			Return(&rpc.PendingBlockTxHashes{}, nil)

		attestInfo := main.AttestInfo{
			TargetBlock: main.BlockNumber(targetBlockNumber),
		}
		main.SetTargetBlockHashIfExists(mockAccount, mockLogger, &attestInfo)

		require.Equal(t, main.BlockHash{}, attestInfo.TargetBlockHash)
	})

	t.Run("Target block already exists and is not pending", func(t *testing.T) {
		targetBlockHashFelt := utils.HexToFelt(t, "0x123")
		blockWithTxs := rpc.BlockTxHashes{
			BlockHeader: rpc.BlockHeader{
				BlockHash: targetBlockHashFelt,
			},
		}
		targetBlockNumber := uint64(1)
		mockAccount.
			EXPECT().
			BlockWithTxHashes(context.Background(), rpc.BlockID{Number: &targetBlockNumber}).
			Return(&blockWithTxs, nil)

		targetBlockHash := main.BlockHash(*targetBlockHashFelt)
		mockLogger.EXPECT().
			Infow(
				"Target block already exists, registered block hash to attest to it if still within attestation window",
				"block hash", targetBlockHash.String(),
			)

		attestInfo := main.AttestInfo{
			TargetBlock: main.BlockNumber(targetBlockNumber),
		}
		main.SetTargetBlockHashIfExists(mockAccount, mockLogger, &attestInfo)

		require.Equal(t, targetBlockHash, attestInfo.TargetBlockHash)
	})
}

func TestFetchEpochAndAttestInfoWithRetry(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)
	mockLogger := mocks.NewMockLogger(mockCtrl)

	t.Run("Return error fetching epoch info", func(t *testing.T) {
		// Mock fetchEpochInfo call
		validatorOperationalAddress := utils.HexToFelt(t, "0x123")
		mockAccount.EXPECT().Address().Return(validatorOperationalAddress).Times(main.DEFAULT_MAX_RETRIES + 1)

		expectedEpochInfoFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, main.STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("get_attestation_info_by_operational_address"),
			Calldata:           []*felt.Felt{validatorOperationalAddress},
		}

		fetchingError := "some internal error fetching epoch info"
		mockAccount.
			EXPECT().
			Call(context.Background(), expectedEpochInfoFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New(fetchingError)).
			Times(main.DEFAULT_MAX_RETRIES + 1)

		fetchedError := errors.Errorf("Error when calling entrypoint `get_attestation_info_by_operational_address`: %s", fetchingError)

		newEpochId := "123"
		mockLogger.EXPECT().Debugw("Failed to fetch epoch info", "epoch id", newEpochId, "error", fetchedError.Error()).Times(main.DEFAULT_MAX_RETRIES)
		for i := range main.DEFAULT_MAX_RETRIES {
			mockLogger.EXPECT().Debugw("Retrying to fetch epoch info...", "attempt", i+1)
		}

		main.Sleep = func(time.Duration) {
			// do nothing (avoid waiting)
		}
		defer func() { main.Sleep = time.Sleep }()

		newEpochInfo, newAttestInfo, err := main.FetchEpochAndAttestInfoWithRetry(mockAccount, mockLogger, nil, nil, newEpochId)

		require.Zero(t, newEpochInfo)
		require.Zero(t, newAttestInfo)
		expectedReturnedError := errors.Errorf("Failed to fetch epoch info for epoch id %s: %s", newEpochId, fetchedError.Error())
		require.Equal(t, expectedReturnedError.Error(), err.Error())
	})

	t.Run("Return epoch switch error (combine fetch info & epoch switch errors)", func(t *testing.T) {
		// Mock fetchEpochInfo call
		validatorOperationalAddress := utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		mockAccount.EXPECT().Address().Return(validatorOperationalAddress).Times(1)

		expectedEpochInfoFnCall := rpc.FunctionCall{
			ContractAddress:    utils.HexToFelt(t, main.STAKING_CONTRACT_ADDRESS),
			EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("get_attestation_info_by_operational_address"),
			Calldata:           []*felt.Felt{validatorOperationalAddress},
		}

		fetchingError := "some internal error fetching epoch info"
		mockAccount.
			EXPECT().
			Call(context.Background(), expectedEpochInfoFnCall, rpc.BlockID{Tag: "latest"}).
			Return(nil, errors.New(fetchingError)).
			Times(1)

		fetchedError := errors.Errorf("Error when calling entrypoint `get_attestation_info_by_operational_address`: %s", fetchingError)

		newEpochIdUint := uint64(1516)
		newEpochIdStr := strconv.FormatUint(newEpochIdUint, 10)
		mockLogger.EXPECT().Debugw("Failed to fetch epoch info", "epoch id", newEpochIdStr, "error", fetchedError.Error()).Times(1)

		// fetchEpochInfo now works but returns a wrong next epoch
		stake := uint64(1000000000000000000)
		epochLength := uint64(40)
		newEpochStartingBlock := main.BlockNumber(639270)
		attestWindow := uint64(16)
		expectedTargetBlock := main.BlockNumber(639291)

		mockFetchedEpochAndAttestInfo(
			t,
			mockAccount,
			mockLogger,
			newEpochIdUint,
			epochLength,
			attestWindow,
			newEpochStartingBlock,
			expectedTargetBlock,
			main.DEFAULT_MAX_RETRIES,
		)

		prevEpoch := main.EpochInfo{
			StakerAddress:             main.Address(*validatorOperationalAddress),
			Stake:                     uint128.New(stake, 0),
			EpochLen:                  epochLength,
			EpochId:                   newEpochIdUint - 1,
			CurrentEpochStartingBlock: newEpochStartingBlock - main.BlockNumber(epochLength) + 1, // wrong new epoch start (shouldn't have + 1)
		}

		newEpoch := main.EpochInfo{
			StakerAddress:             main.Address(*validatorOperationalAddress),
			Stake:                     uint128.New(stake, 0),
			EpochLen:                  epochLength,
			EpochId:                   newEpochIdUint,
			CurrentEpochStartingBlock: newEpochStartingBlock,
		}

		mockLogger.EXPECT().Debugw("Wrong epoch switch", "from epoch", &prevEpoch, "to epoch", &newEpoch).Times(main.DEFAULT_MAX_RETRIES - 1)

		for i := range main.DEFAULT_MAX_RETRIES {
			mockLogger.EXPECT().Debugw("Retrying to fetch epoch info...", "attempt", i+1)
		}

		main.Sleep = func(time.Duration) {
			// do nothing (avoid waiting)
		}
		defer func() { main.Sleep = time.Sleep }()

		newEpochInfo, newAttestInfo, err := main.FetchEpochAndAttestInfoWithRetry(
			mockAccount,
			mockLogger,
			&prevEpoch,
			main.IsEpochSwitchCorrect,
			newEpochIdStr,
		)

		fmt.Println("--- returned error: ", err.Error())

		require.Zero(t, newEpochInfo)
		require.Zero(t, newAttestInfo)
		expectedReturnedError := errors.Errorf("Wrong epoch switch: from epoch %s to epoch %s", &prevEpoch, &newEpoch)
		require.Equal(t, expectedReturnedError.Error(), err.Error())
	})
}
