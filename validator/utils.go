package validator

import (
	"fmt"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
)

func logNewEpoch(
	epochInfo *types.EpochInfo,
	attestInfo *types.AttestInfo,
	logger *utils.ZapLogger,
) {
	logger.Infow(
		fmt.Sprintf("epoch %d started", epochInfo.EpochID+1),
		"epoch length", epochInfo.EpochLen,
		"start block", epochInfo.StartingBlock,
		"end block", epochInfo.StartingBlock+types.BlockNumber(epochInfo.EpochLen),
	)
	logger.Infow(
		"attest info",
		"target block", attestInfo.TargetBlock.Uint64(),
		"window start block", attestInfo.WindowStart.Uint64(),
		"window end block", attestInfo.WindowEnd.Uint64(),
	)
}

func logBlock(
	blockNum uint64,
	epochInfo *types.EpochInfo,
	attestInfo *types.AttestInfo,
	logger *utils.ZapLogger,
) {
	base := fmt.Sprintf("block %d received", blockNum)
	var suffix string
	if blockNum < attestInfo.WindowStart.Uint64() {
		suffix = fmt.Sprintf(
			"%d blocks to attest", uint64(attestInfo.WindowStart)-blockNum,
		)
	} else if blockNum < attestInfo.WindowEnd.Uint64() {
		suffix = fmt.Sprintf(
			"%d blocks before end of window",
			uint64(attestInfo.WindowEnd)-blockNum,
		)
	} else {
		suffix = fmt.Sprintf(
			"%d blocks for the next epoch",
			uint64(epochInfo.StartingBlock)+epochInfo.EpochLen-blockNum,
		)
	}

	logger.Info(base + ", " + suffix)
}
