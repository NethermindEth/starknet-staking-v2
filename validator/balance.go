package validator

import (
	"math"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"go.uber.org/zap"
)

func CheckBalance[S signerP.Signer](
	signer S, threshold float64, logger log.Logger, tracer metrics.Tracer,
) {
	// call the stark token balance based on the signer address
	// record the balance
	// give a warning if below certain threshold (optional)
	logger.Debug("Calling account balance", zap.String("address", signer.Address().String()))
	balanceWei, err := signerP.FetchValidatorBalance(signer)
	if err != nil {
		logger.Warn(
			"Unable to get STRK balance of account",
			zap.String("address", signer.Address().String()),
			zap.Error(err),
		)

		return
	}
	balance := balanceWei.Strk()
	logger.Info(
		"Account balance",
		zap.String("address", signer.Address().String()),
		zap.Float64("STRK", balance),
		zap.String("WEI", balanceWei.Text(10)), //nolint:mnd // Decimal base
	)

	if math.IsInf(balance, 1) {
		logger.Debug(
			"Signer STRK balance value cannot be represented as a float64, using +Inf",
			zap.Float64("balance", balance),
		)
	} else if math.IsInf(balance, -1) || math.IsNaN(balance) {
		logger.Error(
			"Unexpected balance conversion value from WEI to STRK",
			zap.String("WEI", balanceWei.Text(10)), //nolint:mnd // Decimal base
			zap.Float64("STRK", balance),
		)

		return
	}
	tracer.UpdateSignerBalance(balance)

	if balance <= threshold {
		logger.Warn(
			"Balance below threshold",
			zap.Float64("balance", balance),
			zap.Float64("threshold", threshold),
		)
		tracer.RecordSignerBalanceBelowThreshold()
	} else {
		tracer.RecordSignerBalanceAboveThreshold()
	}
}
