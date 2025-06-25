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
	balance, err := signerP.FetchValidatorBalance(signer)
	if err != nil {
		logger.Warnf("Unable to get balance of account %s: %s", signer.Address(), err.Error())
		return
	}
	balanceF64 := balance.Float()
	logger.Infow(
		"Account balance",
		"address", signer.Address(),
		"STRK", balanceF64,
		"WEI", balance.Text(10),
	)

	if math.IsInf(balanceF64, 1) {
		logger.Debugf(
			"Signer balance value cannot be represented as a float64. Setting value to %d",
			balanceF64,
		)
	} else if math.IsInf(balanceF64, -1) || math.IsNaN(balanceF64) {
		logger.Error(
			"Unexpected balance conversion value from %s to %f",
			balance.Text(10),
			balanceF64,
		)
	}
	tracer.UpdateSignerBalance(balanceF64)

	if balanceF64 <= threshold {
		logger.Warnf("Balance below threshold: %f <= %f", balanceF64, threshold)
		tracer.RecordSignerBalanceBelowThreshold()
	} else {
		tracer.RecordSignerBalanceAboveThreshold()
	}
}
