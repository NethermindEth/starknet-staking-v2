package validator_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	main "github.com/NethermindEth/starknet-staking-v2/validator"
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
	logger := utils.NewNopZapLogger()

	t.Run("Simple scenario: 1 epoch", func(t *testing.T) {
		dispatcher := validator.NewEventDispatcher[*mocks.MockAccounter, *utils.ZapLogger]()
		headersFeed := make(chan *rpc.BlockHeader)

		attestWindow := uint64(16)
		epoch := validator.EpochInfo{
			StakerAddress:             main.AddressFromString("0x11efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e"),
			Stake:                     uint128.New(1000000000000000000, 0),
			EpochId:                   1516,
			CurrentEpochStartingBlock: 639270,
			EpochLen:                  40,
		}
		expectedTargetBlock := validator.BlockNumber(639291)
		mockSuccessfullyFetchedEpochAndAttestInfo(
			t,
			mockAccount,
			&epoch,
			attestWindow,
			1,
		)

		targetBlockHash := validator.BlockHash(*utils.HexToFelt(t, "0x6d8dc0a8bdf98854b6bc146cb7cab6cddda85619c6ae2948ee65da25815e045"))
		blockHeaders := mockHeaderFeed(t, epoch.CurrentEpochStartingBlock, expectedTargetBlock, &targetBlockHash, epoch.EpochLen)

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
		receivedAttestEvents := make(map[validator.AttestRequired]uint)
		receivedEndOfWindowEvents := uint8(0)
		wgDispatcher := conc.NewWaitGroup()
		wgDispatcher.Go(func() { registerReceivedEvents(t, &dispatcher, receivedAttestEvents, &receivedEndOfWindowEvents) })

		validator.ProcessBlockHeaders(headersFeed, mockAccount, logger, &dispatcher)

		// No need to wait for wgFeed routine as it'll be the 1st closed, causing ProcessBlockHeaders to have returned
		// Still calling it just in case.
		wgFeed.Wait()

		// Will terminate the registerReceivedEvents routine
		close(dispatcher.AttestRequired)
		wgDispatcher.Wait()

		// Assert
		require.Equal(t, 1, len(receivedAttestEvents))

		actualCount, exists := receivedAttestEvents[validator.AttestRequired{BlockHash: targetBlockHash}]
		require.True(t, exists)
		require.Equal(t, uint(attestWindow-validator.MIN_ATTESTATION_WINDOW+1), actualCount)

		require.Equal(t, uint8(1), receivedEndOfWindowEvents)
	})

	t.Run("Scenario: transition between 2 epochs", func(t *testing.T) {
		dispatcher := validator.NewEventDispatcher[*mocks.MockAccounter, *utils.ZapLogger]()
		headersFeed := make(chan *rpc.BlockHeader)

		stakerAddress := main.AddressFromString("0x11efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		stake := uint128.New(1000000000000000000, 0)
		epochLength := uint64(40)
		attestWindow := uint64(16)

		epoch1 := validator.EpochInfo{
			StakerAddress:             stakerAddress,
			Stake:                     stake,
			EpochId:                   1516,
			CurrentEpochStartingBlock: 639270,
			EpochLen:                  epochLength,
		}
		expectedTargetBlock1 := validator.BlockNumber(639291) // calculated by fetch epoch & attest info call
		mockSuccessfullyFetchedEpochAndAttestInfo(t, mockAccount, &epoch1, attestWindow, 1)

		epoch2 := validator.EpochInfo{
			StakerAddress:             stakerAddress,
			Stake:                     stake,
			EpochId:                   1517,
			CurrentEpochStartingBlock: 639310,
			EpochLen:                  epochLength,
		}
		expectedTargetBlock2 := validator.BlockNumber(639316) // calculated by fetch epoch & attest info call
		mockSuccessfullyFetchedEpochAndAttestInfo(t, mockAccount, &epoch2, attestWindow, 1)

		targetBlockHashEpoch1 := validator.BlockHash(
			*utils.HexToFelt(t, "0x6d8dc0a8bdf98854b6bc146cb7cab6cddda85619c6ae2948ee65da25815e045"),
		)
		blockHeaders1 := mockHeaderFeed(
			t, epoch1.CurrentEpochStartingBlock, expectedTargetBlock1, &targetBlockHashEpoch1, epoch1.EpochLen,
		)

		targetBlockHashEpoch2 := validator.BlockHash(
			*utils.HexToFelt(t, "0x2124ae375432a16ef644f539c3b148f63c706067bf576088f32033fe59c345e"),
		)
		blockHeaders2 := mockHeaderFeed(
			t,
			epoch2.CurrentEpochStartingBlock,
			expectedTargetBlock2,
			&targetBlockHashEpoch2,
			epoch2.EpochLen,
		)

		// Mock SetTargetBlockHashIfExists call
		targetBlockUint64 := expectedTargetBlock1.Uint64()
		mockAccount.
			EXPECT().
			BlockWithTxHashes(context.Background(), rpc.BlockID{Number: &targetBlockUint64}).
			Return(nil, errors.New("Block not found")) // Let's say block does not exist yet

		// Headers feeder routine
		wgFeed := conc.NewWaitGroup()
		wgFeed.Go(func() {
			sendHeaders(t, headersFeed, blockHeaders1)
			sendHeaders(t, headersFeed, blockHeaders2)
			close(headersFeed) // close channel once headers are sent to terminate ProcessBlockHeaders
		})

		// Events receiver routine
		receivedAttestEvents := make(map[validator.AttestRequired]uint)
		receivedEndOfWindowEvents := uint8(0)
		wgDispatcher := conc.NewWaitGroup()
		wgDispatcher.Go(func() { registerReceivedEvents(t, &dispatcher, receivedAttestEvents, &receivedEndOfWindowEvents) })

		validator.ProcessBlockHeaders(headersFeed, mockAccount, logger, &dispatcher)

		// No need to wait for wgFeed routine as it'll be the 1st closed, causing ProcessBlockHeaders to have returned
		// Still calling it just in case.
		wgFeed.Wait()

		// Will terminate the registerReceivedEvents routine
		close(dispatcher.AttestRequired)
		wgDispatcher.Wait()

		// Assert
		require.Equal(t, 2, len(receivedAttestEvents))

		countEpoch1, exists := receivedAttestEvents[validator.AttestRequired{BlockHash: targetBlockHashEpoch1}]
		require.True(t, exists)
		require.Equal(t, uint(16-validator.MIN_ATTESTATION_WINDOW+1), countEpoch1)

		countEpoch2, exists := receivedAttestEvents[validator.AttestRequired{BlockHash: targetBlockHashEpoch2}]
		require.True(t, exists)
		require.Equal(t, uint(16-validator.MIN_ATTESTATION_WINDOW+1), countEpoch2)

		require.Equal(t, uint8(2), receivedEndOfWindowEvents)
	})

	t.Run("Scenario: error transitioning between 2 epochs (wrong epoch switch)", func(t *testing.T) {
		dispatcher := validator.NewEventDispatcher[*mocks.MockAccounter, *utils.ZapLogger]()
		headersFeed := make(chan *rpc.BlockHeader)

		stakerAddress := main.AddressFromString("0x11efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		stake := uint128.New(1000000000000000000, 0)
		epochLength := uint64(40)
		attestWindow := uint64(16)

		epoch1 := validator.EpochInfo{
			StakerAddress:             stakerAddress,
			Stake:                     stake,
			EpochId:                   1516,
			CurrentEpochStartingBlock: 639270,
			EpochLen:                  epochLength,
		}
		expectedTargetBlock1 := validator.BlockNumber(639291) // calculated by fetch epoch & attest info call
		mockSuccessfullyFetchedEpochAndAttestInfo(t, mockAccount, &epoch1, attestWindow, 1)

		epoch2 := validator.EpochInfo{
			StakerAddress:             stakerAddress,
			Stake:                     stake,
			EpochId:                   1517,
			CurrentEpochStartingBlock: 639311, // Wrong new epoch start (1 block after expected one)
			EpochLen:                  epochLength,
		}
		expectedTargetBlock2 := validator.BlockNumber(639316) // calculated by fetch epoch & attest info call
		// The call to fetch next epoch's info will return an erroneous starting block
		mockSuccessfullyFetchedEpochAndAttestInfo(t, mockAccount, &epoch2, attestWindow, validator.DEFAULT_MAX_RETRIES+1)

		targetBlockHashEpoch1 := validator.BlockHash(*utils.HexToFelt(t, "0x6d8dc0a8bdf98854b6bc146cb7cab6cddda85619c6ae2948ee65da25815e045"))
		blockHeaders1 := mockHeaderFeed(t, epoch1.CurrentEpochStartingBlock, expectedTargetBlock1, &targetBlockHashEpoch1, epoch1.EpochLen)

		targetBlockHashEpoch2 := validator.BlockHash(*utils.HexToFelt(t, "0x2124ae375432a16ef644f539c3b148f63c706067bf576088f32033fe59c345e"))
		// Have the feeder routine feed the next epoch's correct starting block
		blockHeaders2 := mockHeaderFeed(t, epoch2.CurrentEpochStartingBlock-1, expectedTargetBlock2, &targetBlockHashEpoch2, epoch2.EpochLen)

		// Mock SetTargetBlockHashIfExists call
		targetBlockUint64 := expectedTargetBlock1.Uint64()
		mockAccount.
			EXPECT().
			BlockWithTxHashes(context.Background(), rpc.BlockID{Number: &targetBlockUint64}).
			Return(nil, errors.New("Block not found")) // Let's say block does not exist yet

		// Headers feeder routine
		wgFeed := conc.NewWaitGroup()
		wgFeed.Go(func() {
			sendHeaders(t, headersFeed, blockHeaders1)
			sendHeaders(t, headersFeed, blockHeaders2)
			close(headersFeed) // Will never get closed
		})

		// Events receiver routine
		receivedAttestEvents := make(map[validator.AttestRequired]uint)
		receivedEndOfWindowEvents := uint8(0)
		wgDispatcher := conc.NewWaitGroup()
		wgDispatcher.Go(func() { registerReceivedEvents(t, &dispatcher, receivedAttestEvents, &receivedEndOfWindowEvents) })

		validator.Sleep = func(time.Duration) {
			// do nothing (avoid waiting)
		}
		defer func() { validator.Sleep = time.Sleep }()

		err := validator.ProcessBlockHeaders(headersFeed, mockAccount, logger, &dispatcher)

		// wgFeed is trying to send the 2nd epoch's blocks and is now stuck there because
		// ProcessBlockHeaders already returned as the epoch switch failed as the new epoch's starting block was not correct
		close(headersFeed)
		// Close the channel to terminate the routine (it will panic trying to send a msg to the now closed channel)
		panicRecovered := wgFeed.WaitAndRecover()
		require.Contains(t, panicRecovered.Value, "send on closed channel")

		// Will terminate the registerReceivedEvents routine
		close(dispatcher.AttestRequired)
		wgDispatcher.Wait()

		// Assert
		require.Equal(t, 1, len(receivedAttestEvents))

		countEpoch1, exists := receivedAttestEvents[validator.AttestRequired{BlockHash: targetBlockHashEpoch1}]
		require.True(t, exists)
		require.Equal(t, uint(attestWindow-validator.MIN_ATTESTATION_WINDOW+1), countEpoch1)

		require.Equal(t, uint8(1), receivedEndOfWindowEvents)

		expectedReturnedError := errors.Errorf("Wrong epoch switch: from epoch %s to epoch %s", &epoch1, &epoch2)
		require.Equal(t, expectedReturnedError.Error(), err.Error())
	})
}

// TODO: Add test with error when calling FetchEpochAndAttestInfo <-- for whole Attest test

// Test helper function to send headers
func sendHeaders(t *testing.T, headersFeed chan *rpc.BlockHeader, blockHeaders []rpc.BlockHeader) {
	t.Helper()

	for i := range blockHeaders {
		headersFeed <- &blockHeaders[i]
	}
}

// Test helper function to register received events to assert on them
// Note: to exit this function, close the AttestRequired channel
func registerReceivedEvents[T validator.Accounter, Log validator.Logger](
	t *testing.T,
	dispatcher *validator.EventDispatcher[T, Log],
	receivedAttestRequired map[validator.AttestRequired]uint,
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
func mockSuccessfullyFetchedEpochAndAttestInfo(
	t *testing.T,
	mockAccount *mocks.MockAccounter,
	epoch *validator.EpochInfo,
	attestWindow uint64,
	howManyTimes int,
) {
	t.Helper()

	// Mock fetchEpochInfo call
	validatorOperationalAddress := utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
	mockAccount.EXPECT().Address().Return(validatorOperationalAddress).Times(howManyTimes)

	stake := uint64(1000000000000000000)

	expectedEpochInfoFnCall := rpc.FunctionCall{
		ContractAddress: utils.HexToFelt(t, validator.STAKING_CONTRACT_ADDRESS),
		EntryPointSelector: snGoUtils.GetSelectorFromNameFelt(
			"get_attestation_info_by_operational_address",
		),
		Calldata: []*felt.Felt{validatorOperationalAddress},
	}

	mockAccount.
		EXPECT().
		Call(context.Background(), expectedEpochInfoFnCall, rpc.BlockID{Tag: "latest"}).
		Return(
			[]*felt.Felt{
				validatorOperationalAddress,
				new(felt.Felt).SetUint64(stake),
				new(felt.Felt).SetUint64(epoch.EpochLen),
				new(felt.Felt).SetUint64(epoch.EpochId),
				new(felt.Felt).SetUint64(epoch.CurrentEpochStartingBlock.Uint64()),
			},
			nil,
		).
		Times(howManyTimes)

	// Mock fetchAttestWindow call
	expectedWindowFnCall := rpc.FunctionCall{
		ContractAddress:    utils.HexToFelt(t, validator.ATTEST_CONTRACT_ADDRESS),
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
}

func mockFailedFetchingEpochAndAttestInfo(
	t *testing.T,
	mockAccount *mocks.MockAccounter,
	operationalAddress *felt.Felt,
	fetchingError string,
	howManyTimes int,
) {
	t.Helper()

	// Mock fetchEpochInfo call
	mockAccount.EXPECT().Address().Return(operationalAddress).Times(howManyTimes)

	expectedEpochInfoFnCall := rpc.FunctionCall{
		ContractAddress:    utils.HexToFelt(t, validator.STAKING_CONTRACT_ADDRESS),
		EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("get_attestation_info_by_operational_address"),
		Calldata:           []*felt.Felt{operationalAddress},
	}

	mockAccount.
		EXPECT().
		Call(context.Background(), expectedEpochInfoFnCall, rpc.BlockID{Tag: "latest"}).
		Return(nil, errors.New(fetchingError)).
		Times(howManyTimes)
}

func mockHeaderFeed(
	t *testing.T,
	startingBlock,
	targetBlock validator.BlockNumber,
	targetBlockHash *validator.BlockHash,
	epochLength uint64,
) []rpc.BlockHeader {
	t.Helper()

	blockHeaders := make([]rpc.BlockHeader, epochLength)
	for i := uint64(0); i < epochLength; i++ {
		blockNumber := validator.BlockNumber(i) + startingBlock

		// All block hashes are set to 0x1 except for the target block
		blockHash := new(felt.Felt).SetUint64(1)
		if blockNumber == targetBlock {
			blockHash = targetBlockHash.Felt()
		}

		blockHeaders[i] = rpc.BlockHeader{
			BlockNumber: blockNumber.Uint64(),
			BlockHash:   blockHash,
		}
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

		attestInfo := validator.AttestInfo{
			TargetBlock: validator.BlockNumber(targetBlockNumber),
		}
		validator.SetTargetBlockHashIfExists(mockAccount, mockLogger, &attestInfo)

		require.Equal(t, validator.BlockHash{}, attestInfo.TargetBlockHash)
	})

	t.Run("Target block already exists but is pending", func(t *testing.T) {
		targetBlockNumber := uint64(1)
		mockAccount.
			EXPECT().
			BlockWithTxHashes(context.Background(), rpc.BlockID{Number: &targetBlockNumber}).
			Return(&rpc.PendingBlockTxHashes{}, nil)

		attestInfo := validator.AttestInfo{
			TargetBlock: validator.BlockNumber(targetBlockNumber),
		}
		validator.SetTargetBlockHashIfExists(mockAccount, mockLogger, &attestInfo)

		require.Equal(t, validator.BlockHash{}, attestInfo.TargetBlockHash)
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

		targetBlockHash := validator.BlockHash(*targetBlockHashFelt)
		mockLogger.EXPECT().
			Infow(
				"Target block already exists, registered block hash to attest to it if still within attestation window",
				"block hash", targetBlockHash.String(),
			)

		attestInfo := validator.AttestInfo{
			TargetBlock: validator.BlockNumber(targetBlockNumber),
		}
		validator.SetTargetBlockHashIfExists(mockAccount, mockLogger, &attestInfo)

		require.Equal(t, targetBlockHash, attestInfo.TargetBlockHash)
	})
}

func TestFetchEpochAndAttestInfoWithRetry(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)
	noOpLogger := utils.NewNopZapLogger()

	t.Run("Return error fetching epoch info", func(t *testing.T) {
		// Sequence of actions:
		// 1. Fetch epoch and attest info: error, causing to retry 10 times
		// 2. After the 10 retries, exit with error

		validatorOperationalAddress := utils.HexToFelt(t, "0x123")
		fetchingError := "some internal error fetching epoch info"
		mockFailedFetchingEpochAndAttestInfo(t, mockAccount, validatorOperationalAddress, fetchingError, validator.DEFAULT_MAX_RETRIES+1)

		fetchedError := errors.Errorf("Error when calling entrypoint `get_attestation_info_by_operational_address`: %s", fetchingError)

		newEpochId := "123"

		validator.Sleep = func(time.Duration) {
			// do nothing (avoid waiting)
		}
		defer func() { validator.Sleep = time.Sleep }()

		newEpochInfo, newAttestInfo, err := validator.FetchEpochAndAttestInfoWithRetry(mockAccount, noOpLogger, nil, nil, newEpochId)

		require.Zero(t, newEpochInfo)
		require.Zero(t, newAttestInfo)
		expectedReturnedError := errors.Errorf("Failed to fetch epoch info for epoch id %s: %s", newEpochId, fetchedError.Error())
		require.Equal(t, expectedReturnedError.Error(), err.Error())
	})

	t.Run("Return epoch switch error (combine fetch info & epoch switch errors)", func(t *testing.T) {
		// Sequence of actions:
		// 1. Fetch epoch and attest info: error
		// 2. Epoch switch: error, causing to retry 10 times
		// 3. After the 10 retries, exit with error

		validatorOperationalAddress := utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		fetchingError := "some internal error fetching epoch info"
		mockFailedFetchingEpochAndAttestInfo(t, mockAccount, validatorOperationalAddress, fetchingError, 1)

		stakerAddress := validator.Address(*validatorOperationalAddress)
		stake := uint64(1000000000000000000)
		epochLength := uint64(40)

		epoch1 := validator.EpochInfo{
			StakerAddress:             stakerAddress,
			Stake:                     uint128.New(stake, 0),
			EpochLen:                  epochLength,
			EpochId:                   1515,
			CurrentEpochStartingBlock: 639230,
		}

		// Mock FetchEpochAndAttestInfo: returns a wrong next epoch (10 times)
		epoch2 := validator.EpochInfo{
			StakerAddress:             stakerAddress,
			Stake:                     uint128.New(stake, 0),
			EpochLen:                  epochLength,
			EpochId:                   1516,
			CurrentEpochStartingBlock: 639271, // wrong new epoch start (1 block after correct block)
		}
		attestWindow := uint64(16)
		mockSuccessfullyFetchedEpochAndAttestInfo(
			t,
			mockAccount,
			&epoch2,
			attestWindow,
			validator.DEFAULT_MAX_RETRIES,
		)

		validator.Sleep = func(time.Duration) {
			// do nothing (avoid waiting)
		}
		defer func() { validator.Sleep = time.Sleep }()

		newEpochInfo, newAttestInfo, err := validator.FetchEpochAndAttestInfoWithRetry(
			mockAccount,
			noOpLogger,
			&epoch1,
			validator.IsEpochSwitchCorrect,
			strconv.FormatUint(epoch1.EpochId+1, 10),
		)

		require.Zero(t, newEpochInfo)
		require.Zero(t, newAttestInfo)
		expectedReturnedError := errors.Errorf("Wrong epoch switch: from epoch %s to epoch %s", &epoch1, &epoch2)
		require.Equal(t, expectedReturnedError.Error(), err.Error())
	})

	t.Run("Successfully return epoch and attest info", func(t *testing.T) {
		// Sequence of actions:
		// 1. Fetch epoch and attest info: error (causing retry)
		// 2. Fetch epoch and attest info: successful
		// 3. Epoch switch: successful
		// 4. Return new epoch and attest info

		// Mock 1st call to FetchEpochAndAttestInfo: fails
		validatorOperationalAddress := utils.HexToFelt(t, "0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e")
		fetchingError := "some internal error fetching epoch info"
		mockFailedFetchingEpochAndAttestInfo(t, mockAccount, validatorOperationalAddress, fetchingError, 1)

		// fetchEpochInfo now works and returns a correct next epoch
		stakerAddress := validator.Address(*validatorOperationalAddress)
		stake := uint64(1000000000000000000)
		epochLength := uint64(40)

		epoch1 := validator.EpochInfo{
			StakerAddress:             stakerAddress,
			Stake:                     uint128.New(stake, 0),
			EpochLen:                  epochLength,
			EpochId:                   1515,
			CurrentEpochStartingBlock: 639230,
		}

		// Mock 2nd FetchEpochAndAttestInfo: returns a correct next epoch
		epoch2 := validator.EpochInfo{
			StakerAddress:             stakerAddress,
			Stake:                     uint128.New(stake, 0),
			EpochLen:                  epochLength,
			EpochId:                   1516,
			CurrentEpochStartingBlock: 639270,
		}
		attestWindow := uint64(16)
		mockSuccessfullyFetchedEpochAndAttestInfo(t, mockAccount, &epoch2, attestWindow, 1)

		validator.Sleep = func(time.Duration) {
			// do nothing (avoid waiting)
		}
		defer func() { validator.Sleep = time.Sleep }()

		newEpochInfo, newAttestInfo, err := validator.FetchEpochAndAttestInfoWithRetry(
			mockAccount,
			noOpLogger,
			&epoch1,
			validator.IsEpochSwitchCorrect,
			strconv.FormatUint(epoch1.EpochId+1, 10),
		)

		expectedEpoch2TargetBlock := validator.BlockNumber(639291)
		expectedEpoch2AttestInfo := validator.AttestInfo{
			TargetBlock:     expectedEpoch2TargetBlock,
			TargetBlockHash: validator.BlockHash{},
			WindowStart:     expectedEpoch2TargetBlock + validator.BlockNumber(validator.MIN_ATTESTATION_WINDOW),
			WindowEnd:       expectedEpoch2TargetBlock + validator.BlockNumber(attestWindow),
		}

		require.Equal(t, &epoch2, &newEpochInfo)
		require.Equal(t, expectedEpoch2AttestInfo, newAttestInfo)
		require.Nil(t, err)
	})
}
