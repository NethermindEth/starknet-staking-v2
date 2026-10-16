package signer

import (
	"context"
	"errors"
	"fmt"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
	"go.uber.org/zap"
)

// Layout of the `get_staker_info_v1` response, an `Option<StakerInfoV1>`:
// https://github.com/starkware-libs/starknet-staking/blob/main/docs/spec.md#stakerinfov1
const (
	stakerInfoRewardAddressIndex = 1
	stakerInfoUnstakeTimeIndex   = 3
)

var (
	errFeeExceedsClaimMaxFee = errors.New("estimated fee is greater than claim max fee")
	errResponseTooShort      = errors.New("response is too short")
)

// RewardSigner is a signer that can claim rewards from the staking contract.
// It is used for the validator to claim rewards automatically, an optional feature
// that can be enabled by setting the corresponding flags in the config.
//
//go:generate go tool mockgen -destination=../../mocks/mock_reward_signer.go -package=mocks github.com/NethermindEth/starknet-staking-v2/validator/signer RewardSigner
type RewardSigner interface {
	Address() *types.Address
	ClaimRewards()
}

// rewardSigner is the implementation of the RewardSigner interface.
//
//go:generate go tool mockgen -destination=../../mocks/mock_rpc_provider.go -package=mocks github.com/NethermindEth/starknet.go/rpc RPCProvider
type rewardSigner struct {
	ctx           context.Context
	logger        log.Logger
	signer        Signer
	provider      rpc.RPCProvider
	stakerAddress *types.Address

	// Max fee, in FRI, to pay for the claim transaction
	claimMaxFee felt.Felt
	// Amount of unclaimed rewards, in STRK, that triggers a claim
	claimThreshold uint64
}

var _ RewardSigner = (*rewardSigner)(nil)

// The relevant fields of the staker info returned by the staking contract
type stakerInfo struct {
	rewardAddress    felt.Felt
	unclaimedRewards felt.Felt
}

// Creates a new RewardSigner and verifies that the configured reward account is allowed
// to claim the staker rewards, i.e. it's either the staker address or its reward address.
func NewRewardSigner(
	ctx context.Context,
	provider rpc.RPCProvider,
	logger log.Logger,
	rewardConfig *config.Reward,
	contractAddresses *config.ContractAddresses,
	stakerAddress *types.Address,
	braavos bool,
	isExternal bool,
) (RewardSigner, error) {
	signerConf := makeSignerConfFromRewardConf(rewardConfig)

	var signer Signer

	if isExternal {
		externalSigner, err := NewExternalSigner(
			ctx, provider, logger, &signerConf, contractAddresses, braavos,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create external signer: %w", err)
		}
		signer = &externalSigner
	} else {
		internalSigner, err := NewInternalSigner(
			ctx, provider, logger, &signerConf, contractAddresses, braavos,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create internal signer: %w", err)
		}
		signer = &internalSigner
	}

	rs := &rewardSigner{
		ctx:            ctx,
		logger:         logger,
		signer:         signer,
		provider:       provider,
		stakerAddress:  stakerAddress,
		claimMaxFee:    felt.FromUint64[felt.Felt](rewardConfig.ClaimMaxFee),
		claimThreshold: rewardConfig.ClaimThreshold,
	}

	if err := rs.checkRewardAddress(); err != nil {
		return nil, fmt.Errorf("reward address check: %w", err)
	}

	return rs, nil
}

func makeSignerConfFromRewardConf(rewardConfig *config.Reward) config.Signer {
	return config.Signer{
		ExternalURL:        rewardConfig.ExternalURL,
		PrivKey:            rewardConfig.PrivKey,
		OperationalAddress: rewardConfig.Address,
	}
}

func (rs *rewardSigner) Address() *types.Address {
	return rs.signer.Address()
}

// Only the staker address or its reward address are allowed to claim the staker rewards
func (rs *rewardSigner) checkRewardAddress() error {
	info, err := rs.fetchStakerInfo()
	if err != nil {
		return fmt.Errorf("failed to fetch staker info: %w", err)
	}

	address := rs.Address().Felt()
	if !address.Equal(rs.stakerAddress.Felt()) && !address.Equal(&info.rewardAddress) {
		return fmt.Errorf(
			"the configured reward account is not the real reward address nor the staker address:\n"+
				"configured reward account: %s\n"+
				"real reward address: %s\n"+
				"staker address: %s",
			address.String(),
			&info.rewardAddress,
			rs.stakerAddress,
		)
	}

	return nil
}

// ClaimRewards claims rewards if the unclaimed rewards amount reaches the claim threshold.
// All errors and state changes are logged, the feature does not stop the flow.
func (rs *rewardSigner) ClaimRewards() {
	pref := "Auto-Claiming Rewards: "
	rs.logger.Debug(pref + "starting claim rewards")

	info, err := rs.fetchStakerInfo()
	if err != nil {
		rs.logger.Error(pref+"failed to get unclaimed rewards amount", zap.Error(err))

		return
	}

	// The contract returns the unclaimed rewards in FRI
	unclaimedRewardsStrk := utils.FRIToSTRK(&info.unclaimedRewards)
	if utils.STRKToFRI(float64(rs.claimThreshold)).Cmp(&info.unclaimedRewards) > 0 {
		rs.logger.Debug(
			pref+"unclaimed rewards amount is below claim threshold; skipping claim...",
			zap.Float64("unclaimed rewards (STRK)", unclaimedRewardsStrk),
			zap.Uint64("claim threshold (STRK)", rs.claimThreshold),
		)

		return
	}

	rs.logger.Info(
		pref+"rewards amount reached claim threshold; claiming rewards...",
		zap.Float64("unclaimed rewards (STRK)", unclaimedRewardsStrk),
	)

	txn, err := rs.buildClaimRewardTransaction()
	if err != nil {
		rs.logger.Error(pref+"failed to build claim reward transaction", zap.Error(err))

		return
	}

	overallFee, err := rs.populateFees(&txn)
	if err != nil {
		if errors.Is(err, errFeeExceedsClaimMaxFee) {
			rs.logger.Warn(
				pref+"estimated fee exceeds claim max fee; skipping claim...",
				zap.String("overall fee", overallFee.String()),
				zap.String("claim max fee", rs.claimMaxFee.String()),
			)

			return
		}
		rs.logger.Error(pref+"failed to populate fees", zap.Error(err))

		return
	}

	resp, err := rs.provider.AddInvokeTransaction(rs.ctx, &txn)
	if err != nil {
		rs.logger.Error(pref+"failed to send reward claim transaction", zap.Error(err))

		return
	}

	rs.logger.Info(
		pref+"reward claim transaction sent successfully",
		zap.String("transaction hash", resp.Hash.String()),
	)
}

func (rs *rewardSigner) fetchStakerInfo() (stakerInfo, error) {
	functionCall := rpc.FunctionCall{
		ContractAddress: rs.signer.ValidationContracts().Staking.Felt(),
		EntryPointSelector: utils.GetSelectorFromNameFelt(
			"get_staker_info_v1",
		),
		Calldata: []*felt.Felt{rs.stakerAddress.Felt()},
	}

	resp, err := rs.signer.Call(functionCall, rpc.WithBlockTag(rpc.BlockTagLatest))
	if err != nil {
		return stakerInfo{}, fmt.Errorf("calling 'get_staker_info_v1': %w", err)
	}

	result, err := rs.decodeGetStakerInfoV1(resp)
	if err != nil {
		return stakerInfo{}, fmt.Errorf("decoding 'get_staker_info_v1': %w", err)
	}

	return result, nil
}

// Decodes the `Option<StakerInfoV1>` returned by `get_staker_info_v1`.
// https://github.com/starkware-libs/starknet-staking/blob/main/docs/spec.md#stakerinfov1
func (rs *rewardSigner) decodeGetStakerInfoV1(resp []*felt.Felt) (stakerInfo, error) {
	if len(resp) == 0 {
		return stakerInfo{}, errors.New("response is empty")
	}

	isStaker, err := isOptionSome(resp[0])
	if err != nil {
		return stakerInfo{}, fmt.Errorf("staker info: %w", err)
	}
	if !isStaker {
		return stakerInfo{}, fmt.Errorf(
			"staker address %s not found by the staking contract",
			rs.stakerAddress,
		)
	}

	if len(resp) <= stakerInfoUnstakeTimeIndex {
		return stakerInfo{}, errResponseTooShort
	}

	isUnstaking, err := isOptionSome(resp[stakerInfoUnstakeTimeIndex])
	if err != nil {
		return stakerInfo{}, fmt.Errorf("unstake time: %w", err)
	}

	// `amount_own` follows `unstake_time`, which holds an extra felt (the timestamp
	// seconds) when it is `Some`. `unclaimed_rewards_own` follows `amount_own`.
	amountOwnIndex := stakerInfoUnstakeTimeIndex + 1
	if isUnstaking {
		amountOwnIndex++
	}
	unclaimedRewardsIndex := amountOwnIndex + 1
	if len(resp) <= unclaimedRewardsIndex {
		return stakerInfo{}, errResponseTooShort
	}

	return stakerInfo{
		rewardAddress:    *resp[stakerInfoRewardAddressIndex],
		unclaimedRewards: *resp[unclaimedRewardsIndex],
	}, nil
}

// Returns whether a serialised Cairo `Option` variant is `Some` (0) or `None` (1)
func isOptionSome(variant *felt.Felt) (bool, error) {
	switch {
	case variant.IsZero():
		return true, nil
	case variant.IsOne():
		return false, nil
	default:
		return false, fmt.Errorf("invalid Option variant %s", variant)
	}
}

func (rs *rewardSigner) buildClaimRewardTransaction() (rpc.BroadcastInvokeTxnV3, error) {
	invokeCall := []rpc.InvokeFunctionCall{{
		ContractAddress: rs.signer.ValidationContracts().Staking.Felt(),
		FunctionName:    "claim_rewards",
		CallData:        []*felt.Felt{rs.stakerAddress.Felt()},
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
	claimTransaction := rpc.BroadcastInvokeTxnV3{
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

	_, err = rs.signer.SignTransaction(&claimTransaction)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, fmt.Errorf("failed to sign transaction: %w", err)
	}

	return claimTransaction, nil
}

// populateFees estimates the fee and populates the transaction with the fee.
// Returns the estimated overall fee and an error.
// If the estimated fee exceeds the claim max fee, [errFeeExceedsClaimMaxFee] is returned
// alongside the estimated fee, and the transaction is left untouched.
func (rs *rewardSigner) populateFees(txn *rpc.BroadcastInvokeTxnV3) (felt.Felt, error) {
	estimate, err := rs.signer.EstimateFee(txn)
	if err != nil {
		return felt.Felt{}, fmt.Errorf("failed to estimate fee: %w", err)
	}
	overallFee := *estimate.OverallFee

	if rs.claimMaxFee.Cmp(&overallFee) < 0 {
		return overallFee, errFeeExceedsClaimMaxFee
	}

	txn.ResourceBounds = utils.FeeEstToResBoundsMap(estimate, constants.FeeEstimationMultiplier)

	// patch for making sure txn.Version is correct
	txn.Version = rpc.TransactionV3

	_, err = rs.signer.SignTransaction(txn)
	if err != nil {
		return overallFee, fmt.Errorf("failed to sign the transaction: %w", err)
	}

	return overallFee, nil
}
