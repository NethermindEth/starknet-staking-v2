package validator

import (
	"math"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/metrics"
	signerP "github.com/NethermindEth/starknet-staking-v2/validator/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"go.uber.org/zap"
)

const (
	signerAccount = "signer"
	rewardAccount = "reward"
)

// Checks the STRK balance of the signer account and, if [rewardAddress] is not nil,
// of the reward account as well.
// Each balance is recorded and a warning is given if it is below the threshold
func CheckBalance[S signerP.Signer](
	signer S,
	rewardAddress *types.Address,
	threshold float64,
	logger log.Logger,
	tracer metrics.Tracer,
) {
	if balance, ok := fetchStrkBalance(signer, signerAccount, signer.Address(), logger); ok {
		tracer.UpdateSignerBalance(balance)
		if isBelowThreshold(signerAccount, balance, threshold, logger) {
			tracer.RecordSignerBalanceBelowThreshold()
		} else {
			tracer.RecordSignerBalanceAboveThreshold()
		}
	}

	if rewardAddress == nil {
		return
	}
	if balance, ok := fetchStrkBalance(signer, rewardAccount, rewardAddress, logger); ok {
		tracer.UpdateRewardBalance(balance)
		if isBelowThreshold(rewardAccount, balance, threshold, logger) {
			tracer.RecordRewardBalanceBelowThreshold()
		} else {
			tracer.RecordRewardBalanceAboveThreshold()
		}
	}
}

// Fetches the STRK balance of the given address. Returns false if the balance
// couldn't be fetched or has an unexpected value
func fetchStrkBalance[S signerP.Signer](
	signer S, account string, address *types.Address, logger log.Logger,
) (float64, bool) {
	logger.Debug(
		"Calling account balance",
		zap.String("account", account),
		zap.String("address", address.String()),
	)
	balanceWei, err := signerP.FetchBalance(signer, address)
	if err != nil {
		logger.Warn(
			"Unable to get STRK balance of account",
			zap.String("account", account),
			zap.String("address", address.String()),
			zap.Error(err),
		)

		return 0, false
	}
	balance := balanceWei.Strk()
	logger.Info(
		"Account balance",
		zap.String("account", account),
		zap.String("address", address.String()),
		zap.Float64("STRK", balance),
		zap.String("WEI", balanceWei.Text(10)), //nolint:mnd // Decimal base
	)

	if math.IsInf(balance, 1) {
		logger.Debug(
			"STRK balance value cannot be represented as a float64, using +Inf",
			zap.String("account", account),
			zap.Float64("balance", balance),
		)
	} else if math.IsInf(balance, -1) || math.IsNaN(balance) {
		logger.Error(
			"Unexpected balance conversion value from WEI to STRK",
			zap.String("account", account),
			zap.String("WEI", balanceWei.Text(10)), //nolint:mnd // Decimal base
			zap.Float64("STRK", balance),
		)

		return 0, false
	}

	return balance, true
}

func isBelowThreshold(account string, balance, threshold float64, logger log.Logger) bool {
	if balance > threshold {
		return false
	}
	logger.Warn(
		"Balance below threshold",
		zap.String("account", account),
		zap.Float64("balance", balance),
		zap.Float64("threshold", threshold),
	)

	return true
}
