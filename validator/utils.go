package validator

import (
	"fmt"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"go.uber.org/zap"
)

func logNewEpoch(
	epochInfo *types.EpochInfo,
	attestInfo *types.AttestInfo,
	logger log.Logger,
) {
	logger.Info(
		"epoch started",
		zap.Uint64("epochID", epochInfo.EpochID+1),
		zap.Uint64("epochLength", epochInfo.EpochLen),
		zap.Uint64("startBlock", epochInfo.StartingBlock.Uint64()),
		zap.Uint64("endBlock", epochInfo.StartingBlock.Uint64()+epochInfo.EpochLen),
	)
	logger.Info(
		"attest info",
		zap.Uint64("targetBlock", attestInfo.TargetBlock.Uint64()),
		zap.Uint64("windowStartBlock", attestInfo.WindowStart.Uint64()),
		zap.Uint64("windowEndBlock", attestInfo.WindowEnd.Uint64()),
	)
}

func logBlock(
	blockNum uint64,
	epochInfo *types.EpochInfo,
	attestInfo *types.AttestInfo,
	logger log.Logger,
) {
	base := fmt.Sprintf("block %d received", blockNum)
	var suffix string
	switch {
	case blockNum < attestInfo.WindowStart.Uint64():
		suffix = fmt.Sprintf(
			"%d blocks to attest", uint64(attestInfo.WindowStart)-blockNum,
		)
	case blockNum < attestInfo.WindowEnd.Uint64():
		suffix = fmt.Sprintf(
			"%d blocks before end of window",
			uint64(attestInfo.WindowEnd)-blockNum,
		)
	default:
		suffix = fmt.Sprintf(
			"%d blocks for the next epoch",
			uint64(epochInfo.StartingBlock)+epochInfo.EpochLen-blockNum,
		)
	}

	logger.Info(base + ", " + suffix)
}
