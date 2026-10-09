package signer

import (
	"context"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
)

type RewardSigner struct {
	signer Signer
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

	return &RewardSigner{signer: &internalSigner}, nil
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

	return &RewardSigner{signer: &externalSigner}, nil
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
