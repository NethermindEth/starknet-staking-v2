package validator

import (
	"math"

	junoUtils "github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
)

func CheckBalance[S signerP.Signer](
	signer S, threshold float64, logger *junoUtils.ZapLogger, tracer metrics.Tracer,
) {
	// call the stark token balance based on the signer address
	// record the balance
	// give a warning if below certain threshold (optional)
	logger.Debugf("Calling balance of %s", signer.Address())
	balanceWei, err := signerP.FetchValidatorBalance(signer)
	if err != nil {
		logger.Warnf("Unable to get STRK balance of account %s: %s", signer.Address(), err.Error())

		return
	}
	balance := balanceWei.Strk()
	logger.Infow(
		"Account balance",
		"address", signer.Address(),
		"STRK", balance,
		"WEI", balanceWei.Text(10), //nolint:mnd // Decimal base
	)

	if math.IsInf(balance, 1) {
		logger.Debugf(
			"Signer STRK balance value cannot be represented as a float64. Setting value to %d",
			balance,
		)
	} else if math.IsInf(balance, -1) || math.IsNaN(balance) {
		logger.Error(
			"Unexpected balance conversion value from WEI: %s to STRK: %f",
			balanceWei.Text(10), //nolint:mnd // Decimal base
			balance,
		)

		return
	}
	tracer.UpdateSignerBalance(balance)

	if balance <= threshold {
		logger.Warnf("Balance below threshold: %f <= %f", balance, threshold)
		tracer.RecordSignerBalanceBelowThreshold()
	} else {
		tracer.RecordSignerBalanceAboveThreshold()
	}
}
