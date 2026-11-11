package metrics

import "github.com/prometheus/client_golang/prometheus"

const (
	networkLabel = "network"
	accountLabel = "account"
)

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		latestBlockNumber: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "validator_attestation_starknet_latest_block_number",
				Help: "The latest block number seen by the validator",
			},
			[]string{"network"},
		),
		balanceCheckFailureCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "validator_attestation_balance_check_failure_count",
				Help: "Balance checks that failed, by account (signer or reward)",
			},
			[]string{networkLabel, accountLabel},
		),
		uptime: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "validator_attestation_uptime_seconds",
			Help: "Seconds since " + "startup",
		}),
	}

	registry.MustRegister(m.latestBlockNumber, m.balanceCheckFailureCount)
	registry.MustRegister(m.uptime)

	return m
}
