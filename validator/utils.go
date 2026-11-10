package validator

import (
	"fmt"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"go.uber.org/zap"
)

// skipCaller returns a logger that attributes log lines to the caller of the
// helper function using it, rather than to the helper itself. Loggers that are
// not *log.ZapLogger are returned unchanged.
func skipCaller(logger log.Logger) log.Logger {
	if zl, ok := logger.(*log.ZapLogger); ok {
		return zl.WithOptions(zap.AddCallerSkip(1))
	}

	return logger
}

func logNewEpoch(
	epochInfo *types.EpochInfo,
	attestInfo *types.AttestInfo,
	logger log.Logger,
) {
	logger = skipCaller(logger)
	logger.Info(
		"epoch started",
		zap.Uint64("epochID", epochInfo.EpochID),
		zap.Uint64("epochLength", epochInfo.EpochLen),
		zap.Uint64("startBlock", epochInfo.StartingBlock.Uint64()),
		zap.Uint64("endBlock", epochInfo.StartingBlock.Uint64()+epochInfo.EpochLen-1),
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
	logger = skipCaller(logger)
	base := fmt.Sprintf("block %d received", blockNum)
	var suffix string
	switch {
	case blockNum < attestInfo.WindowStart.Uint64():
		suffix = blockCount(uint64(attestInfo.WindowStart)-blockNum) + " to attest"
	case blockNum < attestInfo.WindowEnd.Uint64():
		suffix = blockCount(uint64(attestInfo.WindowEnd)-blockNum) + " before end of window"
	default:
		suffix = blockCount(
			uint64(epochInfo.StartingBlock)+epochInfo.EpochLen-blockNum,
		) + " for the next epoch"
	}

	logger.Info(base+", "+suffix, zap.Uint64("blockNumber", blockNum))
}

func blockCount(blocks uint64) string {
	if blocks == 1 {
		return "1 block"
	}

	return fmt.Sprintf("%d blocks", blocks)
}
