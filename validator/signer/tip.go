package signer

import (
	"context"

	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet.go/rpc"
)

type responseFlagsProvider struct {
	rpc.RPCProvider
}

func (p responseFlagsProvider) BlockWithTxs(
	ctx context.Context, blockID rpc.BlockID, responseFlags []rpc.TxnResponseFlag,
) (any, error) {
	if responseFlags == nil {
		responseFlags = []rpc.TxnResponseFlag{}
	}

	return p.RPCProvider.BlockWithTxs(ctx, blockID, responseFlags)
}

// estimateTip wraps rpc.EstimateTip, which requests the latest block with nil response
// flags. Those are sent as ["latest", null], and Pathfinder rejects a null response_flags
// with -32602 "expected array for response_flags". The wrapped provider sends an empty
// array instead, which both Juno and Pathfinder accept.
// The real fix should be done in starknet.go but that is discontinued
func estimateTip(ctx context.Context, provider rpc.RPCProvider) (rpc.U64, error) {
	return rpc.EstimateTip(ctx, responseFlagsProvider{provider}, constants.TipMultiplier)
}
