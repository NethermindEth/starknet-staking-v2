package signer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	"github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const (
	blockWithTxsMethod        = "starknet_getBlockWithTxs"
	stakerInfoEntrypoint      = "get_staker_info_v1"
	invalidResponseFlagsError = `{"code": -32602, "message": "Invalid params", ` +
		`"data": {"reason": "expected array for \"response_flags\""}}`
)

type blockWithTxsRequests struct {
	mu    sync.Mutex
	flags []string
}

func (r *blockWithTxsRequests) add(flags string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flags = append(r.flags, flags)
}

func (r *blockWithTxsRequests) responseFlags() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.flags)
}

func strictResponseFlagsNode(
	t *testing.T, mock validator.MockRPC,
) (*rpc.Provider, *blockWithTxsRequests) {
	t.Helper()

	upstream := validator.MockRPCServer(t, mock)
	requests := new(blockWithTxsRequests)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		var req struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &req) == nil && req.Method == blockWithTxsMethod {
			var flags string
			if len(req.Params) > 1 {
				flags = string(req.Params[1])
			}
			requests.add(flags)
			if flags == "null" {
				_, _ = fmt.Fprintf(
					w,
					`{"jsonrpc": "2.0", "id": %s, "error": %s}`,
					req.ID,
					invalidResponseFlagsError,
				)

				return
			}
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		upstream.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	provider, err := rpc.NewProvider(t.Context(), server.URL)
	require.NoError(t, err)

	return provider, requests
}

func requireNoNullResponseFlags(t *testing.T, requests *blockWithTxsRequests) {
	t.Helper()

	flags := requests.responseFlags()
	require.NotEmpty(t, flags)
	for _, sent := range flags {
		require.Contains(t, []string{"", "[]"}, sent)
	}
}

func flagsTestResults() map[string]string {
	return map[string]string{
		"starknet_blockNumber": "1000",
		"starknet_getNonce":    `"0x0"`,
		blockWithTxsMethod:     `{"block_hash": "0x1", "transactions": []}`,
	}
}

func TestBuildAttestTransactionResponseFlags(t *testing.T) {
	blockHash := types.BlockHash(felt.FromUint64[felt.Felt](0xabc))
	signerConfig := config.Signer{
		PrivKey:            "0x1",
		ExternalURL:        "http://127.0.0.1:1",
		OperationalAddress: "0x1234",
	}
	signers := []struct {
		name string
		new  func(ctx context.Context, provider rpc.RPCProvider) (signer.Signer, error)
	}{
		{
			name: "Internal signer",
			new: func(ctx context.Context, provider rpc.RPCProvider) (signer.Signer, error) {
				internalSigner, err := signer.NewInternalSigner(
					ctx, provider, log.NewNopZapLogger(), &signerConfig,
					new(config.ContractAddresses),
				)

				return &internalSigner, err
			},
		},
		{
			name: "External signer",
			new: func(ctx context.Context, provider rpc.RPCProvider) (signer.Signer, error) {
				externalSigner, err := signer.NewExternalSigner(
					ctx, provider, log.NewNopZapLogger(), &signerConfig,
					new(config.ContractAddresses),
				)

				return &externalSigner, err
			},
		},
	}

	for _, tc := range signers {
		t.Run(tc.name, func(t *testing.T) {
			provider, requests := strictResponseFlagsNode(
				t, validator.MockRPC{Results: flagsTestResults()},
			)
			attestSigner, err := tc.new(t.Context(), provider)
			require.NoError(t, err)

			txn, err := attestSigner.BuildAttestTransaction(t.Context(), &blockHash, math.MaxUint64)

			require.NoError(t, err)
			require.Equal(t, rpc.U64("0x0"), txn.Tip)
			requireNoNullResponseFlags(t, requests)
		})
	}
}

func TestClaimRewardsResponseFlags(t *testing.T) {
	provider, requests := strictResponseFlagsNode(t, validator.MockRPC{
		Results: flagsTestResults(),
		CallResults: map[string]string{
			stakerInfoEntrypoint: `["0x0", "` + rewardAccountAddress + `", "` +
				stakerAccountAddress + `", "0x1", "0x1", "0x1000000000000000000", "0x1"]`,
		},
	})
	stakerAddress, err := types.AddressFromString(stakerAccountAddress)
	require.NoError(t, err)
	core, logs := observer.New(zapcore.ErrorLevel)
	rewardSigner, err := signer.NewRewardSigner(
		t.Context(),
		provider,
		log.NewZapLoggerWithCore(core),
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
	require.NoError(t, err)

	rewardSigner.ClaimRewards(t.Context(), metrics.NewNoOpMetrics())

	entries := logs.AllUntimed()
	messages := make([]string, 0, len(entries))
	for _, entry := range entries {
		messages = append(messages, entry.Message)
	}
	require.Equal(t, []string{"Auto-Claiming Rewards: failed to populate fees"}, messages)
	// The claim transaction pays no tip, so the latest block is never requested
	require.Empty(t, requests.responseFlags())
}
