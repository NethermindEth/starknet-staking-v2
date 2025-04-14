package validator_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

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

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
			}, nil)

		// Start routine
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, logger) })

		// Send event
		blockHash := validator.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}
		close(dispatcher.AttestRequired)

		// Wait for dispatch routine (and consequently its spawned subroutines) to finish
		wg.Wait()

		// Assert
		require.Equal(t, blockHash, dispatcher.CurrentAttest.BlockHash)
		require.Equal(t, validator.Successful, dispatcher.CurrentAttestStatus)
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
				DoAndReturn(
					func(
						ctx context.Context, calls []rpc.InvokeFunctionCall, multiplier float64,
					) (*rpc.AddInvokeTransactionResponse, error) {
						// The Dispatch routine will sleep 1 second so that we can assert ongoing
						// status (see below)
						time.Sleep(time.Second * 1)
						return &mockedAddTxResp, nil
					}).Times(1)

			// We expect GetTransactionStatus to be called only once (even if 3 events are sent)
			mockAccount.EXPECT().
				GetTransactionStatus(context.Background(), addTxHash).
				Return(&rpc.TxnStatusResp{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
				}, nil).
				Times(1)

			// Start routine
			wg := &conc.WaitGroup{}
			wg.Go(func() { dispatcher.Dispatch(mockAccount, logger) })

			// Send the same event x3
			blockHash := validator.BlockHash(*blockHashFelt)
			dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

			// Mid-execution assertion: attestation is ongoing (dispatch go routine has not
			// finished executing as it sleeps for 1 sec)
			require.Truef(
				t,
				waitFor(
					time.Second,
					func() bool { return dispatcher.CurrentAttestStatus == validator.Ongoing },
				),
				"Ongoing status never set by dispatcher",
			)
			require.Equal(t, blockHash, dispatcher.CurrentAttest.BlockHash)

			// This 2nd event gets ignored when status is ongoing
			// Proof: only 1 call to BuildAndSendInvokeTxn and GetTransactionStatus is asserted
			dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

			// Mid-execution assertion: attestation is successful (1st go routine has indeed finished
			// executing)
			require.Truef(
				t,
				waitFor(
					time.Second,
					func() bool { return dispatcher.CurrentAttestStatus == validator.Successful },
				),
				"Succesful status never set by dispatcher",
			)
			require.Equal(t, blockHash, dispatcher.CurrentAttest.BlockHash)

			// This 3rd event gets ignored also when status is successful
			// Proof: only 1 call to BuildAndSendInvokeTxn and GetTransactionStatus is asserted
			dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}
			close(dispatcher.AttestRequired)

			// Wait for dispatch routine (and consequently its spawned subroutines) to finish
			wg.Wait()

			// Re-assert (3rd event got ignored)
			require.Equal(t, blockHash, dispatcher.CurrentAttest.BlockHash)
			require.Equal(t, validator.Successful, dispatcher.CurrentAttestStatus)
		})

	t.Run("Same AttestRequired events are ignored until attestation fails", func(t *testing.T) {
		// Sequence of actions:
		// - an AttestRequired event A is emitted and processed
		// - an AttestRequired event A is emitted and ignored (as 1st one is getting processed)
		// - an AttestRequired event is considered (as 1st one finished & failed)

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

		// We expect GetTransactionStatus to be called only once (for the 2 first events)
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash1).
			DoAndReturn(func(ctx context.Context, hash *felt.Felt) (*rpc.TxnStatusResp, error) {
				// The spawned routine (created by Dispatch) will sleep 1 second so that we can assert ongoing status
				time.Sleep(time.Second * 1)
				return &rpc.TxnStatusResp{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusREVERTED,
					FailureReason:   "some failure reason",
				}, nil
			}).
			Times(1)

		// Start routine
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, logger) })

		// Send the same event x3
		blockHash := validator.BlockHash(*blockHashFelt)
		dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

		// Sleep just a bit so that dispatch routine has time to set the status as ongoing
		time.Sleep(time.Second / 10)

		// Mid-execution assertion: attestation is ongoing (1st go routine has not finished executing as it sleeps for 1 sec)
		require.Equal(t, blockHash, dispatcher.CurrentAttest.BlockHash)
		require.Equal(t, validator.Ongoing, dispatcher.CurrentAttestStatus)

		// This 2nd event gets ignored when status is ongoing
		// Proof: only 1 call to BuildAndSendInvokeTxn and GetTransactionStatus is asserted so far
		dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

		// This time sleep is more than enough to make sure spawned trackAttest routine has time to execute (2nd event got ignored)
		time.Sleep(time.Second * 2)

		// Mid-execution assertion: attestation has failed (1st go routine has indeed finished executing)
		require.Equal(t, blockHash, dispatcher.CurrentAttest.BlockHash)
		require.Equal(t, validator.Failed, dispatcher.CurrentAttestStatus)

		// Preparation for 3rd event

		addTxHash2 := utils.HexToFelt(t, "0x456")
		mockedAddTxResp2 := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash2}

		// We expect a 2nd call to BuildAndSendInvokeTxn
		mockAccount.EXPECT().
			BuildAndSendInvokeTxn(context.Background(), calls, validator.FEE_ESTIMATION_MULTIPLIER).
			Return(&mockedAddTxResp2, nil).
			Times(1)

		// We expect a 2nd call to GetTransactionStatus
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHash2).
			Return(&rpc.TxnStatusResp{
				FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
				ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
			}, nil).
			Times(1)

		// This 3rd event does not get ignored as previous attestation has failed
		// Proof: a 2nd call to BuildAndSendInvokeTxn and GetTransactionStatus is asserted
		dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}
		close(dispatcher.AttestRequired)

		// Wait for dispatch routine (and consequently its spawned subroutines) to finish
		wg.Wait()

		// Re-assert (3rd event got ignored)
		require.Equal(t, blockHash, dispatcher.CurrentAttest.BlockHash)
		require.Equal(t, validator.Successful, dispatcher.CurrentAttestStatus)
	})

	t.Run(
		"Failed invoke tx also (just like TrackAttest) marks attest as failed if invoke tx fails",
		func(t *testing.T) {
			// Sequence of actions:
			// - an AttestRequired event A is emitted and processed (invoke tx, not trackAttest,
			//   fails)
			// - an AttestRequired event A is emitted and considered (as 1st one failed)

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
				Return(nil, errors.New("invoke tx failed for some reason")).
				Times(1)

			addTxHash := utils.HexToFelt(t, "0x123")
			mockedAddTxResp := rpc.AddInvokeTransactionResponse{TransactionHash: addTxHash}

			// Next call to BuildAndSendInvokeTxn to then succeed
			mockAccount.EXPECT().
				BuildAndSendInvokeTxn(
					context.Background(), calls, validator.FEE_ESTIMATION_MULTIPLIER,
				).
				Return(&mockedAddTxResp, nil).
				Times(1)

			// We expect GetTransactionStatus to be called only once
			mockAccount.EXPECT().
				GetTransactionStatus(context.Background(), addTxHash).
				DoAndReturn(
					func(ctx context.Context, hash *felt.Felt) (*rpc.TxnStatusResp, error) {
						// The spawned routine (created by Dispatch) will sleep 1 second so
						// that we can assert ongoing status
						time.Sleep(time.Second * 1)
						return &rpc.TxnStatusResp{
							FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
							ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
						}, nil
					}).
				Times(1)

			// Start routine
			wg := &conc.WaitGroup{}
			wg.Go(func() { dispatcher.Dispatch(mockAccount, logger) })

			// Send the same event x2
			blockHash := validator.BlockHash(*blockHashFelt)
			dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

			// Mid-execution assertion: attestation has failed
			require.Truef(
				t,
				waitFor(
					2*time.Second,
					func() bool { return dispatcher.CurrentAttestStatus == validator.Failed },
				),
				"Failed status never set by dispatcher",
			)
			require.Equal(t, blockHash, dispatcher.CurrentAttest.BlockHash)

			// This 2nd event gets considered as previous one failed
			dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHash}

			close(dispatcher.AttestRequired)
			// Wait for dispatch routine (and consequently its spawned subroutines) to finish
			wg.Wait()

			// Mid-execution assertion: attestation has failed (1st go routine has indeed finished executing)
			require.Equal(t, blockHash, dispatcher.CurrentAttest.BlockHash)
			require.Equal(t, validator.Successful, dispatcher.CurrentAttestStatus)
		})

	t.Run("AttestRequired events transition with EndOfWindow events", func(t *testing.T) {
		// Sequence of actions:
		// - an AttestRequired event A is emitted and processed (successful)
		// - an EndOfWindow event for A is emitted
		// - an AttestRequired event B is emitted and processed (failed)
		// - an EndOfWindow event for B is emitted

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

		// We expect GetTransactionStatus to be called once for event B
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), addTxHashB).
			DoAndReturn(func(ctx context.Context, hash *felt.Felt) (*rpc.TxnStatusResp, error) {
				// The spawned routine (created by Dispatch) will sleep 1 second so that we can assert ongoing status
				time.Sleep(time.Second * 1)

				return &rpc.TxnStatusResp{
					FinalityStatus: rpc.TxnStatus_Rejected,
				}, nil
			}).
			Times(1)

		// Start routine
		wg := &conc.WaitGroup{}
		wg.Go(func() { dispatcher.Dispatch(mockAccount, logger) })

		// Send event A
		blockHashA := validator.BlockHash(*blockHashFeltA)
		dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHashA}

		// To give time for the spawned routine to execute
		time.Sleep(time.Second / 5)

		// Mid-execution assertion: attestation A is successful
		require.Equal(t, blockHashA, dispatcher.CurrentAttest.BlockHash)
		require.Equal(t, validator.Successful, dispatcher.CurrentAttestStatus)

		// Send EndOfWindow event for event A
		dispatcher.EndOfWindow <- struct{}{}

		// Send event B
		blockHashB := validator.BlockHash(*blockHashFeltB)
		dispatcher.AttestRequired <- validator.AttestRequired{BlockHash: blockHashB}

		// Sleep just a bit so that dispatch routine has time to set the status as ongoing
		time.Sleep(time.Second / 10)

		// Mid-execution assertion: attestation B has failed
		require.Equal(t, blockHashB, dispatcher.CurrentAttest.BlockHash)
		require.Equal(t, validator.Ongoing, dispatcher.CurrentAttestStatus)

		// To give time for the spawned routine to finish executing
		time.Sleep(time.Second * 1)

		// Send EndOfWindow event for event B
		dispatcher.EndOfWindow <- struct{}{}

		close(dispatcher.AttestRequired)
		// Wait for dispatch routine to finish executing (its spawned subroutines must now be done)
		wg.Wait()

		// End of execution assertion: attestation B was successful
		require.Equal(t, blockHashB, dispatcher.CurrentAttest.BlockHash)
		require.Equal(t, validator.Failed, dispatcher.CurrentAttestStatus)
	})
}

func TestTrackAttest(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)
	logger := utils.NewNopZapLogger()

	t.Run("Status gets set if block hash entry exists", func(t *testing.T) {
		t.Run("attestation fails if error", func(t *testing.T) {
			txHash := new(felt.Felt).SetUint64(1)
			txRes := &rpc.AddInvokeTransactionResponse{TransactionHash: txHash}

			blockHash := new(felt.Felt).SetUint64(1)
			attestEvent := validator.AttestRequired{BlockHash: validator.BlockHash(*blockHash)}

			mockAccount.EXPECT().
				GetTransactionStatus(context.Background(), txHash).
				Return(nil, errors.New("some internal error"))

			x, _ := utils.NewZapLogger(utils.DEBUG, true)
			txStatus := validator.TrackAttest(mockAccount, x, &attestEvent, txRes)

			require.Equal(t, validator.Failed, txStatus)
		})

		t.Run("attestation fails if REJECTED", func(t *testing.T) {
			txHash := new(felt.Felt).SetUint64(1)
			txRes := &rpc.AddInvokeTransactionResponse{TransactionHash: txHash}

			blockHash := new(felt.Felt).SetUint64(1)
			attestEvent := validator.AttestRequired{BlockHash: validator.BlockHash(*blockHash)}

			mockAccount.EXPECT().
				GetTransactionStatus(context.Background(), txHash).
				Return(&rpc.TxnStatusResp{
					FinalityStatus: rpc.TxnStatus_Rejected,
				}, nil)

			txStatus := validator.TrackAttest(mockAccount, logger, &attestEvent, txRes)

			require.Equal(t, validator.Failed, txStatus)
		})

		t.Run("attestation fails if accepted but REVERTED", func(t *testing.T) {
			txHash := new(felt.Felt).SetUint64(1)
			txRes := &rpc.AddInvokeTransactionResponse{TransactionHash: txHash}

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

			txStatus := validator.TrackAttest(mockAccount, logger, &attestEvent, txRes)

			require.Equal(t, validator.Failed, txStatus)
		})

		t.Run("attestation succeeds if accepted & SUCCEEDED", func(t *testing.T) {
			txHash := new(felt.Felt).SetUint64(1)
			txRes := &rpc.AddInvokeTransactionResponse{TransactionHash: txHash}

			blockHash := new(felt.Felt).SetUint64(1)
			attestEvent := validator.AttestRequired{BlockHash: validator.BlockHash(*blockHash)}

			mockAccount.EXPECT().
				GetTransactionStatus(context.Background(), txHash).
				Return(&rpc.TxnStatusResp{
					FinalityStatus:  rpc.TxnStatus_Accepted_On_L2,
					ExecutionStatus: rpc.TxnExecutionStatusSUCCEEDED,
				}, nil)

			txStatus := validator.TrackAttest(mockAccount, logger, &attestEvent, txRes)

			require.Equal(t, validator.Successful, txStatus)
		})
	})
}

func TestTrackTransactionStatus(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockAccount := mocks.NewMockAccounter(mockCtrl)
	logger := utils.NewNopZapLogger()

	t.Run("GetTransactionStatus returns an error different from tx hash not found", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		// Set expectations
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(nil, errors.New("some internal error"))

		status, err := validator.TrackTransactionStatus(mockAccount, logger, txHash)

		require.Nil(t, status)
		require.Equal(t, errors.New("some internal error"), err)
	})

	t.Run("Returning a tx hash not found error triggers a retry", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)
		// Mock time.Sleep (no reason to wait in that test)
		validator.Sleep = func(d time.Duration) {
			// Do nothing
		}

		// Set expectations
		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(nil, validator.ErrTxnHashNotFound)

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus: rpc.TxnStatus_Accepted_On_L2,
			}, nil)

		status, err := validator.TrackTransactionStatus(mockAccount, logger, txHash)

		require.Equal(t, &rpc.TxnStatusResp{
			FinalityStatus: rpc.TxnStatus_Accepted_On_L2,
		}, status)
		require.Nil(t, err)

		// Reset time.Sleep function
		validator.Sleep = time.Sleep
	})

	t.Run(
		"Returns an error if tx status does not change for `DEFAULT_MAX_RETRIES` seconds",
		func(t *testing.T) {
			// Mock time.Sleep (absolutely no reason to wait in that test)
			validator.Sleep = func(d time.Duration) {}
			defer func() { validator.Sleep = time.Sleep }()

			txHash := new(felt.Felt).SetUint64(1)

			mockAccount.EXPECT().
				GetTransactionStatus(context.Background(), txHash).
				Return(&rpc.TxnStatusResp{
					FinalityStatus: rpc.TxnStatus_Received,
				}, nil).
				Times(validator.DEFAULT_MAX_RETRIES)

			status, err := validator.TrackTransactionStatus(mockAccount, logger, txHash)

			require.Nil(t, status)
			require.Equal(
				t,
				fmt.Errorf(
					"tx status did not change for at least %s seconds",
					strconv.Itoa(validator.DEFAULT_MAX_RETRIES),
				),
				err,
			)
		})

	t.Run("Returns the status if different from RECEIVED, here ACCEPTED_ON_L2", func(t *testing.T) {
		txHash := new(felt.Felt).SetUint64(1)

		mockAccount.EXPECT().
			GetTransactionStatus(context.Background(), txHash).
			Return(&rpc.TxnStatusResp{
				FinalityStatus: rpc.TxnStatus_Accepted_On_L2,
			}, nil).
			Times(1)

		status, err := validator.TrackTransactionStatus(mockAccount, logger, txHash)

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

		status, err := validator.TrackTransactionStatus(mockAccount, logger, txHash)

		require.Equal(t, &rpc.TxnStatusResp{
			FinalityStatus: rpc.TxnStatus_Accepted_On_L1,
		}, status)
		require.Nil(t, err)
	})
}

func waitFor(duration time.Duration, condition func() bool) bool {
	startTime := time.Now()
	interval := duration / 100
	for time.Since(startTime) < duration {
		time.Sleep(interval)
		if condition() {
			return true
		}
	}
	return false
}
