package validator_test

import (
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	/*
		"math"
		"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
		"github.com/NethermindEth/starknet-staking-v2/validator/types"
		"github.com/sourcegraph/conc"
	*/)

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
			wg.Go(func() { dispatcher.Dispatch(mockSigner, math.Inf(1), logger, tracer) })

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
	logger := utils.NewNopZapLogger()

	t.Run("attestation fails if error is transaction status not found", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		mockSigner.EXPECT().
			GetTransactionStatus(txHash).
			Return(nil, validator.ErrTxnHashNotFound)

		txStatus := validator.TrackAttest(mockSigner, logger, txHash)

		require.Equal(t, validator.Ongoing, txStatus)
	})

	t.Run(
		"attestation fails also if error different from transaction status not found",
		func(t *testing.T) {
			txHash := new(felt.Felt).SetUint64(1)

			mockSigner.EXPECT().
				GetTransactionStatus(txHash).
				Return(nil, errors.New("some internal error"))

			txStatus := validator.TrackAttest(mockSigner, logger, txHash)

			require.Equal(t, validator.Failed, txStatus)
		})

	t.Run("attestation fails if REJECTED", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		mockSigner.EXPECT().
			GetTransactionStatus(txHash).
			Return(&rpc.TxnStatusResult{
				FinalityStatus: rpc.TxnStatus_Rejected,
			}, nil)

		txStatus := validator.TrackAttest(mockSigner, logger, txHash)

		require.Equal(t, validator.Failed, txStatus)
	})

	t.Run("attestation fails if accepted but REVERTED", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		revertError := "reverted for some reason"
		mockSigner.EXPECT().
			GetTransactionStatus(txHash).
			Return(&rpc.TxnStatusResult{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusREVERTED,
				FailureReason:   revertError,
			}, nil)

		txStatus := validator.TrackAttest(mockSigner, logger, txHash)

		require.Equal(t, validator.Failed, txStatus)
	})

	t.Run("attestation succeeds if accepted & SUCCEEDED", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		mockSigner.EXPECT().
			GetTransactionStatus(txHash).
			Return(&rpc.TxnStatusResult{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
			}, nil)

		txStatus := validator.TrackAttest(mockSigner, logger, txHash)

		require.Equal(t, validator.Successful, txStatus)
	})
}
