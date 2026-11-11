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

func estimateTip(ctx context.Context, provider rpc.RPCProvider) (rpc.U64, error) {
	return rpc.EstimateTip(ctx, responseFlagsProvider{provider}, constants.TipMultiplier)
}
