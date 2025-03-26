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
	"github.com/golang/mock/gomock"
	"github.com/sourcegraph/conc"
	"github.com/stretchr/testify/require"
)

func TestDispatch(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)

	t.Run("Simple successful scenario: only 1 attest to make", func(t *testing.T) {
		// Setup
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter]()
		blockHashFelt := new(felt.Felt).SetUint64(1)

		contractAddrFelt := main.AttestationContractAddress.ToFelt()
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
		activeAttestations := make(map[main.BlockHash]main.AttestationStatus)
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, activeAttestations, wg) })

		// Send event
		blockHash := main.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}
		close(dispatcher.AttestRequired)

		// Wait for routine (and subroutines) to finish
		// Note: Dispatch already waits inside for subroutines but still call it to wait here too
		wg.Wait()

		// Assert
		status, exists := activeAttestations[blockHash]
		require.Equal(t, true, exists)
		require.Equal(t, main.Successful, status)
	})

	t.Run("Same AttestRequired events are ignored if already ongoing or successful", func(t *testing.T) {
		// What this test tests:
		// - an AttestRequired event is ignored if an attest is already ongoing
		// - an AttestRequired event is ignored if an attest is already successful

		// Setup
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter]()
		blockHashFelt := new(felt.Felt).SetUint64(1)

		contractAddrFelt := main.AttestationContractAddress.ToFelt()
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
		activeAttestations := make(map[main.BlockHash]main.AttestationStatus)
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, activeAttestations, wg) })

		// Send the same event x3
		blockHash := main.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}

		// Mid-execution assertion: attestation is ongoing (1st go routine has not finished executing as it sleeps for 1 sec)
		//
		// This middle-exec assert might be a bit dangerous (could sleep here 0.1s maybe to be sure?)
		// will fail if thread here reaches the assert below before dispatcher main routine sets the status as ongoing
		status, exists := activeAttestations[blockHash]
		require.Equal(t, true, exists)
		require.Equal(t, main.Ongoing, status)

		// This 2nd event gets ignored when status is ongoing
		// Proof: only 1 call to BuildAndSendInvokeTxn and GetTransactionStatus is asserted
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}

		// This time sleep is more than enough to make sure the 1st go routine has time to execute (2nd event got ignored)
		time.Sleep(time.Second * 2)

		// Mid-execution assertion: attestation is successful (1st go routine has indeed finished executing)
		status, exists = activeAttestations[blockHash]
		require.Equal(t, true, exists)
		require.Equal(t, main.Successful, status)

		// This 3rd event gets ignored also when status is successful
		// Proof: only 1 call to BuildAndSendInvokeTxn and GetTransactionStatus is asserted
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}
		close(dispatcher.AttestRequired)

		// Wait for routine (and subroutines) to finish
		// Note: Dispatch already waits inside for subroutines but still call it to wait here too
		wg.Wait()

		// Re-assert (3rd event got ignored)
		status, exists = activeAttestations[blockHash]
		require.Equal(t, true, exists)
		require.Equal(t, main.Successful, status)
	})

	t.Run("Same AttestRequired events are ignored until attestation fails", func(t *testing.T) {
		// What this test tests:
		// - an AttestRequired event is ignored if an attest is already ongoing
		// - an AttestRequired event is considered if previous attestation has failed

		// Setup
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter]()
		blockHashFelt := new(felt.Felt).SetUint64(1)

		contractAddrFelt := main.AttestationContractAddress.ToFelt()
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
				// The spawned routine (created by Dispatch) will sleep 1 second so that we can assert failed status
				time.Sleep(time.Second * 1)
				return &rpc.TxnStatusResp{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusREVERTED,
				}, nil
			}).
			Times(1)

		// Start routine
		activeAttestations := make(map[main.BlockHash]main.AttestationStatus)
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, activeAttestations, wg) })

		// Send the same event x3
		blockHash := main.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}

		// Mid-execution assertion: attestation is ongoing (1st go routine has not finished executing as it sleeps for 1 sec)
		//
		// This middle-exec assert might be a bit dangerous (could sleep here 0.1s maybe to be sure?)
		// will fail if thread here reaches the assert below before dispatcher main routine sets the status as ongoing
		status, exists := activeAttestations[blockHash]
		require.Equal(t, true, exists)
		require.Equal(t, main.Ongoing, status)

		// This 2nd event gets ignored when status is ongoing
		// Proof: only 1 call to BuildAndSendInvokeTxn and GetTransactionStatus is asserted so far
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}

		// This time sleep is more than enough to make sure the 1st go routine has time to execute (2nd event got ignored)
		time.Sleep(time.Second * 2)

		// Mid-execution assertion: attestation has failed (1st go routine has indeed finished executing)
		status, exists = activeAttestations[blockHash]
		require.Equal(t, true, exists)
		require.Equal(t, main.Failed, status)

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

		// Wait for routine (and subroutines) to finish
		// Note: Dispatch already waits inside for subroutines but still call it to wait here too
		wg.Wait()

		// Re-assert (3rd event got ignored)
		status, exists = activeAttestations[blockHash]
		require.Equal(t, true, exists)
		require.Equal(t, main.Successful, status)
	})

	t.Run("Different AttestRequired events mixed with AttestationsToRemove events", func(t *testing.T) {
		t.Skip()
		// What this test tests:
		// - an AttestRequired event A is sent
		// - an AttestRequired event B is sent
		// - an AttestationsToRemove event containing A is sent

		// Setup
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter]()

		// For event A
		blockHashFeltA := new(felt.Felt).SetUint64(1)
		contractAddrFelt := main.AttestationContractAddress.ToFelt()
		callsA := []rpc.InvokeFunctionCall{{
			ContractAddress: &contractAddrFelt,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFeltA},
		}}
		addTxHashA := utils.HexToFelt(t, "0x123")
		mockedAddTxRespA := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHashA}

		// We expect BuildAndSendInvokeTxn to be called (event A)
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), callsA, main.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxRespA, nil).
			Times(1)

		// We expect GetTransactionStatus to be called (event A)
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

		// We expect BuildAndSendInvokeTxn to be called (event B)
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), callsB, main.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxRespB, nil).
			Times(1)

		// We expect GetTransactionStatus to be called (event B)
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHashB).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusREVERTED,
			}, nil).
			Times(1)

		// Start routine
		activeAttestations := make(map[main.BlockHash]main.AttestationStatus)
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, activeAttestations, wg) })

		// Send event A
		blockHashA := main.BlockHash(*blockHashFeltA)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHashA}

		// To give time for the spawned routine to execute
		time.Sleep(time.Second / 5)

		// Mid-execution assertion: attestation is successful
		status, exists := activeAttestations[blockHashA]
		require.Equal(t, true, exists)
		require.Equal(t, main.Successful, status)

		// Send event B
		blockHashB := main.BlockHash(*blockHashFeltB)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHashB}

		// To give time for the spawned routine to execute
		time.Sleep(time.Second / 5)

		// Mid-execution assertion: attestation has failed
		status, exists = activeAttestations[blockHashB]
		require.Equal(t, true, exists)
		require.Equal(t, main.Failed, status)

		// Send AttestationsToRemove event
		dispatcher.AttestationsToRemove <- []main.BlockHash{blockHashA, blockHashB}
		close(dispatcher.AttestRequired)

		// Wait for dispatch routine to finish
		// Note: Dispatch already waits inside for subroutines but still call it to wait here too
		wg.Wait()

		// Assert that blockHashA does not exist anymore in map (attestation was successful)
		_, exists = activeAttestations[blockHashA]
		require.Equal(t, false, exists)

		// Assert that blockHashB does not exist anymore in map (attestation failed)
		_, exists = activeAttestations[blockHashB]
		require.Equal(t, false, exists)
	})

	t.Run("AttestationsToRemove event indefinitely removes the block hash to attest to", func(t *testing.T) {
		// What this test tests:
		// - an AttestRequired event A is sent (takes some time)
		// - an AttestationsToRemove event containing A is sent (deleting A from the map)
		// - the AttestRequired event A finally finishes (successful/failed, whatever) and should not set the status in map (as it got deleted)

		// Setup
		dispatcher := main.NewEventDispatcher[*mocks.MockAccounter]()

		blockHashFelt := new(felt.Felt).SetUint64(1)
		contractAddrFelt := main.AttestationContractAddress.ToFelt()
		calls := []rpc.InvokeFunctionCall{{
			ContractAddress: &contractAddrFelt,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFelt},
		}}
		addTxHash := utils.HexToFelt(t, "0x123")
		mockedAddTxResp := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash}

		// We expect BuildAndSendInvokeTxn to be called
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), calls, main.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxResp, nil).
			Times(1)

		// We expect GetTransactionStatus to be called
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash).
			DoAndReturn(func(ctx context.Context, hash *felt.Felt) (*rpc.TxnStatusResp, error) {
				// Takes enough time for the event to get deleted
				time.Sleep(time.Second * 2)

				return &rpc.TxnStatusResp{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
				}, nil
			}).
			Times(1)

		// Start routine
		activeAttestations := make(map[main.BlockHash]main.AttestationStatus)
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, activeAttestations, wg) })

		// Send event
		blockHash := main.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: blockHash}

		// To give time for the disptach routine to set the status
		time.Sleep(time.Second / 5)

		// Mid-execution assertion: attestation is ongoing
		status, exists := activeAttestations[blockHash]
		require.Equal(t, true, exists)
		require.Equal(t, main.Ongoing, status)

		// Send AttestationsToRemove event
		dispatcher.AttestationsToRemove <- []main.BlockHash{blockHash}

		// To give time for the disptach routine to delete the entry in the map
		time.Sleep(time.Second / 5)

		// Assert that attest got deleted
		_, exists = activeAttestations[blockHash]
		require.Equal(t, false, exists)

		close(dispatcher.AttestRequired)
		// Wait for dispatch routine to finish
		// Note: Dispatch already waits inside for subroutines but still call it to wait here too
		wg.Wait()

		// Assert that spawned routine did not re-set the (successful) status in the map
		_, exists = activeAttestations[blockHash]
		require.Equal(t, false, exists)
	})
}

func TestTrackAttest(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)

	t.Run("attestation is not successful if error", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(nil, errors.New("some internal error"))

		blockHash := main.BlockHash(*txHash)
		event := main.AttestRequired{BlockHash: blockHash}
		txRes := &rpc.AddInvokeTransactionResponse{TransactionHash: txHash}
		activeAttestations := make(map[main.BlockHash]main.AttestationStatus)
		activeAttestations[blockHash] = main.Ongoing

		main.TrackAttest(mockAccount, event, txRes, activeAttestations)

		actualStatus, exists := activeAttestations[blockHash]
		require.Equal(t, main.Failed, actualStatus)
		require.Equal(t, true, exists)
	})

	t.Run("attestation is not successful if REJECTED", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus: rpc.TxnStatus_Rejected,
			}, nil)

		blockHash := main.BlockHash(*txHash)
		event := main.AttestRequired{BlockHash: blockHash}
		txRes := &rpc.AddInvokeTransactionResponse{TransactionHash: txHash}
		activeAttestations := make(map[main.BlockHash]main.AttestationStatus)
		activeAttestations[blockHash] = main.Ongoing

		main.TrackAttest(mockAccount, event, txRes, activeAttestations)

		actualStatus, exists := activeAttestations[blockHash]
		require.Equal(t, main.Failed, actualStatus)
		require.Equal(t, true, exists)
	})

	t.Run("attestation is not successful if accepted but REVERTED", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusREVERTED,
			}, nil)

		blockHash := main.BlockHash(*txHash)
		event := main.AttestRequired{BlockHash: blockHash}
		txRes := &rpc.AddInvokeTransactionResponse{TransactionHash: txHash}
		activeAttestations := make(map[main.BlockHash]main.AttestationStatus)
		activeAttestations[blockHash] = main.Ongoing

		main.TrackAttest(mockAccount, event, txRes, activeAttestations)

		actualStatus, exists := activeAttestations[blockHash]
		require.Equal(t, main.Failed, actualStatus)
		require.Equal(t, true, exists)
	})

	t.Run("attestation is not succesful if accepted & SUCCEEDED", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
			}, nil)

		blockHash := main.BlockHash(*txHash)
		event := main.AttestRequired{BlockHash: blockHash}
		txRes := &rpc.AddInvokeTransactionResponse{TransactionHash: txHash}
		activeAttestations := make(map[main.BlockHash]main.AttestationStatus)
		activeAttestations[blockHash] = main.Ongoing

		main.TrackAttest(mockAccount, event, txRes, activeAttestations)

		actualStatus, exists := activeAttestations[blockHash]
		require.Equal(t, main.Successful, actualStatus)
		require.Equal(t, true, exists)
	})
}

func TestTrackTransactionStatus(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)

	t.Run("GetTransactionStatus returns an error", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		// Set expectations
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(nil, errors.New("some internal error"))

		status, err := main.TrackTransactionStatus(mockAccount, txHash)

		require.Nil(t, status)
		require.Equal(t, errors.New("some internal error"), err)
	})

	t.Run("Returns an error if tx status does not change for `defaultAttestDelay` seconds", func(t *testing.T) {
		// Mock time.Sleep (absolutely no reason to wait in that test)
		main.SleepFn = func(d time.Duration) {
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
		main.SleepFn = time.Sleep
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
