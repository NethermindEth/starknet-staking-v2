package signer

import (
	"context"
	"fmt"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
)

type RewardSigner struct {
	ctx      context.Context
	signer   Signer
	provider rpc.RPCProvider
}

func NewInternalRewardSigner(
	ctx context.Context,
	provider *rpc.Provider,
	logger log.Logger,
	rewardConfig *config.Reward,
	contractAddresses *config.ContractAddresses,
	braavos bool,
) (*RewardSigner, error) {
	signer := makeSignerConfFromRewardConf(rewardConfig)

	internalSigner, err := NewInternalSigner(
		ctx, provider, logger, &signer, contractAddresses, braavos,
	)
	if err != nil {
		return nil, err
	}

	return &RewardSigner{
		ctx:      ctx,
		signer:   &internalSigner,
		provider: provider,
	}, nil
}

func NewExternalRewardSigner(
	ctx context.Context,
	provider *rpc.Provider,
	logger log.Logger,
	rewardConfig *config.Reward,
	contractAddresses *config.ContractAddresses,
	braavos bool,
) (*RewardSigner, error) {
	signer := makeSignerConfFromRewardConf(rewardConfig)

	externalSigner, err := NewExternalSigner(ctx, provider, logger, &signer, contractAddresses, braavos)
	if err != nil {
		return nil, err
	}

	return &RewardSigner{
		ctx:      ctx,
		signer:   &externalSigner,
		provider: provider,
	}, nil
}

func makeSignerConfFromRewardConf(rewardConfig *config.Reward) config.Signer {
	return config.Signer{
		ExternalURL:        rewardConfig.ExternalURL,
		PrivKey:            rewardConfig.PrivKey,
		OperationalAddress: rewardConfig.Address,
	}
}

func (r *RewardSigner) Address() *types.Address {
	return r.signer.Address()
}

func (r *RewardSigner) ClaimReward(ctx context.Context) error {

	return nil
}

func (r *RewardSigner) buildClaimRewardTransaction() (rpc.BroadcastInvokeTxnV3, error) {
	invokeCall := []rpc.InvokeFunctionCall{{
		ContractAddress: r.signer.ValidationContracts().Staking.Felt(),
		FunctionName:    "claim_reward",
		CallData:        []*felt.Felt{},
	}}
	call := utils.InvokeFuncCallsToFunctionCalls(invokeCall)
	calldata := account.FmtCallDataCairo2(call)
	defaultResources := makeDefaultResources()

	nonce, err := r.provider.Nonce(
		r.ctx,
		rpc.WithBlockTag(rpc.BlockTagPreConfirmed),
		r.Address().Felt(),
	)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, err
	}

	tip, err := rpc.EstimateTip(r.ctx, r.provider, constants.TipMultiplier)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, fmt.Errorf("failed to estimate tip: %w", err)
	}

	// Taken from starknet.go `utils.BuildInvokeTxn`
	attestTransaction := rpc.BroadcastInvokeTxnV3{
		Type:                  rpc.TransactionTypeInvoke,
		SenderAddress:         r.Address().Felt(),
		Calldata:              calldata,
		Version:               rpc.TransactionV3,
		Signature:             []*felt.Felt{},
		Nonce:                 nonce,
		ResourceBounds:        &defaultResources,
		Tip:                   tip,
		PayMasterData:         []*felt.Felt{},
		AccountDeploymentData: []*felt.Felt{},
		NonceDataMode:         rpc.DAModeL1,
		FeeMode:               rpc.DAModeL1,
		Proof:                 []string{},
		ProofFacts:            []*felt.Felt{},
	}

	return attestTransaction, nil
}
