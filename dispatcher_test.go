package main_test

import (
	"context"
	"errors"
	"sync"
	"testing"

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

	mockAccount := mocks.NewMockAccount(mockCtrl)

	dispatcher := main.NewEventDispatcher()

	t.Run("Simple successful scenario: only 1 attest to make", func(t *testing.T) {
		// Setup
		blockHashFelt := new(felt.Felt).SetUint64(1)

		contractAddrFelt := main.AttestationContractAddress.ToFelt()
		calls := []rpc.InvokeFunctionCall{{
			ContractAddress: &contractAddrFelt,
			FunctionName:    "attest",
			CallData:        []*felt.Felt{blockHashFelt},
		}}
		addTxHash := utils.HexToFelt(t, "0x123")
		mockedAddTxResp := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash}
		mockAccount.EXPECT().BuildAndSendInvokeTxn(context.Background(), calls, 1.5).Return(&mockedAddTxResp, nil)

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
			}, nil)

		// Start routine
		wg := &sync.WaitGroup{}
		activeAttestations := make(map[main.BlockHash]main.AttestationStatus)
		wg.Add(1)
		go dispatcher.Dispatch(mockAccount, activeAttestations, wg)

		// Send event
		blockHash := main.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- main.AttestRequired{BlockHash: &blockHash}
		close(dispatcher.AttestRequired)

		// Wait for routine (and subroutines) to finish
		// Note: Dispatch alreadys waits inside for subroutines but still call it here for the test
		wg.Wait()

		// Assert
		status, exists := activeAttestations[blockHash]
		require.Equal(t, true, exists)
		require.Equal(t, main.Successful, status)
	})
}

func TestTrackAttest(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccount(mockCtrl)

	t.Run("attestation is not succesful if error", func(t *testing.T) {
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
		require.Equal(t, main.Ongoing, actualStatus)
		require.Equal(t, true, exists)
	})

	t.Run("attestation is not succesful if REJECTED", func(t *testing.T) {
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
		require.Equal(t, main.Ongoing, actualStatus)
		require.Equal(t, true, exists)
	})

	t.Run("attestation is not succesful if accepted but REVERTED", func(t *testing.T) {
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
		require.Equal(t, main.Ongoing, actualStatus)
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

	mockAccount := mocks.NewMockAccount(mockCtrl)

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
		t.Skip()
		txHash := new(felt.Felt).SetUint64(1)

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus: rpc.TxnStatus_Received,
			}, nil).
			// equal to `defaultAttestDelay`
			Times(10)

		// TODO: can we mock the time.Sleep? to make it faster
		status, err := main.TrackTransactionStatus(mockAccount, txHash)

		require.Nil(t, status)
		require.Equal(t, errors.New("Tx status did not change for a long time, retrying with next block"), err)
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
