package signer_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/client/rpcerr"
	"github.com/NethermindEth/starknet.go/rpc"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const (
	byteArrayMagicHex   = "0x46a6158a16a947e5916b2a2ca68501a45e93d7110e81aa2d6438b1c57c879a3"
	stakerNotFoundFelts = byteArrayMagicHex +
		", 0x0, 0x5374616b657220646f6573206e6f74206578697374, 0x15"
	attestationDoneFelts = byteArrayMagicHex +
		", 0x1, 0x4174746573746174696f6e20697320646f6e6520666f722074686973206570" +
		", 0x6f6368, 0x3"
	entrypointFailedFelt = "0x454e545259504f494e545f4641494c4544"
	invalidCallerFelt    = "0x496e76616c69642063616c6c6572"
	sepoliaStaking       = "0x3745ab04a431fc02871a139be6b93d9260b0ff3e779ad9c8b377183b23109f1"
	sepoliaAttest        = "0x3f32e152b9637c31bfcf73e434f78591067a01ba070505ff6ee195642c9acfb"
	stakerHint           = "; the operational address 0x1234 is not registered as a staker " +
		"on SN_SEPOLIA (check --signer-op-address and --staking-contract-address)"
)

type nodeError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e nodeError) Error() string { return e.Message }

func contractRevert(t *testing.T, revertError string) error {
	t.Helper()

	err := rpcerr.UnwrapToRPCErr(nodeError{
		Code:    40,
		Message: "Contract error",
		Data:    json.RawMessage(`{"revert_error": ` + revertError + `}`),
	}, rpc.ErrContractError)
	require.Equal(t, 40, err.Code)

	return err
}

func executionRevert(t *testing.T, executionError string) error {
	t.Helper()

	err := rpcerr.UnwrapToRPCErr(nodeError{
		Code:    41,
		Message: "Transaction execution error",
		Data: json.RawMessage(
			`{"transaction_index": 0, "execution_error": ` + executionError + `}`,
		),
	}, rpc.ErrTxnExec)
	require.Equal(t, 41, err.Code)

	return err
}

func quoted(t *testing.T, text string) string {
	t.Helper()

	encoded, err := json.Marshal(text)
	require.NoError(t, err)

	return string(encoded)
}

func frame(t *testing.T, contract, entrypoint, inner string) string {
	t.Helper()

	return fmt.Sprintf(
		`{"contract_address": %q, "class_hash": "0x1", "selector": %q, "error": %s}`,
		contract, snGoUtils.GetSelectorFromNameFelt(entrypoint).String(), inner,
	)
}

func TestReadableContractError(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "ByteArray with a pending word only",
			err:      contractRevert(t, quoted(t, stakerNotFoundFelts)),
			expected: "Staker does not exist",
		},
		{
			name:     "ByteArray with full words",
			err:      contractRevert(t, quoted(t, attestationDoneFelts)),
			expected: "Attestation is done for this epoch",
		},
		{
			name:     "ByteArray followed by other panic values",
			err:      contractRevert(t, quoted(t, attestationDoneFelts+", "+entrypointFailedFelt)),
			expected: "'Attestation is done for this epoch', 'ENTRYPOINT_FAILED'",
		},
		{
			name:     "empty ByteArray",
			err:      contractRevert(t, quoted(t, byteArrayMagicHex+", 0x0, 0x0, 0x0")),
			expected: "empty revert reason",
		},
		{
			name:     "single short string",
			err:      contractRevert(t, quoted(t, invalidCallerFelt)),
			expected: "Invalid caller",
		},
		{
			name: "short strings mixed with values that are not text",
			err: contractRevert(t, quoted(t,
				"0x617267656e742f6d756c746963616c6c2d6661696c6564, 0x1, "+entrypointFailedFelt,
			)),
			expected: "'argent/multicall-failed', 0x1, 'ENTRYPOINT_FAILED'",
		},
		{
			name:     "values that are not text stay hex",
			err:      contractRevert(t, quoted(t, "0x15, 0x0")),
			expected: "0x15, 0x0",
		},
		{
			name: "ByteArray with more full words than given",
			err: contractRevert(t, quoted(t,
				byteArrayMagicHex+", 0x2, 0x5374616b657220646f6573206e6f74206578697374, 0x15",
			)),
			expected: byteArrayMagicHex + ", 0x2, 0x5374616b657220646f6573206e6f74206578697374, 0x15",
		},
		{
			name: "ByteArray with a pending length above 30",
			err: contractRevert(t, quoted(t,
				byteArrayMagicHex+", 0x0, 0x5374616b657220646f6573206e6f74206578697374, 0x1f",
			)),
			expected: byteArrayMagicHex + ", 0x0, 0x5374616b657220646f6573206e6f74206578697374, 0x1f",
		},
		{
			name: "ByteArray with a pending word longer than its length",
			err: contractRevert(t, quoted(t,
				byteArrayMagicHex+", 0x0, 0x5374616b657220646f6573206e6f74206578697374, 0x3",
			)),
			expected: byteArrayMagicHex + ", 0x0, 0x5374616b657220646f6573206e6f74206578697374, 0x3",
		},
		{
			name: "ByteArray with a full word over 31 bytes",
			err: contractRevert(t, quoted(t,
				byteArrayMagicHex+", 0x1, 0x"+
					"1000000000000000000000000000000000000000000000000000000000000000, 0x0, 0x0",
			)),
			expected: byteArrayMagicHex + ", 0x1, 0x" +
				"1000000000000000000000000000000000000000000000000000000000000000, 0x0, 0x0",
		},
		{
			name:     "text revert reason on one line",
			err:      contractRevert(t, quoted(t, "Execution failed. Failure reason:\n0x1 ('x').")),
			expected: "Execution failed. Failure reason: 0x1 ('x').",
		},
		{
			name: "structured error",
			err: contractRevert(t, frame(
				t, sepoliaStaking, "get_staker_info_v1", quoted(t, `"Staker does not exist"`),
			)),
			expected: "Staker does not exist (contract " + sepoliaStaking +
				", entrypoint get_staker_info_v1)",
		},
		{
			name: "structured error from an unknown entrypoint",
			err:  contractRevert(t, frame(t, "0x5", "other", quoted(t, invalidCallerFelt))),
			expected: "Invalid caller (contract 0x5, selector " +
				snGoUtils.GetSelectorFromNameFelt("other").String() + ")",
		},
		{
			name: "structured error nested twice",
			err: executionRevert(t, frame(t, "0x1234", "__execute__", frame(
				t, sepoliaAttest, "attest",
				quoted(t, attestationDoneFelts+", "+entrypointFailedFelt),
			))),
			expected: "'Attestation is done for this epoch', 'ENTRYPOINT_FAILED' " +
				"(contract " + sepoliaAttest + ", entrypoint attest)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			readable := signer.ReadableContractError(tc.err)

			require.EqualError(t, readable, tc.expected)
			var rpcErr *rpcerr.RPCError
			require.ErrorAs(t, readable, &rpcErr)
			require.Same(t, tc.err, rpcErr)
		})
	}

	t.Run("wrapped error keeps its context", func(t *testing.T) {
		revert := contractRevert(t, quoted(t, stakerNotFoundFelts))
		err := fmt.Errorf("signer failed to estimate fee: %w", revert)

		readable := signer.ReadableContractError(err)

		require.EqualError(t, readable, "signer failed to estimate fee: Staker does not exist")
		require.ErrorIs(t, readable, revert)
	})

	unchanged := []struct {
		name string
		err  error
	}{
		{
			name: "RPC error that is not a contract error",
			err: &rpcerr.RPCError{
				Code:    rpcerr.InternalError,
				Message: "Internal Error",
				Data:    rpcerr.StringErrData(stakerNotFoundFelts),
			},
		},
		{
			name: "validation failure",
			err: &rpcerr.RPCError{
				Code:    55,
				Message: "Account validation failed",
				Data:    rpcerr.StringErrData(invalidCallerFelt),
			},
		},
		{
			name: "transport error",
			err:  errors.New("dial tcp 127.0.0.1:1: connect: connection refused"),
		},
	}
	for _, tc := range unchanged {
		t.Run(tc.name+" is unchanged", func(t *testing.T) {
			require.Same(t, tc.err, signer.ReadableContractError(tc.err))
		})
	}
}

func newMockedInternalSigner(t *testing.T, mock validator.MockRPC) *signer.InternalSigner {
	t.Helper()

	server := validator.MockRPCServer(t, mock)
	provider, err := rpc.NewProvider(t.Context(), server.URL)
	require.NoError(t, err)
	internalSigner, err := signer.NewInternalSigner(
		t.Context(),
		provider,
		log.NewNopZapLogger(),
		&config.Signer{PrivKey: "0x1", OperationalAddress: "0x1234"},
		new(config.ContractAddresses),
	)
	require.NoError(t, err)

	return &internalSigner
}

func nodeErrorBody(code int, message, data string) string {
	return fmt.Sprintf(`{"code": %d, "message": %q, "data": %s}`, code, message, data)
}

func TestFetchEpochInfoContractRevert(t *testing.T) {
	const epochInfoFailed = "calling get_attestation_info_by_operational_address on the " +
		"staking contract " + sepoliaStaking + " failed: "

	testCases := []struct {
		name        string
		revertError string
		expected    string
	}{
		{
			name:        "felt list from Juno",
			revertError: `"` + stakerNotFoundFelts + `"`,
			expected:    epochInfoFailed + "Staker does not exist" + stakerHint,
		},
		{
			name: "structured error",
			revertError: frame(
				t,
				sepoliaStaking,
				"get_attestation_info_by_operational_address",
				quoted(t, `"Staker does not exist"`),
			),
			expected: epochInfoFailed + "Staker does not exist" + stakerHint,
		},
		{
			name:        "other revert reason",
			revertError: `"` + invalidCallerFelt + `"`,
			expected:    epochInfoFailed + "Invalid caller",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			internalSigner := newMockedInternalSigner(t, validator.MockRPC{
				CallErrors: map[string]string{
					"get_attestation_info_by_operational_address": nodeErrorBody(
						40, "Contract error", `{"revert_error": `+tc.revertError+`}`,
					),
				},
			})

			epochInfo, err := signer.FetchEpochInfo(t.Context(), internalSigner)

			require.Equal(t, types.EpochInfo{}, epochInfo)
			require.EqualError(t, err, tc.expected)
			var rpcErr *rpcerr.RPCError
			require.ErrorAs(t, err, &rpcErr)
			require.Equal(t, 40, rpcErr.Code)
		})
	}

	t.Run("Error that is not a revert keeps its text", func(t *testing.T) {
		internalSigner := newMockedInternalSigner(t, validator.MockRPC{
			CallErrors: map[string]string{
				"get_attestation_info_by_operational_address": nodeErrorBody(
					-32603, "Internal error", `"node is starting"`,
				),
			},
		})

		_, err := signer.FetchEpochInfo(t.Context(), internalSigner)

		require.EqualError(
			t, err, epochInfoFailed+`-32603 Internal Error: Internal error "node is starting"`,
		)
	})
}

const (
	rewardAccountAddress = "0x5c85dd30df86ed1f2cfe1806417efb2cae421bffdee8110a74a3d3eb95b28d3"
	stakerAccountAddress = "0x7f3f971211da11bbf86527424b9fb7640c0f3fc3c426d3cd1ad833389476a6"
)

func newMockedRewardSigner(
	t *testing.T, mock validator.MockRPC, logger log.Logger,
) (signer.RewardSigner, error) {
	t.Helper()

	server := validator.MockRPCServer(t, mock)
	provider, err := rpc.NewProvider(t.Context(), server.URL)
	require.NoError(t, err)
	stakerAddress, err := types.AddressFromString(stakerAccountAddress)
	require.NoError(t, err)

	return signer.NewRewardSigner(
		t.Context(),
		provider,
		logger,
		&config.Reward{
			PrivKey:        "0x1",
			Address:        rewardAccountAddress,
			ClaimThreshold: 1,
			ClaimMaxFee:    1,
		},
		new(config.ContractAddresses),
		&stakerAddress,
		false,
	)
}

func TestRewardSignerContractRevert(t *testing.T) {
	t.Run("Staker info call", func(t *testing.T) {
		rewardSigner, err := newMockedRewardSigner(t, validator.MockRPC{
			CallErrors: map[string]string{
				"get_staker_info_v1": nodeErrorBody(
					40, "Contract error", `{"revert_error": "`+stakerNotFoundFelts+`"}`,
				),
			},
		}, log.NewNopZapLogger())

		require.Nil(t, rewardSigner)
		require.EqualError(t, err, "reward address check: failed to fetch staker info: "+
			"calling get_staker_info_v1 on the staking contract "+sepoliaStaking+
			" failed: Staker does not exist")
	})

	t.Run("Claim fee estimate", func(t *testing.T) {
		core, logs := observer.New(zapcore.ErrorLevel)
		rewardSigner, err := newMockedRewardSigner(t, validator.MockRPC{
			Results: map[string]string{
				"starknet_blockNumber":     "1000",
				"starknet_getNonce":        `"0x0"`,
				"starknet_getBlockWithTxs": `{"block_hash": "0x1", "transactions": []}`,
			},
			CallResults: map[string]string{
				"get_staker_info_v1": `["0x0", "` + rewardAccountAddress + `", "` +
					stakerAccountAddress + `", "0x1", "0x1", "0x1000000000000000000", "0x1"]`,
			},
			Errors: map[string]string{
				estimateFeeMethod: nodeErrorBody(
					41, "Transaction execution error", `{"transaction_index": 0, `+
						`"execution_error": `+frame(t, rewardAccountAddress, "__execute__", frame(
						t, sepoliaStaking, "claim_rewards", quoted(t, byteArrayMagicHex+
							", 0x0, 0x43616c6c6572206973206e6f74207265776172642061646472657373"+
							", 0x1c"),
					))+`}`,
				),
			},
		}, log.NewZapLoggerWithCore(core))
		require.NoError(t, err)

		rewardSigner.ClaimRewards(t.Context(), metrics.NewNoOpMetrics())

		failures := logs.
			FilterMessage("Auto-Claiming Rewards: failed to populate fees").
			AllUntimed()
		require.Len(t, failures, 1)
		require.Equal(t, "failed to estimate fee: Caller is not reward address "+
			"(contract "+sepoliaStaking+", entrypoint claim_rewards)",
			failures[0].ContextMap()["error"])
	})
}
