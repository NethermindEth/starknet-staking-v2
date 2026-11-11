package main

import (
	"github.com/NethermindEth/juno/utils/log"
	configP "github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/spf13/cobra"
)

const logLevels = "trace, debug, info"

func NewCommand() cobra.Command {
	var configPath string
	var logLevelF string
	var metricsF bool
	var rewardBraavosF bool
	var balanceThreshold float64

	var config configP.Config
	var snConfig configP.StarknetConfig

	preRunE := func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("reward-braavos-account") {
			config.Reward.Braavos = &rewardBraavosF
		}

		return nil
	}

	cmd := cobra.Command{Use: "validator", PreRunE: preRunE}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to JSON config file")
	cmd.Flags().StringVar(&config.Provider.HTTP, "provider-http", "", "Provider http address")
	cmd.Flags().Uint64Var(
		&config.Reward.ClaimMaxFee,
		"reward-claim-max-fee",
		0,
		"(Optional) Max fee, in FRI, to pay for the claim transaction.\n"+
			"Defaults to 'unlimited' (~18.4 STRK)",
	)
	cmd.Flags().Uint64Var(
		&config.Reward.ClaimThreshold, "reward-claim-threshold", 0, "Claim threshold, in STRK",
	)
	cmd.Flags().BoolVar(
		&rewardBraavosF, "reward-braavos-account", false, "Set it for a Braavos reward account",
	)
	cmd.Flags().StringVar(
		&snConfig.ContractAddresses.Attest,
		"attest-contract-address",
		"",
		"Attestation contract address",
	)
	cmd.Flags().BoolVar(&metricsF, "metrics", false, "Enable metric tracking via Prometheus")
	cmd.Flags().Float64Var(
		&balanceThreshold,
		"balance-threshold",
		100.5,
		"Fee estimation uses version 1<<128 + 0x3 for *Braavos* | {x} [y] `z` a_b",
	)
	cmd.Flags().StringVar(&logLevelF, "log-level", log.INFO.String(), "Options: "+logLevels+".")

	return cmd
}
