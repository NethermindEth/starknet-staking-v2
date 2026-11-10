package metrics

import (
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
)

type Tracer interface {
	UpdateLatestBlockNumber(blockNumber uint64)
	UpdateEpochInfo(epochInfo *types.EpochInfo, targetBlock uint64)
	UpdateSignerBalance(balance float64)
	RecordAttestationSubmitted()
	RecordAttestationFailure()
	RecordAttestationConfirmed()
	RecordEpochInfoFetchFailure()
	RecordSignerBalanceAboveThreshold()
	RecordSignerBalanceBelowThreshold()
	RecordBalanceCheckFailure(account string)
	UpdateRewardBalance(balance float64)
	RecordRewardBalanceAboveThreshold()
	RecordRewardBalanceBelowThreshold()
	UpdateRewardUnclaimed(amount float64)
	RecordRewardClaimSubmitted()
	RecordRewardClaimConfirmed()
	RecordRewardClaimFailure()
	RecordRewardClaimMaxFeeExceeded()
}
