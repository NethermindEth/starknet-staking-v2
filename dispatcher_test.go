package main_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	main "github.com/NethermindEth/starknet-staking-v2"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestDispatch(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccountInterface(mockCtrl)

	t.Run("Simple successful scenario: only 1 attest to make", func(t *testing.T) {
		// Setup
		dispatcher := main.NewEventDispatcher()
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
		wg := &sync.WaitGroup{}
		wg.Add(1)
		go dispatcher.Dispatch(mockAccount, activeAttestations, wg)

		// Send event
		blockHash := main.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: &blockHash}
		close(dispatcher.AttestRequired)

		// Wait for routine (and subroutines) to finish
		// Note: Dispatch already waits inside for subroutines but still call it to wait here too
		wg.Wait()

		// Assert
		status, exists := activeAttestations[blockHash]
		require.Equal(t, true, exists)
		require.Equal(t, main.Successful, status)
	})

	t.Run("Same attestRequired events are ignored if already ongoing or successful", func(t *testing.T) {
		// Setup
		dispatcher := main.NewEventDispatcher()
		blockHashFelt := new(felt.Felt).SetUint64(1)

		contractAddrFelt := main.AttestationContractAddress.ToFelt()
		calls := []rpc.InvokeFunctionCall{{
			ContractAddress: &contractAddrFelt,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFelt},
		}}
		addTxHash := utils.HexToFelt(t, "0x123")
		mockedAddTxResp := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash}
		// We expect this to be called only once (even though 3 events are sent)
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), calls, main.FEE_ESTIMATION_MULTIPLIER).
			DoAndReturn(func(ctx context.Context, calls []rpc.InvokeFunctionCall, multiplier float64) (*rpc.AddInvokeTransactionResponse, error) {
				// The spawned routine will sleep 1 second so that we can assert ongoing status (see below)
				time.Sleep(time.Second * 1)
				return &mockedAddTxResp, nil
			}).Times(1)

		// We expect this to be called only once (even though 3 events are sent)
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
			}, nil).
			Times(1)

		// Start routine
		activeAttestations := make(map[main.BlockHash]main.AttestationStatus)
		wg := &sync.WaitGroup{}
		wg.Add(1)
		go dispatcher.Dispatch(mockAccount, activeAttestations, wg)

		// Send the same event x3
		blockHash := main.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: &blockHash}

		// Mid-execution assertion: attestation is ongoing (1st go routine has not finished executing as it sleeps for 1 sec)
		//
		// This middle-exec assert might be a bit dangerous (could sleep here 0.1s maybe to be sure?)
		// will fail if thread here reaches the assert below before dispatcher main routine sets the status as ongoing
		status, exists := activeAttestations[blockHash]
		require.Equal(t, true, exists)
		require.Equal(t, main.Ongoing, status)

		// This 2nd event gets ignored when status is ongoing
		// Proof: only 1 call to BuildAndSendInvokeTxn nor GetTransactionStatus is asserted
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: &blockHash}

		// This time sleep is more than enough to make sure the 1st go routine has time to execute (2nd event got ignored)
		time.Sleep(time.Second * 2)

		// Mid-execution assertion: attestation is successful (1st go routine has indeed finished executing)
		status, exists = activeAttestations[blockHash]
		require.Equal(t, true, exists)
		require.Equal(t, main.Successful, status)

		// This 3rd event gets ignored also when status is successful
		// Proof: only 1 call to BuildAndSendInvokeTxn nor GetTransactionStatus is asserted
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: &blockHash}
		close(dispatcher.AttestRequired)

		// Wait for routine (and subroutines) to finish
		// Note: Dispatch already waits inside for subroutines but still call it to wait here too
		wg.Wait()

		// Re-assert (3rd event got ignored)
		status, exists = activeAttestations[blockHash]
		require.Equal(t, true, exists)
		require.Equal(t, main.Successful, status)
	})
}

func TestTrackAttest(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccountInterface(mockCtrl)

	t.Run("attestation is not successful if error", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(nil, errors.New("some internal error"))

		blockHash := main.BlockHash(*txHash)
		event := main.AttestRequired{BlockHash: &blockHash}
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
		event := main.AttestRequired{BlockHash: &blockHash}
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
		event := main.AttestRequired{BlockHash: &blockHash}
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
		event := main.AttestRequired{BlockHash: &blockHash}
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

	mockAccount := mocks.NewMockAccountInterface(mockCtrl)

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
