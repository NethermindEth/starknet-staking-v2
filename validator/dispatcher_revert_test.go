package validator_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/client/rpcerr"
	"github.com/NethermindEth/starknet.go/rpc"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/sourcegraph/conc"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const (
	byteArrayMagicHex    = "0x46a6158a16a947e5916b2a2ca68501a45e93d7110e81aa2d6438b1c57c879a3"
	entrypointFailedFelt = "0x454e545259504f494e545f4641494c4544"
	attestationDoneFelts = byteArrayMagicHex +
		", 0x1, 0x4174746573746174696f6e20697320646f6e6520666f722074686973206570" +
		", 0x6f6368, 0x3"
	outOfWindowFelts = byteArrayMagicHex +
		", 0x0, 0x4174746573746174696f6e206973206f7574206f662077696e646f77, 0x1c"
	sepoliaAttest = "0x3f32e152b9637c31bfcf73e434f78591067a01ba070505ff6ee195642c9acfb"
)

type nodeError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e nodeError) Error() string { return e.Message }

func attestRevert(t *testing.T, panicFelts string, nested bool) error {
	t.Helper()

	encodedFelts, err := json.Marshal(panicFelts)
	require.NoError(t, err)
	executionError := string(encodedFelts)
	if nested {
		executionError = revertFrame("0x123", "__execute__", revertFrame(
			sepoliaAttest, "attest", executionError,
		))
	}

	rpcErr := rpcerr.UnwrapToRPCErr(nodeError{
		Code:    41,
		Message: "Transaction execution error",
		Data: json.RawMessage(
			`{"transaction_index": 0, "execution_error": ` + executionError + `}`,
		),
	}, rpc.ErrTxnExec)
	require.Equal(t, 41, rpcErr.Code)

	return rpcErr
}

func revertFrame(contract, entrypoint, inner string) string {
	return fmt.Sprintf(
		`{"contract_address": %q, "class_hash": "0x1", "selector": %q, "error": %s}`,
		contract, snGoUtils.GetSelectorFromNameFelt(entrypoint).String(), inner,
	)
}

func TestDispatchAttestationDoneRevert(t *testing.T) {
	blockHash := types.BlockHash(*new(felt.Felt).SetUint64(0xb10c))
	alreadyDone := attestationDoneFelts + ", " + entrypointFailedFelt

	testCases := []struct {
		name   string
		expect func(mockSigner *mocks.MockSigner)
	}{
		{
			name: "fee estimate reverts",
			expect: func(mockSigner *mocks.MockSigner) {
				mockSigner.EXPECT().
					EstimateFee(gomock.Any(), gomock.Any()).
					Return(rpc.FeeEstimation{}, attestRevert(t, alreadyDone, true))
			},
		},
		{
			name: "invoke reverts",
			expect: func(mockSigner *mocks.MockSigner) {
				mockSigner.EXPECT().
					EstimateFee(gomock.Any(), gomock.Any()).
					Return(feeEstimate(), nil)
				mockSigner.EXPECT().SignTransaction(gomock.Any(), gomock.Any()).Return(nil, nil)
				mockSigner.EXPECT().
					InvokeTransaction(gomock.Any(), gomock.Any()).
					Return(rpc.AddInvokeTransactionResponse{}, attestRevert(t, alreadyDone, false))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockSigner, balanceChecked := newAttestSigner(t)
			mockSigner.EXPECT().
				BuildAttestTransaction(gomock.Any(), &blockHash).
				Return(attestTxn(), nil)
			mockSigner.EXPECT().SignTransaction(gomock.Any(), gomock.Any()).Return(nil, nil)
			tc.expect(mockSigner)
			tracer := new(attestTracer)
			dispatcher, stop := startDispatcher(t, mockSigner, tracer)

			sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
			sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
			require.Equal(t, validator.Successful, dispatcher.CurrentAttest.Status)
			sendEvent(t, dispatcher.EndOfWindow, struct{}{})
			stop()
			waitBalanceCheck(t, balanceChecked)

			require.Equal(t, 0, tracer.submitted)
			require.Equal(t, 1, tracer.confirmed)
			require.Equal(t, 0, tracer.failures)
		})
	}
}

func TestDispatchLogsReadableRevert(t *testing.T) {
	var buf bytes.Buffer
	logger, err := log.NewZapLogger(
		log.NewLevel(log.INFO), log.WithWriter(&buf), log.WithColour(false),
	)
	require.NoError(t, err)
	blockHash := types.BlockHash(*new(felt.Felt).SetUint64(0xb10c))
	mockSigner, _ := newAttestSigner(t)
	mockSigner.EXPECT().BuildAttestTransaction(gomock.Any(), &blockHash).Return(attestTxn(), nil)
	mockSigner.EXPECT().SignTransaction(gomock.Any(), gomock.Any()).Return(nil, nil)
	mockSigner.EXPECT().
		EstimateFee(gomock.Any(), gomock.Any()).
		Return(rpc.FeeEstimation{}, attestRevert(t, outOfWindowFelts, true))

	dispatcher := validator.NewEventDispatcher[*mocks.MockSigner]()
	wg := conc.NewWaitGroup()
	wg.Go(func() {
		dispatcher.Dispatch(t.Context(), mockSigner, nil, 0, logger, metrics.NewNoOpMetrics())
	})
	sendEvent(t, dispatcher.DoAttest, types.DoAttest{BlockHash: blockHash})
	close(dispatcher.DoAttest)
	require.Nil(t, wg.WaitAndRecover())

	require.Equal(t, validator.Failed, dispatcher.CurrentAttest.Status)
	require.Contains(t, buf.String(), `failed to attest. Will retry on the next block	`+
		`{"error": "signer failed to estimate fee: Attestation is out of window `+
		`(contract `+sepoliaAttest+`, entrypoint attest)"}`)
}
