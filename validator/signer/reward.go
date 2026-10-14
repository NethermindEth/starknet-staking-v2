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
	ctx            context.Context
	signer         Signer
	provider       rpc.RPCProvider
	stakingAddress *types.Address
}

func NewRewardSigner(
	ctx context.Context,
	provider *rpc.Provider,
	logger log.Logger,
	rewardConfig *config.Reward,
	contractAddresses *config.ContractAddresses,
	stakingAddress *types.Address,
	braavos bool,
	isExternal bool,
) (*RewardSigner, error) {
	signerConf := makeSignerConfFromRewardConf(rewardConfig)

	var signer Signer

	if isExternal {
		externalSigner, err := NewExternalSigner(ctx, provider, logger, &signerConf, contractAddresses, braavos)
		if err != nil {
			return nil, fmt.Errorf("failed to create internal signer: %w", err)
		}
		signer = &externalSigner
	} else {
		internalSigner, err := NewInternalSigner(ctx, provider, logger, &signerConf, contractAddresses, braavos)
		if err != nil {
			return nil, fmt.Errorf("failed to create internal signer: %w", err)
		}
		signer = &internalSigner
	}
	return &RewardSigner{
		ctx:            ctx,
		signer:         signer,
		provider:       provider,
		stakingAddress: stakingAddress,
	}, nil
}

func makeSignerConfFromRewardConf(rewardConfig *config.Reward) config.Signer {
	return config.Signer{
		ExternalURL:        rewardConfig.ExternalURL,
		PrivKey:            rewardConfig.PrivKey,
		OperationalAddress: rewardConfig.Address,
	}
}

func (rs *RewardSigner) Address() *types.Address {
	return rs.signer.Address()
}

func (rs *RewardSigner) getUnclaimedRewardsAmount() (felt.Felt, error) {
	functionCall := rpc.FunctionCall{
		ContractAddress: rs.signer.ValidationContracts().Staking.Felt(),
		EntryPointSelector: utils.GetSelectorFromNameFelt(
			"get_staker_info_v1",
		),
		Calldata: []*felt.Felt{rs.stakingAddress.Felt()},
	}

	resp, err := rs.signer.Call(functionCall, rpc.WithBlockTag(rpc.BlockTagLatest))
	if err != nil {
		return felt.Felt{}, fmt.Errorf("calling 'get_staker_info_v1': %w", err)
	}

	result, err := rs.decode_get_staker_info_v1(resp)
	if err != nil {
		return felt.Felt{}, fmt.Errorf("decoding 'get_staker_info_v1': %w", err)
	}

	return result, nil
}

// https://github.com/starkware-libs/starknet-staking/blob/main/docs/spec.md#stakerinfov1
func (rs *RewardSigner) decode_get_staker_info_v1(resp []*felt.Felt) (felt.Felt, error) {
	var result felt.Felt

	if len(resp) == 0 {
		return result, fmt.Errorf("response is empty")
	}

	// #1 = enum. 0x0 is valid, 0x1 is not.
	if resp[0].IsOne() {
		return result, fmt.Errorf(
			"staker address %s not found by the staking contract",
			rs.stakingAddress,
		)
	}

	if len(resp) < 4 {
		return result, fmt.Errorf("response is too short")
	}

	// #2 = reward_address
	// #3 = operational_address

	var index int = 4
	// #4 = unstake_time
	if resp[3].IsZero() {
		// if 'unstake_time' is valid (0 value), there is a new field in the list: 'seconds'.
		// Increase the index by 1 to skip it.
		index++
	}

	// #5 = amount_own; skip it.
	index++
	// #6 = unclaimed_rewards_own
	if len(resp) < index+1 {
		return result, fmt.Errorf("response is too short")
	}
	result = *resp[index]

	return result, nil
}

func (rs *RewardSigner) claimReward() (
	rpc.AddInvokeTransactionResponse, error,
) {
	var resp rpc.AddInvokeTransactionResponse

	txn, err := rs.buildClaimRewardTransaction()
	if err != nil {
		return resp, fmt.Errorf("failed to build claim reward transaction: %w", err)
	}

	err = rs.populateFees(&txn)
	if err != nil {
		return resp, fmt.Errorf("failed to populate fees: %w", err)
	}

	resp, err = rs.provider.AddInvokeTransaction(rs.ctx, &txn)
	if err != nil {
		return resp, fmt.Errorf("failed to invoke transaction: %w", err)
	}

	return resp, nil
}

func (rs *RewardSigner) buildClaimRewardTransaction() (rpc.BroadcastInvokeTxnV3, error) {
	invokeCall := []rpc.InvokeFunctionCall{{
		ContractAddress: rs.signer.ValidationContracts().Staking.Felt(),
		FunctionName:    "claim_reward",
		CallData:        []*felt.Felt{rs.stakingAddress.Felt()},
	}}
	call := utils.InvokeFuncCallsToFunctionCalls(invokeCall)
	calldata := account.FmtCallDataCairo2(call)
	defaultResources := makeDefaultResources()

	nonce, err := rs.provider.Nonce(
		rs.ctx,
		rpc.WithBlockTag(rpc.BlockTagPreConfirmed),
		rs.Address().Felt(),
	)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, fmt.Errorf("failed to get nonce: %w", err)
	}

	tip, err := rpc.EstimateTip(rs.ctx, rs.provider, constants.TipMultiplier)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, fmt.Errorf("failed to estimate tip: %w", err)
	}

	// Taken from starknet.go `utils.BuildInvokeTxn`
	attestTransaction := rpc.BroadcastInvokeTxnV3{
		Type:                  rpc.TransactionTypeInvoke,
		SenderAddress:         rs.Address().Felt(),
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

	_, err = rs.signer.SignTransaction(&attestTransaction)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, fmt.Errorf("failed to sign transaction: %w", err)
	}

	return attestTransaction, nil
}

func (rs *RewardSigner) populateFees(txn *rpc.BroadcastInvokeTxnV3) error {
	estimate, err := rs.signer.EstimateFee(txn)
	if err != nil {
		return fmt.Errorf("failed to estimate fee: %w", err)
	}
	txn.ResourceBounds = utils.FeeEstToResBoundsMap(estimate, constants.FeeEstimationMultiplier)

	// patch for making sure txn.Version is correct
	txn.Version = rpc.TransactionV3

	_, err = rs.signer.SignTransaction(txn)
	if err != nil {
		return fmt.Errorf("failed to sign the transaction: %w", err)
	}

	return nil
}
