package validator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/sourcegraph/conc"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestDispatch(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)
	logger := utils.NewNopZapLogger()

	t.Run("Simple scenario: only 1 attest that succeeds", func(t *testing.T) {
		// Setup
		dispatcher := validator.NewEventDispatcher[*mocks.MockAccounter, *utils.ZapLogger]()
		blockHashFelt := new(felt.Felt).SetUint64(1)

		contractAddr := validator.AttestContract.Felt()
		calls := []rpc.InvokeFunctionCall{{
			ContractAddress: contractAddr,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFelt},
		}}
		addTxHash := utils.HexToFelt(t, "0x123")
		mockedAddTxResp := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash}
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), calls, validator.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxResp, nil)

		// Start routine
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, logger) })

		// Send event
		blockHash := validator.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

		// Preparation for EndOfWindow event
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
			}, nil)

		// Send EndOfWindow
		dispatcher.EndOfWindow <- struct{}{}

		close(dispatcher.AttestRequired)
		// Wait for dispatch routine to finish
		wg.Wait()

		// Assert
		expectedAttest := validator.AttestTracker{
			Event:           validator.AttestRequired{BlockHash: blockHash},
			TransactionHash: *addTxHash,
			Status:          validator.Successful,
		}
		require.Equal(t, expectedAttest, dispatcher.CurrentAttest)
	})

	t.Run(
		"Same AttestRequired events are ignored if already ongoing or successful",
		func(t *testing.T) {
			// Sequence of actions:
			// - an AttestRequired event A is emitted and processed
			// - an AttestRequired event A is emitted and ignored (as 1st one is getting processed)
			// - an AttestRequired event A is emitted and ignored (as 1st one finished & succeeded)

			// Setup
			dispatcher := validator.NewEventDispatcher[*mocks.MockAccounter, *utils.ZapLogger]()
			blockHashFelt := new(felt.Felt).SetUint64(1)

			contractAddr := validator.AttestContract.Felt()
			calls := []rpc.InvokeFunctionCall{{
				ContractAddress: contractAddr,
				FunctionName:    "attest",
				CallData:        []*felt.Felt{blockHashFelt},
			}}
			addTxHash := utils.HexToFelt(t, "0x123")
			mockedAddTxResp := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash}
			// We expect BuildAndSendInvokeTxn to be called only once (even though 3 events are sent)
			mockAccount.EXPECT().
				BuildAndSendInvokeTxn(
					context.Background(), calls, validator.FEE_ESTIMATION_MULTIPLIER,
				).
				Return(&mockedAddTxResp, nil).
				Times(1)

			// Start routine
			wg := &conc.WaitGroup{}
			wg.Go(func() { dispatcher.Dispatch(mockAccount, logger) })

			// Send the same event x3
			blockHash := validator.BlockHash(*blockHashFelt)
			dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

			// Preparation for 2nd event

			// Invoke tx is RECEIVED
			mockAccount.EXPECT().
				GetTransactionStatus(context.Background(), addTxHash).
				Return(&rpc.TxnStatusResp{
					FinalityStatus: rpc.TxnStatus_Received,
				}, nil).
				Times(1)

			// This 2nd event gets ignored when status is ongoing
			// Proof: only 1 call to BuildAndSendInvokeTxn is asserted
			dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

			// Preparation for 3rd event

			// Invoke tx ended up ACCEPTED
			mockAccount.EXPECT().
				GetTransactionStatus(context.Background(), addTxHash).
				Return(&rpc.TxnStatusResp{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
				}, nil).
				Times(1)

			// This 3rd event gets ignored also when status is successful
			// Proof: only 1 call to BuildAndSendInvokeTxn is asserted
			dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}
			close(dispatcher.AttestRequired)

			// Wait for dispatch routine (and consequently its spawned subroutines) to finish
			wg.Wait()

			// Re-assert (3rd event got ignored)
			expectedAttest := validator.AttestTracker{
				Event:           validator.AttestRequired{BlockHash: blockHash},
				TransactionHash: *addTxHash,
				Status:          validator.Successful,
			}
			require.Equal(t, expectedAttest, dispatcher.CurrentAttest)
		})

	t.Run("Same AttestRequired events are ignored until attestation fails", func(t *testing.T) {
		// Sequence of actions:
		// - an AttestRequired event A is emitted and processed
		// - an AttestRequired event A is emitted and ignored (as 1st one is getting processed)
		// - an AttestRequired event A is emitted and processed (as 1st one finished & failed)

		// Setup
		dispatcher := validator.NewEventDispatcher[*mocks.MockAccounter, *utils.ZapLogger]()
		blockHashFelt := new(felt.Felt).SetUint64(1)

		contractAddr := validator.AttestContract.Felt()
		calls := []rpc.InvokeFunctionCall{{
			ContractAddress: contractAddr,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFelt},
		}}
		addTxHash1 := utils.HexToFelt(t, "0x123")
		mockedAddTxResp1 := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash1}

		// We expect BuildAndSendInvokeTxn to be called only once (for the 2 first events)
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), calls, validator.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxResp1, nil).
			Times(1)

		// Start routine
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, logger) })

		// Send the same event x3
		blockHash := validator.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

		// Preparation for 2nd event

		// Invoke tx status is RECEIVED
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash1).
			Return(&rpc.TxnStatusResp{
				FinalityStatus: rpc.TxnStatus_Received,
			}, nil).
			Times(1)

		// This 2nd event gets ignored when status is ongoing
		// Proof: only 1 call to BuildAndSendInvokeTxn is asserted so far
		dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

		// Preparation for 3rd event

		// Invoke tx fails, will make a new invoke tx
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash1).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusREVERTED,
				FailureReason:   "some failure reason",
			}, nil).
			Times(1)

		addTxHash2 := utils.HexToFelt(t, "0x456")
		mockedAddTxResp2 := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash2}

		// We expect a 2nd call to BuildAndSendInvokeTxn
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), calls, validator.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxResp2, nil).
			Times(1)

		// This 3rd event does not get ignored as invoke attestation has failed
		// Proof: a 2nd call to BuildAndSendInvokeTxn is asserted
		dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}
		close(dispatcher.AttestRequired)

		// Wait for dispatch routine (and consequently its spawned subroutines) to finish
		wg.Wait()

		// Assert after dispatcher routine has finished processing the 3rd event
		expectedAttest := validator.AttestTracker{
			Event:           validator.AttestRequired{BlockHash: blockHash},
			TransactionHash: *addTxHash2,
			Status:          validator.Ongoing,
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
			dispatcher := validator.NewEventDispatcher[*mocks.MockAccounter, *utils.ZapLogger]()
			blockHashFelt := new(felt.Felt).SetUint64(1)

			contractAddr := validator.AttestContract.Felt()
			calls := []rpc.InvokeFunctionCall{{
				ContractAddress: contractAddr,
				FunctionName:    "attest",
				CallData:        []*felt.Felt{blockHashFelt},
			}}

			// We expect BuildAndSendInvokeTxn to fail once
			mockAccount.EXPECT().
				BuildAndSendInvokeTxn(
					context.Background(), calls, validator.FEE_ESTIMATION_MULTIPLIER,
				).
				Return(nil, errors.New("sending invoke tx failed for some reason")).
				Times(1)

			// Start routine
			wg := &conc.WaitGroup{}
			wg.Go(func() { dispatcher.Dispatch(mockAccount, logger) })

			// Send the same event x2
			blockHash := validator.BlockHash(*blockHashFelt)
			dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

			// Preparation for 2nd event

			// GetTransactionStatus does not get called as invoke tx failed during sending
			// Nothing to track

			// Next call to BuildAndSendInvokeTxn succeeds
			addTxHash := utils.HexToFelt(t, "0x123")
			mockedAddTxResp := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash}
			mockAccount.EXPECT().
				BuildAndSendInvokeTxn(
					context.Background(), calls, validator.FEE_ESTIMATION_MULTIPLIER,
				).
				Return(&mockedAddTxResp, nil).
				Times(1)

			// This 2nd event gets considered as previous one failed
			dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

			// Preparation for 3rd event

			// We expect GetTransactionStatus to be called only once
			mockAccount.EXPECT().
				GetTransactionStatus(context.Background(), addTxHash).
				Return(&rpc.TxnStatusResp{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
				}, nil).
				Times(1)

			dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

			close(dispatcher.AttestRequired)
			// Wait for dispatch routine (and consequently its spawned subroutines) to finish
			wg.Wait()

			// Assert after dispatcher routine has finished processing the 3rd event
			expectedAttest := validator.AttestTracker{
				Event:           validator.AttestRequired{BlockHash: blockHash},
				TransactionHash: *addTxHash,
				Status:          validator.Successful,
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
		dispatcher := validator.NewEventDispatcher[*mocks.MockAccounter, *utils.ZapLogger]()

		// For event A
		blockHashFeltA := new(felt.Felt).SetUint64(1)
		contractAddr := validator.AttestContract.Felt()
		callsA := []rpc.InvokeFunctionCall{{
			ContractAddress: contractAddr,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFeltA},
		}}
		addTxHashA := utils.HexToFelt(t, "0x123")
		mockedAddTxRespA := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHashA}

		// We expect BuildAndSendInvokeTxn to be called once for event A
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), callsA, validator.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxRespA, nil).
			Times(1)

		// We expect GetTransactionStatus to be called for event A (triggered by EndOfWindow)
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHashA).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
			}, nil).
			Times(1)

		// For event B
		blockHashFeltB := new(felt.Felt).SetUint64(2)
		callsB := []rpc.InvokeFunctionCall{{
			ContractAddress: contractAddr,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFeltB},
		}}
		addTxHashB := utils.HexToFelt(t, "0x456")
		mockedAddTxRespB := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHashB}

		// We expect BuildAndSendInvokeTxn to be called once for event B
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), callsB, validator.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxRespB, nil).
			Times(1)

		// We expect GetTransactionStatus to be called once for event B (triggered by EndOfWindow)
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHashB).
			Return(&rpc.TxnStatusResp{
				FinalityStatus: rpc.TxnStatus_Rejected,
			}, nil).
			Times(1)

		// Start routine
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, logger) })

		// Send event A
		blockHashA := validator.BlockHash(*blockHashFeltA)
		dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHashA}

		// Send EndOfWindow event for event A
		dispatcher.EndOfWindow <- struct{}{}

		// Send event B
		blockHashB := validator.BlockHash(*blockHashFeltB)
		dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHashB}

		// Send EndOfWindow event for event B
		dispatcher.EndOfWindow <- struct{}{}

		close(dispatcher.AttestRequired)
		// Wait for dispatch routine to finish executing
		wg.Wait()

		// End of execution assertion: attestation B has failed
		expectedAttest := validator.AttestTracker{
			Event:           validator.AttestRequired{BlockHash: blockHashB},
			TransactionHash: *addTxHashB,
			Status:          validator.Failed,
		}
		require.Equal(t, expectedAttest, dispatcher.CurrentAttest)
	})
}

func TestTrackAttest(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)
	logger := utils.NewNopZapLogger()

	t.Run("attestation fails if error is transaction status not found", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		blockHash := new(felt.Felt).SetUint64(1)
		attestEvent := validator.AttestRequired{BlockHash: validator.BlockHash(*blockHash)}

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(nil, validator.ErrTxnHashNotFound)

		txStatus := validator.TrackAttest(mockAccount, logger, &attestEvent, txHash)

		require.Equal(t, validator.Ongoing, txStatus)
	})

	t.Run("attestation fails also if error different from transaction status not found", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		blockHash := new(felt.Felt).SetUint64(1)
		attestEvent := validator.AttestRequired{BlockHash: validator.BlockHash(*blockHash)}

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(nil, errors.New("some internal error"))

		txStatus := validator.TrackAttest(mockAccount, logger, &attestEvent, txHash)

		require.Equal(t, validator.Failed, txStatus)
	})

	t.Run("attestation fails if REJECTED", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		blockHash := new(felt.Felt).SetUint64(1)
		attestEvent := validator.AttestRequired{BlockHash: validator.BlockHash(*blockHash)}

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus: rpc.TxnStatus_Rejected,
			}, nil)

		txStatus := validator.TrackAttest(mockAccount, logger, &attestEvent, txHash)

		require.Equal(t, validator.Failed, txStatus)
	})

	t.Run("attestation fails if accepted but REVERTED", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		blockHash := new(felt.Felt).SetUint64(1)
		attestEvent := validator.AttestRequired{BlockHash: validator.BlockHash(*blockHash)}

		revertError := "reverted for some reason"
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusREVERTED,
				FailureReason:   revertError,
			}, nil)

		txStatus := validator.TrackAttest(mockAccount, logger, &attestEvent, txHash)

		require.Equal(t, validator.Failed, txStatus)
	})

	t.Run("attestation succeeds if accepted & SUCCEEDED", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		blockHash := new(felt.Felt).SetUint64(1)
		attestEvent := validator.AttestRequired{BlockHash: validator.BlockHash(*blockHash)}

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
			}, nil)

		txStatus := validator.TrackAttest(mockAccount, logger, &attestEvent, txHash)

		require.Equal(t, validator.Successful, txStatus)
	})
}
