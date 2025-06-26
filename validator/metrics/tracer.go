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
	RecordSignerBalanceAboveThreshold()
	RecordSignerBalanceBelowThreshold()
}
