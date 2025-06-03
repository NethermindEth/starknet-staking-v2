package validator_test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/cockroachdb/errors"
	"github.com/sourcegraph/conc"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/net/context"
	"lukechampine.com/uint128"
)

func TestAttest(t *testing.T) {
	sepoliaConfig := new(config.StarknetConfig).SetDefaults("sepolia")

	// This test is based in the old mechanism where `FetchEpochAndAttestInfoWithRetry` was
	// meant to fail a limited amount of times. Since it is now tries through the infinite loop
	// this tests hangs
	t.Run("Successful set up (internal signer)", func(t *testing.T) {
		env, err := validator.LoadEnv(t)
		if err != nil {
			t.Skipf("Ignoring test due to env variables loading failed: %s", err)
		}

		operationalAddress := utils.HexToFelt(t, "0x123")
		serverInternalError := "Some internal server error when fetching epoch and attest info" +
			"(internal signer test)"

		mockRPC := validator.MockRPCServer(t, operationalAddress, serverInternalError)
		defer mockRPC.Close()

		config := &config.Config{
			Provider: config.Provider{
				Http: mockRPC.URL,
				Ws:   env.WsProviderUrl,
			},
			Signer: config.Signer{
				OperationalAddress: operationalAddress.String(),
				ExternalURL:        "http://localhost:5678",
			},
		}

		validator.Sleep = func(d time.Duration) {}
		defer func() { validator.Sleep = time.Sleep }()

		logger := utils.NewNopZapLogger()
		ctx := context.Background()
		metricsServer := mockMetricsServer()

		v, err := validator.New(config, sepoliaConfig, *logger, false)
		require.NoError(t, err)
		err = v.Attest(ctx, defaultRetries(t), metricsServer)

		expectedErrorMsg := fmt.Sprintf(
			"Error when calling entrypoint `get_attestation_info_by_operational_address`: -32603 The error is not a valid RPC error: %d Internal Server Error: %s",
			http.StatusInternalServerError,
			serverInternalError,
		)

		require.ErrorContains(t, err, expectedErrorMsg)
	})

	t.Run("Successful set up (external signer)", func(t *testing.T) {
		env, err := validator.LoadEnv(t)
		if err != nil {
			t.Skipf("Ignoring test due to env variables loading failed: %s", err)
		}

		operationalAddress := utils.HexToFelt(t, "0x456")
		serverInternalError := "Some internal server error when fetching epoch and attest info (external signer test)"

		mockRpc := validator.MockRPCServer(t, operationalAddress, serverInternalError)
		defer mockRpc.Close()

		config := &config.Config{
			Provider: config.Provider{
				Http: mockRpc.URL,
				Ws:   env.WsProviderUrl,
			},
			Signer: config.Signer{
				OperationalAddress: operationalAddress.String(),
				PrivKey:            "0x123",
			},
		}

		validator.Sleep = func(d time.Duration) {
			// No need to wait
		}
		defer func() { validator.Sleep = time.Sleep }()

		logger := utils.NewNopZapLogger()
		ctx := context.Background()
		metricsServer := mockMetricsServer()

		v, err := validator.New(config, sepoliaConfig, *logger, false)
		require.NoError(t, err)
		err = v.Attest(ctx, defaultRetries(t), metricsServer)

		expectedErrorMsg := fmt.Sprintf(
			"Error when calling entrypoint `get_attestation_info_by_operational_address`: -32603 The error is not a valid RPC error: %d Internal Server Error: %s",
			http.StatusInternalServerError,
			serverInternalError,
		)

		require.ErrorContains(t, err, expectedErrorMsg)
	})
}

func TestProcessBlockHeaders(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockSigner := mocks.NewMockSigner(mockCtrl)
	mockSigner.EXPECT().ValidationContracts().Return(
		validator.SepoliaValidationContracts(t),
	).AnyTimes()

	logger := utils.NewNopZapLogger()

	t.Run("Simple scenario: 1 epoch", func(t *testing.T) {
		dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
		headersFeed := make(chan *rpc.BlockHeader)

		epoch := types.EpochInfo{
			StakerAddress: types.AddressFromString("0x123"),
			Stake:         uint128.New(1000000000000000000, 0),
			EpochId:       1516,
			StartingBlock: 639270,
			EpochLen:      40,
		}
		attestWindow := uint64(16)
		mockSuccessfullyFetchedEpochAndAttestInfo(
			t,
			mockSigner,
			&epoch,
			attestWindow,
			1,
		)

		expectedTargetBlock := types.BlockNumber(639276)
		targetBlockHash := types.BlockHash(
			*utils.HexToFelt(
				t, "0x6d8dc0a8bdf98854b6bc146cb7cab6cddda85619c6ae2948ee65da25815e045",
			),
		)
		blockHeaders := mockHeaderFeed(
			t,
			epoch.StartingBlock,
			expectedTargetBlock,
			&targetBlockHash,
			epoch.EpochLen,
		)

		// Mock SetTargetBlockHashIfExists call
		targetBlockUint64 := expectedTargetBlock.Uint64()
		mockSigner.
			EXPECT().
			BlockWithTxHashes(rpc.BlockID{Number: &targetBlockUint64}).
			Return(nil, errors.New("Block not found")) // Let's say block does not exist yet

		// Headers feeder routine
		wgFeed := conc.NewWaitGroup()
		wgFeed.Go(func() {
			sendHeaders(t, headersFeed, blockHeaders)
			close(headersFeed) // close channel once headers are sent to terminate ProcessBlockHeaders
		})

		// Events receiver routine
		receivedAttestEvents := make(map[types.DoAttest]uint)
		receivedEndOfWindowEvents := uint8(0)
		wgDispatcher := conc.NewWaitGroup()
		wgDispatcher.Go(
			func() {
				registerReceivedEvents(
					t, &dispatcher, receivedAttestEvents, &receivedEndOfWindowEvents,
				)
			},
		)

		metricsServer := mockMetricsServer()
		err := validator.ProcessBlockHeaders(
			headersFeed, mockSigner, logger, &dispatcher, defaultRetries(t), metricsServer,
		)
		require.NoError(t, err)

		// No need to wait for wgFeed routine as it'll be the 1st closed,
		// causing ProcessBlockHeaders to have returned. Still calling it just in case.
		wgFeed.Wait()

		// Will terminate the registerReceivedEvents routine
		close(dispatcher.DoAttest)
		wgDispatcher.Wait()

		// Assert
		require.Equal(t, 1, len(receivedAttestEvents))

		actualCount, exists := receivedAttestEvents[types.DoAttest{
			// BlockHash: targetBlockHash,
		}]
		require.True(t, exists)
		require.Equal(t, uint(attestWindow-constants.MIN_ATTESTATION_WINDOW+1), actualCount)

		require.Equal(t, uint8(1), receivedEndOfWindowEvents)
	})

	t.Run("Scenario: transition between 2 epochs", func(t *testing.T) {
		dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
		headersFeed := make(chan *rpc.BlockHeader)

		stakerAddress := types.AddressFromString("0x123")
		stake := uint128.New(1000000000000000000, 0)
		epochLength := uint64(40)
		attestWindow := uint64(16)

		epoch1 := types.EpochInfo{
			StakerAddress: stakerAddress,
			Stake:         stake,
			EpochId:       1516,
			StartingBlock: 639270,
			EpochLen:      epochLength,
		}
		// calculated by fetch epoch & attest info call
		expectedTargetBlock1 := types.BlockNumber(639276)
		mockSuccessfullyFetchedEpochAndAttestInfo(t, mockSigner, &epoch1, attestWindow, 1)

		epoch2 := types.EpochInfo{
			StakerAddress: stakerAddress,
			Stake:         stake,
			EpochId:       1517,
			StartingBlock: 639310,
			EpochLen:      epochLength,
		}
		// calculated by fetch epoch & attest info call
		expectedTargetBlock2 := types.BlockNumber(639315)
		mockSuccessfullyFetchedEpochAndAttestInfo(t, mockSigner, &epoch2, attestWindow, 1)

		targetBlockHashEpoch1 := types.BlockHash(
			*utils.HexToFelt(t, "0x6d8dc0a8bdf98854b6bc146cb7cab6cddda85619c6ae2948ee65da25815e045"),
		)
		blockHeaders1 := mockHeaderFeed(
			t,
			epoch1.StartingBlock,
			expectedTargetBlock1,
			&targetBlockHashEpoch1,
			epoch1.EpochLen,
		)

		targetBlockHashEpoch2 := types.BlockHash(
			*utils.HexToFelt(t, "0x2124ae375432a16ef644f539c3b148f63c706067bf576088f32033fe59c345e"),
		)
		blockHeaders2 := mockHeaderFeed(
			t,
			epoch2.StartingBlock,
			expectedTargetBlock2,
			&targetBlockHashEpoch2,
			epoch2.EpochLen,
		)

		// Mock SetTargetBlockHashIfExists call
		targetBlockUint64 := expectedTargetBlock1.Uint64()
		mockSigner.
			EXPECT().
			BlockWithTxHashes(rpc.BlockID{Number: &targetBlockUint64}).
			Return(nil, errors.New("Block not found")) // Let's say block does not exist yet

		// Headers feeder routine
		wgFeed := conc.NewWaitGroup()
		wgFeed.Go(func() {
			sendHeaders(t, headersFeed, blockHeaders1)
			sendHeaders(t, headersFeed, blockHeaders2)
			close(headersFeed) // close channel once headers are sent to terminate ProcessBlockHeaders
		})

		// Events receiver routine
		receivedAttestEvents := make(map[types.DoAttest]uint)
		receivedEndOfWindowEvents := uint8(0)
		wgDispatcher := conc.NewWaitGroup()
		wgDispatcher.Go(
			func() {
				registerReceivedEvents(
					t, &dispatcher, receivedAttestEvents, &receivedEndOfWindowEvents,
				)
			},
		)
		metricsServer := mockMetricsServer()
		err := validator.ProcessBlockHeaders(
			headersFeed,
			mockSigner,
			logger,
			&dispatcher,
			defaultRetries(t),
			metricsServer,
		)
		require.NoError(t, err)

		// No need to wait for wgFeed routine as it'll be the 1st closed, causing ProcessBlockHeaders to have returned
		// Still calling it just in case.
		wgFeed.Wait()

		// Will terminate the registerReceivedEvents routine
		close(dispatcher.DoAttest)
		wgDispatcher.Wait()

		// Assert
		require.Len(t, receivedAttestEvents, 2)

		countEpoch1, exists := receivedAttestEvents[types.DoAttest{BlockHash: &targetBlockHashEpoch1}]
		require.True(t, exists)
		require.Equal(t, uint(16-constants.MIN_ATTESTATION_WINDOW+1), countEpoch1)

		countEpoch2, exists := receivedAttestEvents[types.DoAttest{BlockHash: &targetBlockHashEpoch2}]
		require.True(t, exists)
		require.Equal(t, uint(16-constants.MIN_ATTESTATION_WINDOW+1), countEpoch2)

		require.Equal(t, uint8(2), receivedEndOfWindowEvents)
	})

	// This one doesn't work with infinity tries. This test is broken somewhere. Ignoring for now
	t.Run(
		"Scenario: error transitioning between 2 epochs (wrong epoch switch)",
		func(t *testing.T) {
			dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
			headersFeed := make(chan *rpc.BlockHeader)

			stakerAddress := types.AddressFromString(
				"0x11efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e",
			)
			stake := uint128.New(1000000000000000000, 0)
			epochLength := uint64(40)
			attestWindow := uint64(16)

			epoch1 := types.EpochInfo{
				StakerAddress: stakerAddress,
				Stake:         stake,
				EpochId:       1516,
				StartingBlock: 639270,
				EpochLen:      epochLength,
			}
			// calculated by fetch epoch & attest info call
			expectedTargetBlock1 := types.BlockNumber(639291)
			mockSuccessfullyFetchedEpochAndAttestInfo(t, mockSigner, &epoch1, attestWindow, 1)

			epoch2 := types.EpochInfo{
				StakerAddress: stakerAddress,
				Stake:         stake,
				EpochId:       1517,
				StartingBlock: 639311, // Wrong new epoch start (1 block after expected one)
				EpochLen:      epochLength,
			}
			// calculated by fetch epoch & attest info call
			expectedTargetBlock2 := types.BlockNumber(639316)
			// The call to fetch next epoch's info will return an erroneous starting block
			mockSuccessfullyFetchedEpochAndAttestInfo(
				t, mockSigner, &epoch2, attestWindow, constants.DEFAULT_MAX_RETRIES+1,
			)

			targetBlockHashEpoch1 := (*types.BlockHash)(
				utils.HexToFelt(
					t,
					"0x6d8dc0a8bdf98854b6bc146cb7cab6cddda85619c6ae2948ee65da25815e045",
				),
			)
			blockHeaders1 := mockHeaderFeed(
				t,
				epoch1.StartingBlock,
				expectedTargetBlock1,
				targetBlockHashEpoch1,
				epoch1.EpochLen,
			)

			targetBlockHashEpoch2 := types.BlockHash(
				*utils.HexToFelt(
					t,
					"0x2124ae375432a16ef644f539c3b148f63c706067bf576088f32033fe59c345e",
				),
			)
			// Have the feeder routine feed the next epoch's correct starting block
			blockHeaders2 := mockHeaderFeed(
				t,
				epoch2.StartingBlock-1,
				expectedTargetBlock2,
				&targetBlockHashEpoch2,
				epoch2.EpochLen,
			)

			// Mock SetTargetBlockHashIfExists call
			targetBlockUint64 := expectedTargetBlock1.Uint64()
			mockSigner.
				EXPECT().
				BlockWithTxHashes(rpc.BlockID{Number: &targetBlockUint64}).
				Return(nil, errors.New("Block not found")) // Let's say block does not exist yet

			// Headers feeder routine
			wgFeed := conc.NewWaitGroup()
			wgFeed.Go(func() {
				sendHeaders(t, headersFeed, blockHeaders1)
				sendHeaders(t, headersFeed, blockHeaders2)
				close(headersFeed) // Will never get closed
			})

			// Events receiver routine
			receivedAttestEvents := make(map[types.DoAttest]uint)
			receivedEndOfWindowEvents := uint8(0)
			wgDispatcher := conc.NewWaitGroup()
			wgDispatcher.Go(
				func() {
					registerReceivedEvents(
						t, &dispatcher, receivedAttestEvents, &receivedEndOfWindowEvents,
					)
				},
			)

			validator.Sleep = func(time.Duration) {
				// do nothing (avoid waiting)
			}
			defer func() { validator.Sleep = time.Sleep }()

			metricsServer := mockMetricsServer()
			err := validator.ProcessBlockHeaders(
				headersFeed, mockSigner, logger, &dispatcher, defaultRetries(t), metricsServer,
			)

			// wgFeed is trying to send the 2nd epoch's blocks and is now stuck there because
			// ProcessBlockHeaders already returned as the epoch switch failed as the new epoch's
			// starting block was not correct
			close(headersFeed)
			// Close the channel to terminate the routine (it will panic trying to send a msg
			// to the now closed channel)
			panicRecovered := wgFeed.WaitAndRecover()
			require.Contains(t, panicRecovered.Value, "send on closed channel")

			// Will terminate the registerReceivedEvents routine
			close(dispatcher.DoAttest)
			wgDispatcher.Wait()

			// Assert
			require.Equal(t, 1, len(receivedAttestEvents))

			countEpoch1, exists := receivedAttestEvents[types.DoAttest{BlockHash: targetBlockHashEpoch1}]
			require.True(t, exists)
			require.Equal(t, uint(attestWindow-constants.MIN_ATTESTATION_WINDOW+1), countEpoch1)

			require.Equal(t, uint8(1), receivedEndOfWindowEvents)

			require.ErrorContains(t, err, epoch1.String())
			require.ErrorContains(t, err, epoch2.String())
		})
}

// Test helper function to send headers
func sendHeaders(t *testing.T, headersFeed chan *rpc.BlockHeader, blockHeaders []rpc.BlockHeader) {
	t.Helper()

	for i := range blockHeaders {
		headersFeed <- &blockHeaders[i]
	}
}

// Helper function to register received events and assert them
// Note: to exit this function, close the AttestRequired channel
func registerReceivedEvents[S signerP.Signer](
	t *testing.T,
	dispatcher *validator.EventDispatcher[S],
	receivedAttestRequired map[types.DoAttest]uint,
	receivedEndOfWindowCount *uint8,
) {
	t.Helper()

	for {
		select {
		case attestRequired, isOpen := <-dispatcher.DoAttest:
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
	mockSigner *mocks.MockSigner,
	epoch *types.EpochInfo,
	attestWindow uint64,
	howManyTimes int,
) {
	t.Helper()

	// Mock fetchEpochInfo call
	validatorOperationalAddress := types.AddressFromString(
		"0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e",
	)
	mockSigner.EXPECT().Address().Return(&validatorOperationalAddress).Times(howManyTimes)

	expectedEpochInfoFnCall := rpc.FunctionCall{
		ContractAddress: utils.HexToFelt(t, constants.SEPOLIA_STAKING_CONTRACT_ADDRESS),
		EntryPointSelector: snGoUtils.GetSelectorFromNameFelt(
			"get_attestation_info_by_operational_address",
		),
		Calldata: []*felt.Felt{validatorOperationalAddress.Felt()},
	}

	mockSigner.
		EXPECT().
		Call(expectedEpochInfoFnCall, rpc.BlockID{Tag: "latest"}).
		Return(
			[]*felt.Felt{
				epoch.StakerAddress.Felt(),
				new(felt.Felt).SetBigInt(epoch.Stake.Big()),
				new(felt.Felt).SetUint64(epoch.EpochLen),
				new(felt.Felt).SetUint64(epoch.EpochId),
				new(felt.Felt).SetUint64(epoch.StartingBlock.Uint64()),
			},
			nil,
		).
		Times(howManyTimes)

	// Mock fetchAttestWindow call
	expectedWindowFnCall := rpc.FunctionCall{
		ContractAddress:    utils.HexToFelt(t, constants.SEPOLIA_ATTEST_CONTRACT_ADDRESS),
		EntryPointSelector: snGoUtils.GetSelectorFromNameFelt("attestation_window"),
		Calldata:           []*felt.Felt{},
	}

	mockSigner.
		EXPECT().
		Call(expectedWindowFnCall, rpc.BlockID{Tag: "latest"}).
		Return([]*felt.Felt{new(felt.Felt).SetUint64(attestWindow)}, nil).
		Times(howManyTimes)
}

func TestSetTargetBlockHashIfExists(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockSigner(mockCtrl)
	logger := utils.NewNopZapLogger()

	t.Run("Target block does not already exist", func(t *testing.T) {
		targetBlockNumber := uint64(1)
		mockAccount.
			EXPECT().
			BlockWithTxHashes(rpc.BlockID{Number: &targetBlockNumber}).
			Return(nil, errors.New("Block not found"))

		attestInfo := types.AttestInfo{
			TargetBlock: types.BlockNumber(targetBlockNumber),
		}
		validator.SetTargetBlockHashIfExists(mockAccount, logger, &attestInfo)

		require.Equal(t, types.BlockHash{}, attestInfo.TargetBlockHash)
	})

	t.Run("Target block already exists but is pending", func(t *testing.T) {
		targetBlockNumber := uint64(1)
		mockAccount.
			EXPECT().
			BlockWithTxHashes(rpc.BlockID{Number: &targetBlockNumber}).
			Return(&rpc.PendingBlockTxHashes{}, nil)

		attestInfo := types.AttestInfo{
			TargetBlock: types.BlockNumber(targetBlockNumber),
		}
		validator.SetTargetBlockHashIfExists(mockAccount, logger, &attestInfo)

		require.Equal(t, types.BlockHash{}, attestInfo.TargetBlockHash)
	})

	t.Run("Target block already exists and is not pending", func(t *testing.T) {
		targetBlockHashFelt := utils.HexToFelt(t, "0x123")
		blockWithTxs := rpc.BlockTxHashes{
			BlockHeader: rpc.BlockHeader{
				Hash: targetBlockHashFelt,
			},
		}
		targetBlockNumber := uint64(1)
		mockAccount.
			EXPECT().
			BlockWithTxHashes(rpc.BlockID{Number: &targetBlockNumber}).
			Return(&blockWithTxs, nil)

		targetBlockHash := types.BlockHash(*targetBlockHashFelt)

		attestInfo := types.AttestInfo{
			TargetBlock: types.BlockNumber(targetBlockNumber),
		}
		validator.SetTargetBlockHashIfExists(mockAccount, logger, &attestInfo)

		require.Equal(t, targetBlockHash, attestInfo.TargetBlockHash)
	})
}

func TestFetchEpochAndAttestInfoWithRetry(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockSigner := mocks.NewMockSigner(mockCtrl)
	mockSigner.EXPECT().ValidationContracts().Return(
		validator.SepoliaValidationContracts(t),
	).AnyTimes()

	noOpLogger := utils.NewNopZapLogger()

	t.Run("Return error fetching epoch info", func(t *testing.T) {
		// Sequence of actions:
		// 1. Fetch epoch and attest info: error, causing to retry 10 times
		// 2. After the 10 retries, exit with error

		validatorOperationalAddress := types.AddressFromString("0x123")
		const fetchingError = "some internal error fetching epoch info"
		mockFailedFetchingEpochAndAttestInfo(
			t,
			mockSigner,
			&validatorOperationalAddress,
			fetchingError,
			11,
		)

		fetchedError := errors.Errorf(
			"Error when calling entrypoint `get_attestation_info_by_operational_address`: %s",
			fetchingError,
		)

		newEpochID := "123"
		validator.Sleep = func(time.Duration) {
			// do nothing (avoid waiting)
		}
		defer func() { validator.Sleep = time.Sleep }()

		newEpochInfo, newAttestInfo, err := validator.FetchEpochAndAttestInfoWithRetry(
			mockSigner, noOpLogger, nil, nil, defaultRetries(t), newEpochID,
		)

		require.Zero(t, newEpochInfo)
		require.Zero(t, newAttestInfo)
		require.ErrorContains(t, err, newEpochID)
		require.ErrorContains(t, err, fetchedError.Error())
	})

	t.Run(
		"Return epoch switch error (combine fetch info & epoch switch errors)",
		func(t *testing.T) {
			// Sequence of actions:
			// 1. Fetch epoch and attest info: error
			// 2. Epoch switch: error, causing to retry 10 times
			// 3. After the 10 retries, exit with error

			validatorOperationalAddress := types.AddressFromString(
				"0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e",
			)
			fetchingError := "some internal error fetching epoch info"
			mockFailedFetchingEpochAndAttestInfo(
				t, mockSigner, &validatorOperationalAddress, fetchingError, 1,
			)

			stakerAddress := validatorOperationalAddress
			var stake uint64 = 1000000000000000000
			var epochLength uint64 = 40

			epoch1 := types.EpochInfo{
				StakerAddress: stakerAddress,
				Stake:         uint128.New(stake, 0),
				EpochLen:      epochLength,
				EpochId:       1515,
				StartingBlock: 639230,
			}

			// Mock FetchEpochAndAttestInfo: returns a wrong next epoch (10 times)
			epoch2 := types.EpochInfo{
				StakerAddress: stakerAddress,
				Stake:         uint128.New(stake, 0),
				EpochLen:      epochLength,
				EpochId:       1516,
				StartingBlock: 639271, // wrong new epoch start (1 block after correct block)
			}
			var attestWindow uint64 = 16
			mockSuccessfullyFetchedEpochAndAttestInfo(
				t,
				mockSigner,
				&epoch2,
				attestWindow,
				11,
			)

			validator.Sleep = func(time.Duration) { /* do nothing (avoid waiting) */ }
			defer func() { validator.Sleep = time.Sleep }()

			newEpochInfo, newAttestInfo, err := validator.FetchEpochAndAttestInfoWithRetry(
				mockSigner,
				noOpLogger,
				&epoch1,
				validator.CorrectEpochSwitch,
				defaultRetries(t),
				strconv.FormatUint(epoch1.EpochId+1, 10),
			)

			require.Error(t, err)
			require.ErrorContains(t, err, epoch1.String())
			require.ErrorContains(t, err, epoch2.String())
			require.Zero(t, newEpochInfo)
			require.Zero(t, newAttestInfo)
		})

	t.Run("Successfully return epoch and attest info", func(t *testing.T) {
		// Sequence of actions:
		// 1. Fetch epoch and attest info: error (causing retry)
		// 2. Fetch epoch and attest info: successful
		// 3. Epoch switch: successful
		// 4. Return new epoch and attest info

		// Mock 1st call to FetchEpochAndAttestInfo: fails
		validatorOperationalAddress := types.AddressFromString(
			"0x011efbf2806a9f6fe043c91c176ed88c38907379e59d2d3413a00eeeef08aa7e",
		)
		fetchingError := "some internal error fetching epoch info"
		mockFailedFetchingEpochAndAttestInfo(
			t, mockSigner, &validatorOperationalAddress, fetchingError, 1,
		)

		// fetchEpochInfo now works and returns a correct next epoch
		stakerAddress := validatorOperationalAddress
		var stake uint64 = 1000000000000000000
		var epochLength uint64 = 40

		epoch1 := types.EpochInfo{
			StakerAddress: stakerAddress,
			Stake:         uint128.New(stake, 0),
			EpochLen:      epochLength,
			EpochId:       1515,
			StartingBlock: 639230,
		}

		// Mock 2nd FetchEpochAndAttestInfo: returns a correct next epoch
		epoch2 := types.EpochInfo{
			StakerAddress: stakerAddress,
			Stake:         uint128.New(stake, 0),
			EpochLen:      epochLength,
			EpochId:       1516,
			StartingBlock: 639270,
		}
		attestWindow := uint64(16)
		mockSuccessfullyFetchedEpochAndAttestInfo(t, mockSigner, &epoch2, attestWindow, 1)

		validator.Sleep = func(time.Duration) { /* do nothing (avoid waiting) */ }
		defer func() { validator.Sleep = time.Sleep }()

		newEpochInfo, newAttestInfo, err := validator.FetchEpochAndAttestInfoWithRetry(
			mockSigner,
			noOpLogger,
			&epoch1,
			validator.CorrectEpochSwitch,
			types.NewRetries(),
			strconv.FormatUint(epoch1.EpochId+1, 10),
		)

		expectedEpoch2TargetBlock := types.BlockNumber(639291)
		expectedEpoch2AttestInfo := types.AttestInfo{
			TargetBlock:     expectedEpoch2TargetBlock,
			TargetBlockHash: types.BlockHash{},
			WindowStart: expectedEpoch2TargetBlock +
				types.BlockNumber(constants.MIN_ATTESTATION_WINDOW),
			WindowEnd: expectedEpoch2TargetBlock + types.BlockNumber(attestWindow),
		}
		require.NoError(t, err)
		require.Equal(t, &epoch2, &newEpochInfo)
		require.Equal(t, expectedEpoch2AttestInfo, newAttestInfo)
	})
}

func defaultRetries(t *testing.T) types.Retries {
	t.Helper()

	r := types.NewRetries()
	r.Set(10)
	return r
}

func mockHeaderFeed(
	t *testing.T,
	startingBlock,
	targetBlock types.BlockNumber,
	targetBlockHash *types.BlockHash,
	epochLength uint64,
) []rpc.BlockHeader {
	t.Helper()

	blockHeaders := make([]rpc.BlockHeader, epochLength)
	for i := range uint64(epochLength) {
		blockNumber := types.BlockNumber(i) + startingBlock

		// All block hashes are set to 0x1 except for the target block
		blockHash := new(felt.Felt).SetUint64(1)
		if blockNumber == targetBlock {
			blockHash = targetBlockHash.Felt()
		}

		blockHeaders[i] = rpc.BlockHeader{
			Number: blockNumber.Uint64(),
			Hash:   blockHash,
		}
	}
	return blockHeaders
}

func mockFailedFetchingEpochAndAttestInfo(
	t *testing.T,
	mockAccount *mocks.MockSigner,
	operationalAddress *types.Address,
	fetchingError string,
	howManyTimes int,
) {
	t.Helper()

	// Mock fetchEpochInfo call
	mockAccount.EXPECT().Address().Return(operationalAddress).Times(howManyTimes)

	expectedEpochInfoFnCall := rpc.FunctionCall{
		ContractAddress: utils.HexToFelt(t, constants.SEPOLIA_STAKING_CONTRACT_ADDRESS),
		EntryPointSelector: snGoUtils.GetSelectorFromNameFelt(
			"get_attestation_info_by_operational_address",
		),
		Calldata: []*felt.Felt{operationalAddress.Felt()},
	}

	mockAccount.
		EXPECT().
		Call(expectedEpochInfoFnCall, rpc.BlockID{Tag: "latest"}).
		Return(nil, errors.New(fetchingError)).
		Times(howManyTimes)
}

func mockMetricsServer() *metrics.Metrics {
	// Create a mock metrics server for testing
	logger := utils.NewNopZapLogger()
	return metrics.NewMetrics("localhost:9090", "SN_SEPOLIA", logger)
}
