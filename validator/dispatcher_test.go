package validator_test

import (
	"testing"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
	"github.com/sourcegraph/conc"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// type EndOfWindow = struct{}
//
// type dispatcherEvent interface {
// 	types.DoAttest
// 	types.PrepareAttest
// 	EndOfWindow
// }
//
// type actions[E dispatcherEvent] struct {
// 	event  E
// 	before func()
// 	after  func()
// }
//
// func TestDispatch2(t *testing.T) {
// 	mockCtrl := gomock.NewController(t)
// 	t.Cleanup(mockCtrl.Finish)
//
// 	setup := func() (signer.Signer, utils.ZapLogger, metrics.Tracer) {
// 		mockSigner := mocks.NewMockSigner(mockCtrl)
// 		noOpLogger := utils.NewNopZapLogger()
// 		noOpTracer := metrics.NewNoOpMetrics()
//
// 		contractAddresses := new(config.ContractAddresses).SetDefaults("SN_SEPOLIA")
// 		validationContracts := types.ValidationContractsFromAddresses(contractAddresses)
//
// 		// validation contracts to expect
// 		mockSigner.EXPECT().ValidationContracts().Return(
// 			validationContracts,
// 		).AnyTimes()
//
// 		return mockSigner, *noOpLogger, noOpTracer
// 	}
// }

func TestDispatch(t *testing.T) {
	/*
		mockCtrl := gomock.NewController(t)
		t.Cleanup(mockCtrl.Finish)

		mockSigner := mocks.NewMockSigner(mockCtrl)
		mockSigner.EXPECT().ValidationContracts().Return(
			validator.SepoliaValidationContracts(t),
		).AnyTimes()
		mockSigner.EXPECT().ValidationContracts().Return(
			validator.SepoliaValidationContracts(t),
		).AnyTimes()

		logger := utils.NewNopZapLogger()
		tracer := metrics.NewNoOpMetrics()
	*/

	/*

		// todo(rdr): I dislike this excessive mocking, find a fix for this.
		t.Run("Simple scenario: only 1 attest that succeeds", func(t *testing.T) {
			// Setup
			blockhash := (*types.BlockHash)(new(felt.Felt).SetUint64(1))
			txHash := new(felt.Felt).SetUint64(0x123)
			mockSigner.
				EXPECT().
				BuildAttestTransaction(blockhash).
				Return(rpc.BroadcastInvokeTxnV3{}, nil)
			mockSigner.
				EXPECT().
				SignTransaction(gomock.Any()).
				Return(&rpc.BroadcastInvokeTxnV3{}, nil).
				AnyTimes()
			mockSigner.
				EXPECT().
				EstimateFee(&rpc.BroadcastInvokeTxnV3{}).
				Return(rpc.FeeEstimation{
					L1GasConsumed:     utils.HexToFelt(t, "0x123"),
					L1GasPrice:        utils.HexToFelt(t, "0x456"),
					L2GasConsumed:     utils.HexToFelt(t, "0x123"),
					L2GasPrice:        utils.HexToFelt(t, "0x456"),
					L1DataGasConsumed: utils.HexToFelt(t, "0x123"),
					L1DataGasPrice:    utils.HexToFelt(t, "0x456"),
					OverallFee:        utils.HexToFelt(t, "0x123"),
					FeeUnit:           rpc.UnitStrk,
				}, nil).
				Times(1)
			mockSigner.
				EXPECT().
				InvokeTransaction(gomock.Any()).
				Return(&rpc.AddInvokeTransactionResponse{Hash: txHash}, nil)

			dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
			wg := &conc.WaitGroup{}
			wg.Go(func() { dispatcher.Dispatch(mockSigner, nil, math.Inf(1), logger, tracer) })

			// Send event
			dispatcher.DoAttest <- types.DoAttest{BlockHash: *blockhash}

			// Preparation for EndOfWindow event
			mockSigner.EXPECT().
				GetTransactionStatus(txHash).
				Return(&rpc.TxnStatusResult{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
				}, nil)

			// Send EndOfWindow
			dispatcher.EndOfWindow <- struct{}{}

			close(dispatcher.DoAttest)
			// Wait for dispatch routine to finish
			wg.Wait()

			// Assert
			expectedAttest := validator.AttestTracker{
				Transaction: validator.AttestTransaction{},
				Hash:        felt.Zero,
				Status:      validator.Iddle,
			}
			require.Equal(t, expectedAttest, dispatcher.CurrentAttest)
		})
	*/

	/*
		t.Run(
			"Same AttestRequired events are ignored if already ongoing or successful",
			func(t *testing.T) {
				// Sequence of actions:
				// - an AttestRequired event A is emitted and processed
				// - an AttestRequired event A is emitted and ignored (as 1st one is getting processed)
				// - an AttestRequired event A is emitted and ignored (as 1st one finished & succeeded)

				// Setup
				dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
				blockHashFelt := new(felt.Felt).SetUint64(1)

				attestAddr := validationContracts.Attest.Felt()
				calls := []rpc.InvokeFunctionCall{{
					ContractAddress: attestAddr,
					FunctionName:    "attest",
					CallData:        []*felt.Felt{blockHashFelt},
				}}
				addTxHash := utils.HexToFelt(t, "0x123")
				mockedAddTxResp := rpc.AddInvokeTransactionResponse{Hash: addTxHash}
				// We expect BuildAndSendInvokeTxn to be called only once (even though 3 events are sent)
				mockSigner.EXPECT().
					BuildAndSendInvokeTxn(
						calls, constants.FEE_ESTIMATION_MULTIPLIER,
					).
					Return(&mockedAddTxResp, nil).
					Times(1)

				// Start routine
				wg := &conc.WaitGroup{}
				wg.Go(func() { dispatcher.Dispatch(mockSigner, logger, tracer) })

				// Send the same event x3
				blockHash := (*types.BlockHash)(blockHashFelt)
				dispatcher.DoAttest <- types.DoAttest{BlockHash: blockHash}

				// Preparation for 2nd event

				// Invoke tx is RECEIVED
				mockSigner.EXPECT().
					GetTransactionStatus(addTxHash).
					Return(&rpc.TxnStatusResult{
						FinalityStatus: rpc.TxnStatus_Received,
					}, nil).
					Times(1)

				// This 2nd event gets ignored when status is ongoing
				// Proof: only 1 call to BuildAndSendInvokeTxn is asserted
				dispatcher.DoAttest <- types.DoAttest{BlockHash: blockHash}

				// Preparation for 3rd event

				// Invoke tx ended up ACCEPTED
				mockSigner.EXPECT().
					GetTransactionStatus(addTxHash).
					Return(&rpc.TxnStatusResult{
						FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
						ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
					}, nil).
					Times(1)

				// This 3rd event gets ignored also when status is successful
				// Proof: only 1 call to BuildAndSendInvokeTxn is asserted
				dispatcher.DoAttest <- types.DoAttest{BlockHash: blockHash}
				close(dispatcher.DoAttest)

				// Wait for dispatch routine (and consequently its spawned subroutines) to finish
				wg.Wait()

				// Re-assert (3rd event got ignored)
				expectedAttest := validator.AttestTracker{
					Transaction: validator.AttestTransaction{},
					Hash:        *addTxHash,
					Status:      validator.Successful,
				}
				require.Equal(t, expectedAttest, dispatcher.CurrentAttest)
			},
		)

		t.Run("Same AttestRequired events are ignored until attestation fails", func(t *testing.T) {
			// Sequence of actions:
			// - an AttestRequired event A is emitted and processed
			// - an AttestRequired event A is emitted and ignored (as 1st one is getting processed)
			// - an AttestRequired event A is emitted and processed (as 1st one finished & failed)

			// Setup
			dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
			blockHashFelt := new(felt.Felt).SetUint64(1)

			attestAddr := validationContracts.Attest.Felt()
			calls := []rpc.InvokeFunctionCall{{
				ContractAddress: attestAddr,
				FunctionName:    "attest",
				CallData:        []*felt.Felt{blockHashFelt},
			}}
			addTxHash1 := utils.HexToFelt(t, "0x123")
			mockedAddTxResp1 := rpc.AddInvokeTransactionResponse{Hash: addTxHash1}

			// We expect BuildAndSendInvokeTxn to be called only once (for the 2 first events)
			mockSigner.EXPECT().
				BuildAndSendInvokeTxn(
					calls, constants.FEE_ESTIMATION_MULTIPLIER,
				).
				Return(&mockedAddTxResp1, nil).
				Times(1)

			// Start routine
			wg := &conc.WaitGroup{}
			wg.Go(func() { dispatcher.Dispatch(mockSigner, logger, tracer) })

			// Send the same event x3
			blockHash := (*types.BlockHash)(blockHashFelt)
			dispatcher.DoAttest <- types.DoAttest{BlockHash: blockHash}

			// Preparation for 2nd event

			// Invoke tx status is RECEIVED
			mockSigner.EXPECT().
				GetTransactionStatus(addTxHash1).
				Return(&rpc.TxnStatusResult{
					FinalityStatus: rpc.TxnStatus_Received,
				}, nil).
				Times(1)

			// This 2nd event gets ignored when status is ongoing
			// Proof: only 1 call to BuildAndSendInvokeTxn is asserted so far
			dispatcher.DoAttest <- types.DoAttest{BlockHash: blockHash}

			// Preparation for 3rd event

			// Invoke tx fails, will make a new invoke tx
			mockSigner.EXPECT().
				GetTransactionStatus(addTxHash1).
				Return(&rpc.TxnStatusResult{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusREVERTED,
					FailureReason:   "some failure reason",
				}, nil).
				Times(1)

			addTxHash2 := utils.HexToFelt(t, "0x456")
			mockedAddTxResp2 := rpc.AddInvokeTransactionResponse{Hash: addTxHash2}

			// We expect a 2nd call to BuildAndSendInvokeTxn
			mockSigner.EXPECT().
				BuildAndSendInvokeTxn(
					calls, constants.FEE_ESTIMATION_MULTIPLIER,
				).
				Return(&mockedAddTxResp2, nil).
				Times(1)

			// This 3rd event does not get ignored as invoke attestation has failed
			// Proof: a 2nd call to BuildAndSendInvokeTxn is asserted
			dispatcher.DoAttest <- types.DoAttest{BlockHash: blockHash}
			close(dispatcher.DoAttest)

			// Wait for dispatch routine (and consequently its spawned subroutines) to finish
			wg.Wait()

			// Assert after dispatcher routine has finished processing the 3rd event
			expectedAttest := validator.AttestTracker{
				Transaction: validator.AttestTransaction{},
				Hash:        *addTxHash2,
				Status:      validator.Ongoing,
			}
			require.Equal(t, expectedAttest, dispatcher.CurrentAttest)
		})
		t.Run(
			"Failed sending invoke tx also (just like TrackAttest) marks attest as failed",
			func(t *testing.T) {
				// Sequence of actions:
				// - an AttestRequired event A is emitted and processed (invoke tx, not TrackAttest, fails)
				// - an AttestRequired event A is emitted and considered (as 1st one failed)
				// - an AttestRequired event A is emitted and ignored (as 2nd one succeeded)

				// Setup
				dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
				blockHashFelt := new(felt.Felt).SetUint64(1)

				attestAddr := validationContracts.Attest.Felt()
				calls := []rpc.InvokeFunctionCall{{
					ContractAddress: attestAddr,
					FunctionName:    "attest",
					CallData:        []*felt.Felt{blockHashFelt},
				}}

				// We expect BuildAndSendInvokeTxn to fail once
				mockSigner.EXPECT().
					BuildAndSendInvokeTxn(
						calls, constants.FEE_ESTIMATION_MULTIPLIER,
					).
					Return(nil, errors.New("sending invoke tx failed for some reason")).
					Times(1)

				// Start routine
				wg := &conc.WaitGroup{}
				wg.Go(func() { dispatcher.Dispatch(mockSigner, logger, tracer) })

				// Send the same event x2
				blockHash := (*types.BlockHash)(blockHashFelt)
				dispatcher.DoAttest <- types.DoAttest{BlockHash: blockHash}

				// Preparation for 2nd event

				// GetTransactionStatus does not get called as invoke tx failed during sending
				// Nothing to track

				// Next call to BuildAndSendInvokeTxn succeeds
				addTxHash := utils.HexToFelt(t, "0x123")
				mockedAddTxResp := rpc.AddInvokeTransactionResponse{Hash: addTxHash}
				mockSigner.EXPECT().
					BuildAndSendInvokeTxn(
						calls, constants.FEE_ESTIMATION_MULTIPLIER,
					).
					Return(&mockedAddTxResp, nil).
					Times(1)

				// This 2nd event gets considered as previous one failed
				dispatcher.DoAttest <- types.DoAttest{BlockHash: blockHash}

				// Preparation for 3rd event

				// We expect GetTransactionStatus to be called only once
				mockSigner.EXPECT().
					GetTransactionStatus(addTxHash).
					Return(&rpc.TxnStatusResult{
						FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
						ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
					}, nil).
					Times(1)

				dispatcher.DoAttest <- types.DoAttest{BlockHash: blockHash}

				close(dispatcher.DoAttest)
				// Wait for dispatch routine (and consequently its spawned subroutines) to finish
				wg.Wait()

				// Assert after dispatcher routine has finished processing the 3rd event
				expectedAttest := validator.AttestTracker{
					Transaction: validator.AttestTransaction{},
					Hash:        *addTxHash,
					Status:      validator.Successful,
				}
				require.Equal(t, expectedAttest, dispatcher.CurrentAttest)
			})

		t.Run("AttestRequired events transition with EndOfWindow events", func(t *testing.T) {
			// Sequence of actions:
			// - an AttestRequired event A is emitted and processed (successful)
			// - an EndOfWindow event for A is emitted and processed
			// - an AttestRequired event B is emitted and processed (failed)
			// - an EndOfWindow event for B is emitted and processed

			// Setup
			dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()

			// For event A
			blockHashFeltA := new(felt.Felt).SetUint64(1)
			attestAddr := validationContracts.Attest.Felt()
			callsA := []rpc.InvokeFunctionCall{{
				ContractAddress: attestAddr,
				FunctionName:    "attest",
				CallData:        []*felt.Felt{blockHashFeltA},
			}}
			addTxHashA := utils.HexToFelt(t, "0x123")
			mockedAddTxRespA := rpc.AddInvokeTransactionResponse{Hash: addTxHashA}

			// We expect BuildAndSendInvokeTxn to be called once for event A
			mockSigner.EXPECT().
				BuildAndSendInvokeTxn(callsA, constants.FEE_ESTIMATION_MULTIPLIER).
				Return(&mockedAddTxRespA, nil).
				Times(1)

			// We expect GetTransactionStatus to be called for event A (triggered by EndOfWindow)
			mockSigner.EXPECT().
				GetTransactionStatus(addTxHashA).
				Return(&rpc.TxnStatusResult{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
				}, nil).
				Times(1)

			// For event B
			blockHashFeltB := new(felt.Felt).SetUint64(2)
			callsB := []rpc.InvokeFunctionCall{{
				ContractAddress: attestAddr,
				FunctionName:    "attest",
				CallData:        []*felt.Felt{blockHashFeltB},
			}}
			addTxHashB := utils.HexToFelt(t, "0x456")
			mockedAddTxRespB := rpc.AddInvokeTransactionResponse{Hash: addTxHashB}

			// We expect BuildAndSendInvokeTxn to be called once for event B
			mockSigner.EXPECT().
				BuildAndSendInvokeTxn(
					callsB, constants.FEE_ESTIMATION_MULTIPLIER,
				).
				Return(&mockedAddTxRespB, nil).
				Times(1)

			// We expect GetTransactionStatus to be called once for event B (triggered by EndOfWindow)
			mockSigner.EXPECT().
				GetTransactionStatus(addTxHashB).
				Return(&rpc.TxnStatusResult{
					FinalityStatus: rpc.TxnStatus_Rejected,
				}, nil).
				Times(1)

			// Start routine
			wg := &conc.WaitGroup{}
			wg.Go(func() { dispatcher.Dispatch(mockSigner, logger, tracer) })

			// Send event A
			blockHashA := (*types.BlockHash)(blockHashFeltA)
			dispatcher.DoAttest <- types.DoAttest{BlockHash: blockHashA}

			// Send EndOfWindow event for event A
			dispatcher.EndOfWindow <- struct{}{}

			// Send event B
			blockHashB := (*types.BlockHash)(blockHashFeltB)
			dispatcher.DoAttest <- types.DoAttest{BlockHash: blockHashB}

			// Send EndOfWindow event for event B
			dispatcher.EndOfWindow <- struct{}{}

			close(dispatcher.DoAttest)
			// Wait for dispatch routine to finish executing
			wg.Wait()

			// End of execution assertion: attestation B has failed
			expectedAttest := validator.AttestTracker{
				Transaction: validator.AttestTransaction{},
				Hash:        *addTxHashB,
				Status:      validator.Failed,
			}
			require.Equal(t, expectedAttest, dispatcher.CurrentAttest)
		})
	*/
}

func TestTrackAttest(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockSigner := mocks.NewMockSigner(mockCtrl)
	logger := log.NewNopZapLogger()

	t.Run("attestation fails if error is transaction status not found", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		mockSigner.EXPECT().
			TransactionStatus(txHash).
			Return(nil, validator.ErrTxnHashNotFound)

		txStatus := validator.TrackAttest(mockSigner, logger, txHash)

		require.Equal(t, validator.Ongoing, txStatus)
	})

	// QA report 01, finding OBS-1: a failed status request says nothing about the transaction
	t.Run(
		"attestation is still ongoing if the transaction status request fails",
		func(t *testing.T) {
			txHash := new(felt.Felt).SetUint64(1)

			mockSigner.EXPECT().
				TransactionStatus(txHash).
				Return(nil, errors.New("some internal error"))

			txStatus := validator.TrackAttest(mockSigner, logger, txHash)

			require.Equal(t, validator.Ongoing, txStatus)
		})

	t.Run("attestation fails if accepted but REVERTED", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		revertError := "reverted for some reason"
		mockSigner.EXPECT().
			TransactionStatus(txHash).
			Return(&rpc.TxnStatusResult{
				FinalityStatus:  rpc.TxnStatusAcceptedOnL2,
				ExecutionStatus: rpc.TxnExecutionStatusREVERTED,
				FailureReason:   revertError,
			}, nil)

		txStatus := validator.TrackAttest(mockSigner, logger, txHash)

		require.Equal(t, validator.Failed, txStatus)
	})

	t.Run("attestation succeeds if accepted & SUCCEEDED", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		mockSigner.EXPECT().
			TransactionStatus(txHash).
			Return(&rpc.TxnStatusResult{
				FinalityStatus:  rpc.TxnStatusAcceptedOnL2,
				ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
			}, nil)

		txStatus := validator.TrackAttest(mockSigner, logger, txHash)

		require.Equal(t, validator.Successful, txStatus)
	})
}

// When the auto-claim reward feature is not configured, the dispatcher receives
// a nil *RewardSigner. Processing an EndOfWindow event must not panic.
func TestDispatchEndOfWindowWithoutRewardSigner(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	signerAddress, err := types.AddressFromString("0x123")
	require.NoError(t, err)
	balanceChecked := make(chan struct{}, 1)

	mockSigner := mocks.NewMockSigner(mockCtrl)
	mockSigner.EXPECT().Address().Return(&signerAddress).AnyTimes()
	mockSigner.EXPECT().
		ValidationContracts().
		Return(validator.SepoliaValidationContracts(t)).
		AnyTimes()
	// Called by `TrackAttest` when the window ends without a successful attest
	mockSigner.EXPECT().
		TransactionStatus(gomock.Any()).
		Return(nil, validator.ErrTxnHashNotFound).
		AnyTimes()
	// Called by the `CheckBalance` goroutine launched at the end of the window
	mockSigner.EXPECT().
		Call(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ rpc.FunctionCall, _ rpc.BlockID) ([]*felt.Felt, error) {
			select {
			case balanceChecked <- struct{}{}:
			default:
			}

			return nil, errors.New("balance unavailable")
		}).
		AnyTimes()

	dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
	wg := conc.NewWaitGroup()
	wg.Go(func() {
		dispatcher.Dispatch(
			mockSigner, nil, 100, log.NewNopZapLogger(), metrics.NewNoOpMetrics(),
		)
	})

	dispatcher.EndOfWindow <- struct{}{}
	close(dispatcher.DoAttest)

	recovered := wg.WaitAndRecover()
	require.Nil(
		t, recovered, "Dispatch panicked on EndOfWindow with a nil reward signer: %s",
		recovered,
	)

	// Make sure the balance check goroutine is done, confirming that the EndOfWindow
	// event was processed correctly.
	select {
	case <-balanceChecked:
	case <-time.After(5 * time.Second):
		require.Fail(t, "balance check was not performed at the end of the window")
	}
}

// When the auto-claim reward feature is configured, processing an EndOfWindow event
// must trigger a reward claim.
func TestDispatchEndOfWindowClaimsRewards(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	signerAddress, err := types.AddressFromString("0x123")
	require.NoError(t, err)
	rewardAddress, err := types.AddressFromString("0x456")
	require.NoError(t, err)
	balanceChecked := make(chan struct{}, 2)
	rewardsClaimed := make(chan struct{}, 1)

	mockSigner := mocks.NewMockSigner(mockCtrl)
	mockSigner.EXPECT().Address().Return(&signerAddress).AnyTimes()
	mockSigner.EXPECT().
		ValidationContracts().
		Return(validator.SepoliaValidationContracts(t)).
		AnyTimes()
	// Called by `TrackAttest` when the window ends without a successful attest
	mockSigner.EXPECT().
		TransactionStatus(gomock.Any()).
		Return(nil, validator.ErrTxnHashNotFound).
		AnyTimes()
	// Called by the `CheckBalance` goroutine for the signer and the reward accounts
	mockSigner.EXPECT().
		Call(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ rpc.FunctionCall, _ rpc.BlockID) ([]*felt.Felt, error) {
			balanceChecked <- struct{}{}

			return nil, errors.New("balance unavailable")
		}).
		Times(2)

	mockRewardSigner := mocks.NewMockRewardSigner(mockCtrl)
	mockRewardSigner.EXPECT().Address().Return(&rewardAddress).AnyTimes()
	mockRewardSigner.EXPECT().
		ClaimRewards(gomock.Any()).
		Do(func(metrics.Tracer) { rewardsClaimed <- struct{}{} }).
		Times(1)

	dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
	wg := conc.NewWaitGroup()
	wg.Go(func() {
		dispatcher.Dispatch(
			mockSigner, mockRewardSigner, 100, log.NewNopZapLogger(), metrics.NewNoOpMetrics(),
		)
	})

	dispatcher.EndOfWindow <- struct{}{}
	close(dispatcher.DoAttest)
	require.Nil(t, wg.WaitAndRecover())

	// Wait for the goroutines launched at the end of the window
	timeout := time.After(5 * time.Second)
	for range 2 {
		select {
		case <-balanceChecked:
		case <-timeout:
			require.Fail(t, "the accounts balances were not checked at the end of the window")
		}
	}
	select {
	case <-rewardsClaimed:
	case <-timeout:
		require.Fail(t, "rewards were not claimed at the end of the window")
	}
}

// Records the attestation outcome metrics, ignoring the rest
type attestTracer struct {
	metrics.NoOpMetrics

	submitted int
	confirmed int
	failures  int
}

func (a *attestTracer) RecordAttestationSubmitted() { a.submitted++ }

func (a *attestTracer) RecordAttestationConfirmed() { a.confirmed++ }

func (a *attestTracer) RecordAttestationFailure() { a.failures++ }

// Signer mock for the attest path. Only the calls made by the balance check launched at the
// end of a window are set up: every test sets the attest calls it expects. A value is sent on
// the returned channel by every balance check.
func newAttestSigner(t *testing.T) (mockSigner *mocks.MockSigner, balanceChecked chan struct{}) {
	t.Helper()

	signerAddress, err := types.AddressFromString("0x123")
	require.NoError(t, err)
	balanceChecked = make(chan struct{}, 10)

	mockSigner = mocks.NewMockSigner(gomock.NewController(t))
	mockSigner.EXPECT().Address().Return(&signerAddress).AnyTimes()
	mockSigner.EXPECT().
		ValidationContracts().
		Return(validator.SepoliaValidationContracts(t)).
		AnyTimes()
	mockSigner.EXPECT().
		Call(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ rpc.FunctionCall, _ rpc.BlockID) ([]*felt.Felt, error) {
			balanceChecked <- struct{}{}

			return nil, errors.New("balance unavailable")
		}).
		AnyTimes()

	return mockSigner, balanceChecked
}

// Waits for the balance check launched at the end of a window, so no mock call happens after
// the test ends
func waitBalanceCheck(t *testing.T, balanceChecked chan struct{}) {
	t.Helper()

	select {
	case <-balanceChecked:
	case <-time.After(5 * time.Second):
		require.Fail(t, "balance check was not performed at the end of the window")
	}
}

// Starts the dispatcher routine. The returned function stops it and waits for it to end, at
// which point every event sent before was fully processed.
func startDispatcher(
	t *testing.T, signer *mocks.MockSigner, tracer metrics.Tracer,
) (dispatcher *validator.EventDispatcher[*mocks.MockSigner], stop func()) {
	t.Helper()

	d := validator.NewEventDispatcher[*mocks.MockSigner]()
	dispatcher = &d
	wg := conc.NewWaitGroup()
	wg.Go(func() {
		dispatcher.Dispatch(signer, nil, 0, log.NewNopZapLogger(), tracer)
	})

	return dispatcher, func() {
		close(dispatcher.DoAttest)
		require.Nil(t, wg.WaitAndRecover())
	}
}

// Sends an event to the dispatcher, failing the test if the dispatcher routine does not take
// it (for example because an unexpected mock call ended it)
func sendEvent[E any](t *testing.T, ch chan<- E, event E) {
	t.Helper()

	select {
	case ch <- event:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the dispatcher did not take the event")
	}
}

func attestTxn() rpc.BroadcastInvokeTxnV3 {
	return rpc.BroadcastInvokeTxnV3{Nonce: new(felt.Felt).SetUint64(1)}
}

func feeEstimate() rpc.FeeEstimation {
	one := new(felt.Felt).SetUint64(1)

	return rpc.FeeEstimation{
		FeeEstimationCommon: rpc.FeeEstimationCommon{
			L1GasConsumed:     one,
			L1GasPrice:        one,
			L2GasConsumed:     one,
			L2GasPrice:        one,
			L1DataGasConsumed: one,
			L1DataGasPrice:    one,
			OverallFee:        one,
		},
	}
}

// A failure while (re)building the attest transaction or refreshing its nonce must not leave
// the tracker waiting for a transaction that was never sent (QA report 01, finding P1): the
// attempt is marked as failed and retried on the next block of the window.
func TestDispatchRetriesFailedAttempts(t *testing.T) {
	blockHash := types.BlockHash(*new(felt.Felt).SetUint64(0xb10c))
	txHash := new(felt.Felt).SetUint64(0xa77e57)
	signerDown := errors.New("signer unreachable")

	t.Run("Nonce refresh fails at window start", func(t *testing.T) {
		mockSigner, _ := newAttestSigner(t)
		gomock.InOrder(
			// Prepared in advance
			mockSigner.EXPECT().BuildAttestTransaction(&blockHash).Return(attestTxn(), nil),
			mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil),
			// 1st block of the window
			mockSigner.EXPECT().Nonce().Return(nil, errors.New("-32603 Internal Error")),
			// 2nd block of the window
			mockSigner.EXPECT().Nonce().Return(new(felt.Felt).SetUint64(1), nil),
			mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(feeEstimate(), nil),
			mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil),
			mockSigner.EXPECT().
				InvokeTransaction(gomock.Any()).
				Return(rpc.AddInvokeTransactionResponse{Hash: txHash}, nil),
		)
		tracer := new(attestTracer)
		dispatcher, stop := startDispatcher(t, mockSigner, tracer)

		sendEvent(t, dispatcher.PrepareAttest, types.PrepareAttest{BlockHash: blockHash})
		sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
		sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
		stop()

		require.Equal(t, validator.Ongoing, dispatcher.CurrentAttest.Status)
		require.Equal(t, *txHash, dispatcher.CurrentAttest.Hash)
		require.Equal(t, 1, tracer.submitted)
	})

	t.Run("Signer down across the window start", func(t *testing.T) {
		mockSigner, _ := newAttestSigner(t)
		gomock.InOrder(
			// Prepared in advance
			mockSigner.EXPECT().BuildAttestTransaction(&blockHash).Return(attestTxn(), nil),
			mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil),
			// 1st block of the window: signing the final transaction fails
			mockSigner.EXPECT().Nonce().Return(new(felt.Felt).SetUint64(1), nil),
			mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(feeEstimate(), nil),
			mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, signerDown),
			// 2nd block of the window: the rebuild fails
			mockSigner.EXPECT().BuildAttestTransaction(&blockHash).Return(attestTxn(), nil),
			mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, signerDown),
			// 3rd block of the window: the signer is back
			mockSigner.EXPECT().BuildAttestTransaction(&blockHash).Return(attestTxn(), nil),
			mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil),
			mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(feeEstimate(), nil),
			mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil),
			mockSigner.EXPECT().
				InvokeTransaction(gomock.Any()).
				Return(rpc.AddInvokeTransactionResponse{Hash: txHash}, nil),
		)
		tracer := new(attestTracer)
		dispatcher, stop := startDispatcher(t, mockSigner, tracer)

		sendEvent(t, dispatcher.PrepareAttest, types.PrepareAttest{BlockHash: blockHash})
		for range 3 {
			sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
		}
		stop()

		require.Equal(t, validator.Ongoing, dispatcher.CurrentAttest.Status)
		require.Equal(t, *txHash, dispatcher.CurrentAttest.Hash)
		require.Equal(t, 1, tracer.submitted)
	})

	t.Run("Re-signing after a nonce change fails", func(t *testing.T) {
		mockSigner, _ := newAttestSigner(t)
		gomock.InOrder(
			// Prepared in advance
			mockSigner.EXPECT().BuildAttestTransaction(&blockHash).Return(attestTxn(), nil),
			mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil),
			// 1st block of the window: the nonce changed, but re-signing fails
			mockSigner.EXPECT().Nonce().Return(new(felt.Felt).SetUint64(2), nil),
			mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, signerDown),
			// 2nd block of the window: the transaction is rebuilt, since its signature no
			// longer matches its nonce
			mockSigner.EXPECT().BuildAttestTransaction(&blockHash).Return(attestTxn(), nil),
			mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil),
			mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(feeEstimate(), nil),
			mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil),
			mockSigner.EXPECT().
				InvokeTransaction(gomock.Any()).
				Return(rpc.AddInvokeTransactionResponse{Hash: txHash}, nil),
		)
		dispatcher, stop := startDispatcher(t, mockSigner, new(attestTracer))

		sendEvent(t, dispatcher.PrepareAttest, types.PrepareAttest{BlockHash: blockHash})
		sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
		sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
		stop()

		require.Equal(t, validator.Ongoing, dispatcher.CurrentAttest.Status)
		require.Equal(t, *txHash, dispatcher.CurrentAttest.Hash)
	})

	// QA report 01, finding OBS-5: with nothing sent there is no transaction status to poll
	t.Run("Window ends after failed attempts only", func(t *testing.T) {
		mockSigner, balanceChecked := newAttestSigner(t)
		mockSigner.EXPECT().
			BuildAttestTransaction(&blockHash).
			Return(rpc.BroadcastInvokeTxnV3{}, errors.New("-32603 Internal Error")).
			Times(2)
		mockSigner.EXPECT().TransactionStatus(gomock.Any()).Times(0)
		tracer := new(attestTracer)
		dispatcher, stop := startDispatcher(t, mockSigner, tracer)

		sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
		sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
		sendEvent(t, dispatcher.EndOfWindow, struct{}{})
		stop()
		waitBalanceCheck(t, balanceChecked)

		require.Equal(t, 0, tracer.submitted)
		require.Equal(t, 1, tracer.failures)
		require.Equal(t, validator.Iddle, dispatcher.CurrentAttest.Status)
	})
}

func TestTrackAttestWithoutTransaction(t *testing.T) {
	mockSigner := mocks.NewMockSigner(gomock.NewController(t))
	mockSigner.EXPECT().TransactionStatus(gomock.Any()).Times(0)

	status := validator.TrackAttest(mockSigner, log.NewNopZapLogger(), &felt.Zero)

	require.Equal(t, validator.Failed, status)
}

// QA report 01, finding OBS-1: the status of a sent transaction can be unknown, because the
// node does not find it or the status request fails. The hash is kept and polled again; after
// some blocks the attestation is sent again; at the end of the window an unknown status is not
// counted as a failure.
func TestDispatchUnknownAttestStatus(t *testing.T) {
	blockHash := types.BlockHash(*new(felt.Felt).SetUint64(0xb10c))
	txHash := new(felt.Felt).SetUint64(0xa77e57)
	nodeDown := errors.New("-32603 The error is not a valid RPC error: connection refused")
	succeeded := &rpc.TxnStatusResult{
		FinalityStatus:  rpc.TxnStatusAcceptedOnL2,
		ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
	}

	// Expects the attest transaction to be built and sent once, at the first block
	expectSend := func(mockSigner *mocks.MockSigner) *gomock.Call {
		return mockSigner.EXPECT().
			InvokeTransaction(gomock.Any()).
			After(mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil).Times(2)).
			After(mockSigner.EXPECT().EstimateFee(gomock.Any()).Return(feeEstimate(), nil)).
			After(mockSigner.EXPECT().BuildAttestTransaction(&blockHash).Return(attestTxn(), nil)).
			Return(rpc.AddInvokeTransactionResponse{Hash: txHash}, nil)
	}

	t.Run("Failed status requests keep the transaction", func(t *testing.T) {
		mockSigner, balanceChecked := newAttestSigner(t)
		sent := expectSend(mockSigner)
		gomock.InOrder(
			sent,
			mockSigner.EXPECT().TransactionStatus(txHash).Return(nil, nodeDown).Times(2),
			mockSigner.EXPECT().TransactionStatus(txHash).Return(succeeded, nil),
		)
		tracer := new(attestTracer)
		dispatcher, stop := startDispatcher(t, mockSigner, tracer)

		for range 4 {
			sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
		}
		sendEvent(t, dispatcher.EndOfWindow, struct{}{})
		stop()
		waitBalanceCheck(t, balanceChecked)

		require.Equal(t, 1, tracer.submitted)
		require.Equal(t, 1, tracer.confirmed)
		require.Equal(t, 0, tracer.failures)
	})

	t.Run("Transaction not found for too long is sent again", func(t *testing.T) {
		mockSigner, _ := newAttestSigner(t)
		sent := expectSend(mockSigner)
		gomock.InOrder(
			sent,
			mockSigner.EXPECT().
				TransactionStatus(txHash).
				Return(nil, validator.ErrTxnHashNotFound).
				Times(5),
			// The first transaction was executed after all: the new one is not sent
			mockSigner.EXPECT().BuildAttestTransaction(&blockHash).Return(attestTxn(), nil),
			mockSigner.EXPECT().SignTransaction(gomock.Any()).Return(nil, nil),
			mockSigner.EXPECT().
				EstimateFee(gomock.Any()).
				Return(rpc.FeeEstimation{}, errors.New("Attestation is done for this epoch")),
		)
		dispatcher, stop := startDispatcher(t, mockSigner, new(attestTracer))

		for range 7 {
			sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
		}
		stop()

		require.Equal(t, validator.Successful, dispatcher.CurrentAttest.Status)
	})

	t.Run("Unknown status at the end of the window", func(t *testing.T) {
		mockSigner, balanceChecked := newAttestSigner(t)
		sent := expectSend(mockSigner)
		gomock.InOrder(
			sent,
			mockSigner.EXPECT().TransactionStatus(txHash).Return(nil, validator.ErrTxnHashNotFound),
		)
		tracer := new(attestTracer)
		dispatcher, stop := startDispatcher(t, mockSigner, tracer)

		sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
		sendEvent(t, dispatcher.EndOfWindow, struct{}{})
		stop()
		waitBalanceCheck(t, balanceChecked)

		require.Equal(t, 1, tracer.submitted)
		require.Equal(t, 0, tracer.confirmed)
		require.Equal(t, 0, tracer.failures)
	})

	t.Run("Transaction still in processing at the end of the window", func(t *testing.T) {
		mockSigner, balanceChecked := newAttestSigner(t)
		sent := expectSend(mockSigner)
		gomock.InOrder(
			sent,
			mockSigner.EXPECT().
				TransactionStatus(txHash).
				Return(&rpc.TxnStatusResult{FinalityStatus: rpc.TxnStatusReceived}, nil),
		)
		tracer := new(attestTracer)
		dispatcher, stop := startDispatcher(t, mockSigner, tracer)

		sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
		sendEvent(t, dispatcher.EndOfWindow, struct{}{})
		stop()
		waitBalanceCheck(t, balanceChecked)

		require.Equal(t, 1, tracer.failures)
	})
}
