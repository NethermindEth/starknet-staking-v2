package main_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	main "github.com/NethermindEth/starknet-staking-v2"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/sourcegraph/conc"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestDispatch(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)

	t.Run("Simple scenario: only 1 attest that succeeds", func(t *testing.T) {
		// Setup
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter]()
		blockHashFelt := new(felt.Felt).SetUint64(1)

		contractAddrFelt := main.AttestContract.ToFelt()
		calls := []rpc.InvokeFunctionCall{{
			ContractAddress: &contractAddrFelt,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFelt},
		}}
		addTxHash := utils.HexToFelt(t, "0x123")
		mockedAddTxResp := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash}
		mockAccount.EXPECT().BuildAndSendInvokeTxn(context.Background(), calls, main.FEE_ESTIMATION_MULTIPLIER).Return(&mockedAddTxResp, nil)

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
			}, nil)

		// Start routine
		wg := &conc.WaitGroup{}
		currentAttest := main.AttestRequired{}
		currentAttestStatus := main.Failed
		wg.Go(func() { dispatcher.Dispatch(mockAccount, &currentAttest, &currentAttestStatus) })

		// Send event
		blockHash := main.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}
		close(dispatcher.AttestRequired)

		// Wait for dispatch routine (and consequently its spawned subroutines) to finish
		wg.Wait()

		// Assert
		require.Equal(t, blockHash, currentAttest.BlockHash)
		require.Equal(t, main.Successful, currentAttestStatus)
	})

	t.Run("Same AttestRequired events are ignored if already ongoing or successful", func(t *testing.T) {
		// Sequence of actions:
		// - an AttestRequired event A is emitted and processed
		// - an AttestRequired event A is emitted and ignored (as 1st one is getting processed)
		// - an AttestRequired event A is emitted and ignored (as 1st one finished & succeeded)

		// Setup
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter]()
		blockHashFelt := new(felt.Felt).SetUint64(1)

		contractAddrFelt := main.AttestContract.ToFelt()
		calls := []rpc.InvokeFunctionCall{{
			ContractAddress: &contractAddrFelt,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFelt},
		}}
		addTxHash := utils.HexToFelt(t, "0x123")
		mockedAddTxResp := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash}
		// We expect BuildAndSendInvokeTxn to be called only once (even though 3 events are sent)
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), calls, main.FEE_ESTIMATION_MULTIPLIER).
			DoAndReturn(func(ctx context.Context, calls []rpc.InvokeFunctionCall, multiplier float64) (*rpc.AddInvokeTransactionResponse, error) {
				// The Dispatch routine will sleep 1 second so that we can assert ongoing status (see below)
				time.Sleep(time.Second * 1)
				return &mockedAddTxResp, nil
			}).Times(1)

		// We expect GetTransactionStatus to be called only once (even though 3 events are sent)
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
			}, nil).
			Times(1)

		// Start routine
		currentAttest := main.AttestRequired{}
		currentAttestStatus := main.Failed
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, &currentAttest, &currentAttestStatus) })

		// Send the same event x3
		blockHash := main.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}

		// Sleep just a bit so that dispatch routine has time to set the status as ongoing
		time.Sleep(time.Second / 10)

		// Mid-execution assertion: attestation is ongoing (dispatch go routine has not finished executing as it sleeps for 1 sec)
		require.Equal(t, blockHash, currentAttest.BlockHash)
		require.Equal(t, main.Ongoing, currentAttestStatus)

		// This 2nd event gets ignored when status is ongoing
		// Proof: only 1 call to BuildAndSendInvokeTxn and GetTransactionStatus is asserted
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}

		// This time sleep is more than enough to make sure spawned trackAttest routine has time to execute (2nd event got ignored)
		time.Sleep(time.Second * 2)

		// Mid-execution assertion: attestation is successful (1st go routine has indeed finished executing)
		require.Equal(t, blockHash, currentAttest.BlockHash)
		require.Equal(t, main.Successful, currentAttestStatus)

		// This 3rd event gets ignored also when status is successful
		// Proof: only 1 call to BuildAndSendInvokeTxn and GetTransactionStatus is asserted
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}
		close(dispatcher.AttestRequired)

		// Wait for dispatch routine (and consequently its spawned subroutines) to finish
		wg.Wait()

		// Re-assert (3rd event got ignored)
		require.Equal(t, blockHash, currentAttest.BlockHash)
		require.Equal(t, main.Successful, currentAttestStatus)
	})

	t.Run("Same AttestRequired events are ignored until attestation fails", func(t *testing.T) {
		// Sequence of actions:
		// - an AttestRequired event A is emitted and processed
		// - an AttestRequired event A is emitted and ignored (as 1st one is getting processed)
		// - an AttestRequired event is considered (as 2nd one finished & failed)

		// Setup
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter]()
		blockHashFelt := new(felt.Felt).SetUint64(1)

		contractAddrFelt := main.AttestContract.ToFelt()
		calls := []rpc.InvokeFunctionCall{{
			ContractAddress: &contractAddrFelt,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFelt},
		}}
		addTxHash := utils.HexToFelt(t, "0x123")
		mockedAddTxResp := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash}

		// We expect BuildAndSendInvokeTxn to be called only once (for the 2 first events)
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), calls, main.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxResp, nil).
			Times(1)

		// We expect GetTransactionStatus to be called only once (for the 2 first events)
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash).
			DoAndReturn(func(ctx context.Context, hash *felt.Felt) (*rpc.TxnStatusResp, error) {
				// The spawned routine (created by Dispatch) will sleep 1 second so that we can assert ongoing status
				time.Sleep(time.Second * 1)
				return &rpc.TxnStatusResp{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusREVERTED,
				}, nil
			}).
			Times(1)

		// Start routine
		currentAttest := main.AttestRequired{}
		currentAttestStatus := main.Failed
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, &currentAttest, &currentAttestStatus) })

		// Send the same event x3
		blockHash := main.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}

		// Sleep just a bit so that dispatch routine has time to set the status as ongoing
		time.Sleep(time.Second / 10)

		// Mid-execution assertion: attestation is ongoing (1st go routine has not finished executing as it sleeps for 1 sec)
		require.Equal(t, blockHash, currentAttest.BlockHash)
		require.Equal(t, main.Ongoing, currentAttestStatus)

		// This 2nd event gets ignored when status is ongoing
		// Proof: only 1 call to BuildAndSendInvokeTxn and GetTransactionStatus is asserted so far
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}

		// This time sleep is more than enough to make sure spawned trackAttest routine has time to execute (2nd event got ignored)
		time.Sleep(time.Second * 2)

		// Mid-execution assertion: attestation has failed (1st go routine has indeed finished executing)
		require.Equal(t, blockHash, currentAttest.BlockHash)
		require.Equal(t, main.Failed, currentAttestStatus)

		// Preparation for 3rd event

		// We expect a 2nd call to BuildAndSendInvokeTxn
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), calls, main.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxResp, nil).
			Times(1)

		// We expect a 2nd call to GetTransactionStatus
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
			}, nil).
			Times(1)

		// This 3rd event does not get ignored as previous attestation has failed
		// Proof: a 2nd call to BuildAndSendInvokeTxn and GetTransactionStatus is asserted
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}
		close(dispatcher.AttestRequired)

		// Wait for dispatch routine (and consequently its spawned subroutines) to finish
		wg.Wait()

		// Re-assert (3rd event got ignored)
		require.Equal(t, blockHash, currentAttest.BlockHash)
		require.Equal(t, main.Successful, currentAttestStatus)
	})

	t.Run("attest marked as failed if invoke tx fails, next same AttestRequired event is considered", func(t *testing.T) {
		// Sequence of actions:
		// - an AttestRequired event A is emitted and processed (invoke tx, not trackAttest, fails)
		// - an AttestRequired event A is emitted and considered (as 1st one failed)

		// Setup
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter]()
		blockHashFelt := new(felt.Felt).SetUint64(1)

		contractAddrFelt := main.AttestContract.ToFelt()
		calls := []rpc.InvokeFunctionCall{{
			ContractAddress: &contractAddrFelt,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFelt},
		}}
		addTxHash := utils.HexToFelt(t, "0x123")
		mockedAddTxResp := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash}

		// We expect BuildAndSendInvokeTxn to fail once
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), calls, main.FEE_ESTIMATION_MULTIPLIER).
			Return(nil, errors.New("invoke tx failed for some reason")).
			Times(1)

		// Next call to BuildAndSendInvokeTxn to then succeed
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), calls, main.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxResp, nil).
			Times(1)

		// We expect GetTransactionStatus to be called only once
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash).
			DoAndReturn(func(ctx context.Context, hash *felt.Felt) (*rpc.TxnStatusResp, error) {
				// The spawned routine (created by Dispatch) will sleep 1 second so that we can assert ongoing status
				time.Sleep(time.Second * 1)
				return &rpc.TxnStatusResp{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
				}, nil
			}).
			Times(1)

		// Start routine
		currentAttest := main.AttestRequired{}
		currentAttestStatus := main.Failed
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, &currentAttest, &currentAttestStatus) })

		// Send the same event x2
		blockHash := main.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}

		// Sleep just a bit so that dispatch routine has time to execute invoke tx (which fails)
		time.Sleep(time.Second / 5)

		// Mid-execution assertion: attestation has failed
		require.Equal(t, blockHash, currentAttest.BlockHash)
		require.Equal(t, main.Failed, currentAttestStatus)

		// This 2nd event gets considered as previous one failed
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}

		close(dispatcher.AttestRequired)
		// Wait for dispatch routine (and consequently its spawned subroutines) to finish
		wg.Wait()

		// Mid-execution assertion: attestation has failed (1st go routine has indeed finished executing)
		require.Equal(t, blockHash, currentAttest.BlockHash)
		require.Equal(t, main.Successful, currentAttestStatus)
	})

	t.Run("AttestRequired events transition", func(t *testing.T) {
		// Sequence of actions:
		// - an AttestRequired event A is emitted and processed (successful)
		// - an AttestRequired event B is emitted and processed (successful)

		// Setup
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter]()

		// For event A
		blockHashFeltA := new(felt.Felt).SetUint64(1)
		contractAddrFelt := main.AttestContract.ToFelt()
		callsA := []rpc.InvokeFunctionCall{{
			ContractAddress: &contractAddrFelt,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFeltA},
		}}
		addTxHashA := utils.HexToFelt(t, "0x123")
		mockedAddTxRespA := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHashA}

		// We expect BuildAndSendInvokeTxn to be called once for event A
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), callsA, main.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxRespA, nil).
			Times(1)

		// We expect GetTransactionStatus to be called for event A
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
			ContractAddress: &contractAddrFelt,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFeltB},
		}}
		addTxHashB := utils.HexToFelt(t, "0x456")
		mockedAddTxRespB := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHashB}

		// We expect BuildAndSendInvokeTxn to be called once for event B
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), callsB, main.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxRespB, nil).
			Times(1)

		// We expect GetTransactionStatus to be called once for event B
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHashB).
			DoAndReturn(func(ctx context.Context, hash *felt.Felt) (*rpc.TxnStatusResp, error) {
				// The spawned routine (created by Dispatch) will sleep 1 second so that we can assert ongoing status
				time.Sleep(time.Second * 1)

				return &rpc.TxnStatusResp{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
				}, nil
			}).
			Times(1)

		// Start routine
		currentAttest := main.AttestRequired{}
		currentAttestStatus := main.Failed
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, &currentAttest, &currentAttestStatus) })

		// Send event A
		blockHashA := main.BlockHash(*blockHashFeltA)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHashA}

		// To give time for the spawned routine to execute
		time.Sleep(time.Second / 5)

		// Mid-execution assertion: attestation A is successful
		require.Equal(t, blockHashA, currentAttest.BlockHash)
		require.Equal(t, main.Successful, currentAttestStatus)

		// Send event B
		blockHashB := main.BlockHash(*blockHashFeltB)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHashB}

		// Sleep just a bit so that dispatch routine has time to set the status as ongoing
		time.Sleep(time.Second / 10)

		// Mid-execution assertion: attestation B has failed
		require.Equal(t, blockHashB, currentAttest.BlockHash)
		require.Equal(t, main.Ongoing, currentAttestStatus)

		close(dispatcher.AttestRequired)
		// Wait for dispatch routine (and consequently its spawned subroutine B) to finish
		wg.Wait()

		// End of execution assertion: attestation B was successful
		require.Equal(t, blockHashB, currentAttest.BlockHash)
		require.Equal(t, main.Successful, currentAttestStatus)
	})
}

func TestTrackAttest(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)

	t.Run("Status gets set if block hash entry exists", func(t *testing.T) {
		t.Run("attestation fails if error", func(t *testing.T) {
			txHash := new(felt.Felt).SetUint64(1)

			mockAccount.EXPECT().
				GetTransactionStatus(context.Background(), txHash).
				Return(nil, errors.New("some internal error"))

			txRes := &rpc.AddInvokeTransactionResponse{TransactionHash: txHash}

			txStatus := main.TrackAttest(mockAccount, txRes)

			require.Equal(t, main.Failed, txStatus)
		})

		t.Run("attestation fails if REJECTED", func(t *testing.T) {
			txHash := new(felt.Felt).SetUint64(1)

			mockAccount.EXPECT().
				GetTransactionStatus(context.Background(), txHash).
				Return(&rpc.TxnStatusResp{
					FinalityStatus: rpc.TxnStatus_Rejected,
				}, nil)

			txRes := &rpc.AddInvokeTransactionResponse{TransactionHash: txHash}

			txStatus := main.TrackAttest(mockAccount, txRes)

			require.Equal(t, main.Failed, txStatus)
		})

		t.Run("attestation fails if accepted but REVERTED", func(t *testing.T) {
			txHash := new(felt.Felt).SetUint64(1)

			mockAccount.EXPECT().
				GetTransactionStatus(context.Background(), txHash).
				Return(&rpc.TxnStatusResp{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusREVERTED,
				}, nil)

			txRes := &rpc.AddInvokeTransactionResponse{TransactionHash: txHash}

			txStatus := main.TrackAttest(mockAccount, txRes)

			require.Equal(t, main.Failed, txStatus)
		})

		t.Run("attestation succeeds if accepted & SUCCEEDED", func(t *testing.T) {
			txHash := new(felt.Felt).SetUint64(1)

			mockAccount.EXPECT().
				GetTransactionStatus(context.Background(), txHash).
				Return(&rpc.TxnStatusResp{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
				}, nil)

			txRes := &rpc.AddInvokeTransactionResponse{TransactionHash: txHash}

			txStatus := main.TrackAttest(mockAccount, txRes)

			require.Equal(t, main.Successful, txStatus)
		})
	})
}

func TestTrackTransactionStatus(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)

	t.Run("GetTransactionStatus returns an error different from tx hash not found", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		// Set expectations
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(nil, errors.New("some internal error"))

		status, err := main.TrackTransactionStatus(mockAccount, txHash)

		require.Nil(t, status)
		require.Equal(t, errors.New("some internal error"), err)
	})

	t.Run("Returning a tx hash not found error triggers a retry", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)
		// Mock time.Sleep (no reason to wait in that test)
		main.Sleep = func(d time.Duration) {
			// Do nothing
		}

		// Set expectations
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(nil, errors.New("Transaction hash not found"))

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus: rpc.TxnStatus_Accepted_On_L2,
			}, nil)

		status, err := main.TrackTransactionStatus(mockAccount, txHash)

		require.Equal(t, &rpc.TxnStatusResp{
			FinalityStatus: rpc.TxnStatus_Accepted_On_L2,
		}, status)
		require.Nil(t, err)

		// Reset time.Sleep function
		main.Sleep = time.Sleep
	})

	t.Run("Returns an error if tx status does not change for `defaultAttestDelay` seconds", func(t *testing.T) {
		// Mock time.Sleep (absolutely no reason to wait in that test)
		main.Sleep = func(d time.Duration) {
			// Do nothing
		}

		txHash := new(felt.Felt).SetUint64(1)

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus: rpc.TxnStatus_Received,
			}, nil).
			// equal to `defaultAttestDelay`
			Times(10)

		status, err := main.TrackTransactionStatus(mockAccount, txHash)

		require.Nil(t, status)
		require.Equal(t, errors.New("Tx status did not change for a long time, retrying with next block"), err)

		// Reset time.Sleep function
		main.Sleep = time.Sleep
	})

	t.Run("Returns the status if different from RECEIVED, here ACCEPTED_ON_L2", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus: rpc.TxnStatus_Accepted_On_L2,
			}, nil).
			Times(1)

		status, err := main.TrackTransactionStatus(mockAccount, txHash)

		require.Equal(t, &rpc.TxnStatusResp{
			FinalityStatus: rpc.TxnStatus_Accepted_On_L2,
		}, status)
		require.Nil(t, err)
	})

	t.Run("Returns the status if different from RECEIVED, here ACCEPTED_ON_L1", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus: rpc.TxnStatus_Accepted_On_L1,
			}, nil).
			Times(1)

		status, err := main.TrackTransactionStatus(mockAccount, txHash)

		require.Equal(t, &rpc.TxnStatusResp{
			FinalityStatus: rpc.TxnStatus_Accepted_On_L1,
		}, status)
		require.Nil(t, err)
	})
}
