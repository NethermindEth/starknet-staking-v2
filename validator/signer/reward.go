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
	braavos bool,
) (*RewardSigner, error) {
	zeroAddresses := emptyContractAddresses()
	signer := makeSignerFromRewardConfig(rewardConfig)

	internalSigner, err := NewInternalSigner(
		ctx, provider, logger, &signer, &zeroAddresses, braavos,
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
	braavos bool,
) (*RewardSigner, error) {
	zeroAddresses := emptyContractAddresses()
	signer := makeSignerFromRewardConfig(rewardConfig)

	externalSigner, err := NewExternalSigner(
		ctx, provider, logger, &signer, &zeroAddresses, braavos,
	)
	if err != nil {
		return nil, err
	}

	return &RewardSigner{signer: &externalSigner}, nil
}

func makeSignerFromRewardConfig(rewardConfig *config.Reward) config.Signer {
	return config.Signer{
		ExternalURL:        rewardConfig.ExternalURL,
		PrivKey:            rewardConfig.PrivKey,
		OperationalAddress: rewardConfig.Address,
	}
}

// Returns a [config.ContractAddresses] with all addresses set to 0x0.
// These addresses are required for the [Signer], but not for the reward feat.
func emptyContractAddresses() config.ContractAddresses {
	return config.ContractAddresses{
		Attest:  "0x0",
		Staking: "0x0",
	}
}

func (r *RewardSigner) Address() *types.Address {
	return r.signer.Address()
}
