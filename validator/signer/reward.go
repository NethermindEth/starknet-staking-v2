package signer

import (
	"context"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
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
