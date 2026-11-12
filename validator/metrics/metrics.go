package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

var _ Tracer = (*Metrics)(nil)

// Metrics represents the metrics server for the validator
const (
	networkLabel = "network"
	accountLabel = "account"
)

type Metrics struct {
	server                          *http.Server
	logger                          log.Logger
	network                         string
	registry                        *prometheus.Registry
	latestBlockNumber               *prometheus.GaugeVec
	currentEpochID                  *prometheus.GaugeVec
	currentEpochLength              *prometheus.GaugeVec
	currentEpochStartingBlockNumber *prometheus.GaugeVec
	currentEpochAssignedBlockNumber *prometheus.GaugeVec
	lastAttestationTimestamp        *prometheus.GaugeVec
	attestationSubmittedCount       *prometheus.CounterVec
	attestationFailureCount         *prometheus.CounterVec
	attestationConfirmedCount       *prometheus.CounterVec
	epochInfoFetchFailureCount      *prometheus.CounterVec
	signerBalance                   *prometheus.GaugeVec
	signerBalanceBelowThreshold     *prometheus.GaugeVec
	balanceCheckFailureCount        *prometheus.CounterVec
	rewardBalance                   *prometheus.GaugeVec
	rewardBalanceBelowThreshold     *prometheus.GaugeVec
	rewardUnclaimed                 *prometheus.GaugeVec
	rewardClaimSubmittedCount       *prometheus.CounterVec
	rewardClaimConfirmedCount       *prometheus.CounterVec
	rewardClaimFailureCount         *prometheus.CounterVec
	rewardClaimMaxFeeExceededCount  *prometheus.CounterVec
	lastRewardClaimTimestamp        *prometheus.GaugeVec
}

// NewMetrics creates a new metrics server
//
//nolint:funlen // Long due to the metric declarations
func NewMetrics(serverAddress, chainID string, logger log.Logger) *Metrics {
	registry := prometheus.NewRegistry()

	//nolint:exhaustruct_v5,lll // Only specifying used fields // We can't break the lines since it'll show in the output
	m := &Metrics{
		logger:   logger,
		network:  chainID,
		registry: registry,
		latestBlockNumber: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "validator_attestation_starknet_latest_block_number",
				Help: "The latest block number seen by the validator on the Starknet network",
			},
			[]string{networkLabel},
		),
		currentEpochID: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "validator_attestation_current_epoch_id",
				Help: "The ID of the current epoch the validator is participating in",
			},
			[]string{networkLabel},
		),
		currentEpochLength: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "validator_attestation_current_epoch_length",
				Help: "The total length (in blocks) of the current epoch",
			},
			[]string{networkLabel},
		),
		currentEpochStartingBlockNumber: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "validator_attestation_current_epoch_starting_block_number",
				Help: "The first block number of the current epoch",
			},
			[]string{networkLabel},
		),
		currentEpochAssignedBlockNumber: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "validator_attestation_current_epoch_assigned_block_number",
				Help: "The specific block number within the current epoch for which the validator is assigned to attest",
			},
			[]string{networkLabel},
		),
		lastAttestationTimestamp: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "validator_attestation_last_attestation_timestamp_seconds",
				Help: "The Unix timestamp (in seconds) of the last successful attestation submission",
			},
			[]string{networkLabel},
		),
		attestationSubmittedCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "validator_attestation_attestation_submitted_count",
				Help: "The total number of attestations submitted by the validator since startup",
			},
			[]string{networkLabel},
		),
		attestationFailureCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "validator_attestation_attestation_failure_count",
				Help: "The total number of attestation transaction submission failures encountered by the validator since startup",
			},
			[]string{networkLabel},
		),
		attestationConfirmedCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "validator_attestation_attestation_confirmed_count",
				Help: "The total number of attestations that have been confirmed on the network since validator startup",
			},
			[]string{networkLabel},
		),
		epochInfoFetchFailureCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "validator_attestation_epoch_info_fetch_failure_count",
				Help: "The total number of failed attempts to fetch the epoch info, at startup or at an epoch switch, since startup. Block headers are not processed while it fails",
			},
			[]string{networkLabel},
		),
		signerBalance: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "validator_attestation_signer_balance",
				Help: "The balance of the account that signs the attestation after each attest transaction",
			},
			[]string{networkLabel},
		),
		signerBalanceBelowThreshold: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "validator_attestation_signer_below_threshold",
				Help: "Set to one if the account that signs the attestation has it's balance below certain threshold",
			},
			[]string{networkLabel},
		),
		balanceCheckFailureCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "validator_attestation_balance_check_failure_count",
				Help: "The total number of balance checks that could not read the account balance since startup, by account (signer or reward)",
			},
			[]string{networkLabel, accountLabel},
		),
		rewardBalance: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "validator_attestation_reward_balance",
				Help: "The balance of the reward account, checked after each attestation window",
			},
			[]string{networkLabel},
		),
		rewardBalanceBelowThreshold: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "validator_attestation_reward_below_threshold",
				Help: "Set to one if the reward account has it's balance below certain threshold",
			},
			[]string{networkLabel},
		),
		rewardUnclaimed: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "validator_attestation_reward_unclaimed",
				Help: "The staker's unclaimed rewards, in STRK, checked after each attestation window",
			},
			[]string{networkLabel},
		),
		rewardClaimSubmittedCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "validator_attestation_reward_claim_submitted_count",
				Help: "The total number of reward claim transactions submitted by the validator since startup",
			},
			[]string{networkLabel},
		),
		rewardClaimConfirmedCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "validator_attestation_reward_claim_confirmed_count",
				Help: "The total number of reward claim transactions accepted on L2 with a successful execution since startup",
			},
			[]string{networkLabel},
		),
		rewardClaimFailureCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "validator_attestation_reward_claim_failure_count",
				Help: "The total number of reward claim failures encountered by the validator since startup",
			},
			[]string{networkLabel},
		),
		rewardClaimMaxFeeExceededCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "validator_attestation_reward_claim_max_fee_exceeded_count",
				Help: "The total number of reward claims skipped since startup because the estimated fee exceeded the claim max fee",
			},
			[]string{networkLabel},
		),
		lastRewardClaimTimestamp: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "validator_attestation_reward_last_claim_timestamp_seconds",
				Help: "The Unix timestamp (in seconds) of the last successful reward claim submission",
			},
			[]string{networkLabel},
		),
	}

	// Register metrics with Prometheus registry
	registry.MustRegister(
		m.latestBlockNumber,
		m.currentEpochID,
		m.currentEpochLength,
		m.currentEpochStartingBlockNumber,
		m.currentEpochAssignedBlockNumber,
		m.lastAttestationTimestamp,
		m.attestationSubmittedCount,
		m.attestationFailureCount,
		m.attestationConfirmedCount,
		m.epochInfoFetchFailureCount,
		m.signerBalance,
		m.signerBalanceBelowThreshold,
		m.balanceCheckFailureCount,
		m.rewardBalance,
		m.rewardBalanceBelowThreshold,
		m.rewardUnclaimed,
		m.rewardClaimSubmittedCount,
		m.rewardClaimConfirmedCount,
		m.rewardClaimFailureCount,
		m.rewardClaimMaxFeeExceededCount,
		m.lastRewardClaimTimestamp,
	)

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("OK"))
		if err != nil {
			m.logger.Error("failed to write health check response", zap.Error(err))
		}
	})
	//nolint:exhaustruct_v5 // Using default values
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	//nolint:exhaustruct_v5 // Only specifying used fields
	m.server = &http.Server{
		Addr:              serverAddress,
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return m
}

// Start starts the metrics server
func (m *Metrics) Start() error {
	m.logger.Info("Starting metrics server", zap.String("URL", m.server.Addr))

	return m.server.ListenAndServe()
}

// Stop stops the metrics server
func (m *Metrics) Stop(ctx context.Context) error {
	m.logger.Info("Stopping metrics server")

	return m.server.Shutdown(ctx)
}

// UpdateLatestBlockNumber updates the latest block number metric
func (m *Metrics) UpdateLatestBlockNumber(blockNumber uint64) {
	m.logger.Debug("UpdateLatestBlockNumber", zap.Uint64("blockNumber", blockNumber))
	m.latestBlockNumber.WithLabelValues(m.network).Set(float64(blockNumber))
}

// UpdateEpochInfo updates the epoch-related metrics
func (m *Metrics) UpdateEpochInfo(epochInfo *types.EpochInfo, targetBlock uint64) {
	m.logger.Debug(
		"UpdateEpochInfo",
		zap.Uint64("epochID", epochInfo.EpochID),
		zap.Uint64("epochLength", epochInfo.EpochLen),
		zap.Uint64("epochStartingBlock", epochInfo.StartingBlock.Uint64()),
		zap.Uint64("targetBlock", targetBlock),
	)
	m.currentEpochID.WithLabelValues(m.network).Set(float64(epochInfo.EpochID))
	m.currentEpochLength.WithLabelValues(m.network).Set(float64(epochInfo.EpochLen))
	m.currentEpochStartingBlockNumber.
		WithLabelValues(m.network).
		Set(float64(epochInfo.StartingBlock.Uint64()))
	m.currentEpochAssignedBlockNumber.WithLabelValues(m.network).Set(float64(targetBlock))
}

// UpdateSignerBalance sets the signer account balance
func (m *Metrics) UpdateSignerBalance(balance float64) {
	m.logger.Debug("UpdateSignerBalance", zap.Float64("balance", balance))
	m.signerBalance.WithLabelValues(m.network).Set(balance)
}

// RecordAttestationSubmitted increments the attestation submitted counter
func (m *Metrics) RecordAttestationSubmitted() {
	m.logger.Debug("RecordAttestationSubmitted")
	m.attestationSubmittedCount.WithLabelValues(m.network).Inc()
	m.lastAttestationTimestamp.WithLabelValues(m.network).Set(float64(time.Now().Unix()))
}

// RecordAttestationFailure increments the attestation failure counter
func (m *Metrics) RecordAttestationFailure() {
	m.logger.Debug("RecordAttestationFailure")
	m.attestationFailureCount.WithLabelValues(m.network).Inc()
}

// RecordAttestationConfirmed increments the attestation confirmed counter
func (m *Metrics) RecordAttestationConfirmed() {
	m.logger.Debug("RecordAttestationConfirmed")
	m.attestationConfirmedCount.WithLabelValues(m.network).Inc()
}

func (m *Metrics) RecordEpochInfoFetchFailure() {
	m.logger.Debug("RecordEpochInfoFetchFailure")
	m.epochInfoFetchFailureCount.WithLabelValues(m.network).Inc()
}

// RecordSignerBalanceAboveThreshold sets the value to 0
func (m *Metrics) RecordSignerBalanceAboveThreshold() {
	m.logger.Debug("RecordSignerBalanceAboveThreshold")
	m.signerBalanceBelowThreshold.WithLabelValues(m.network).Set(0)
}

// RecordSignerBalanceBelowThreshold sets the value to 1
func (m *Metrics) RecordSignerBalanceBelowThreshold() {
	m.logger.Debug("RecordSignerBalanceBelowThreshold")
	m.signerBalanceBelowThreshold.WithLabelValues(m.network).Set(1)
}

func (m *Metrics) RecordBalanceCheckFailure(account string) {
	m.logger.Debug("RecordBalanceCheckFailure", zap.String("account", account))
	m.balanceCheckFailureCount.WithLabelValues(m.network, account).Inc()
}

// UpdateRewardBalance sets the reward account balance
func (m *Metrics) UpdateRewardBalance(balance float64) {
	m.logger.Debug("UpdateRewardBalance", zap.Float64("balance", balance))
	m.rewardBalance.WithLabelValues(m.network).Set(balance)
}

// RecordRewardBalanceAboveThreshold sets the value to 0
func (m *Metrics) RecordRewardBalanceAboveThreshold() {
	m.logger.Debug("RecordRewardBalanceAboveThreshold")
	m.rewardBalanceBelowThreshold.WithLabelValues(m.network).Set(0)
}

// RecordRewardBalanceBelowThreshold sets the value to 1
func (m *Metrics) RecordRewardBalanceBelowThreshold() {
	m.logger.Debug("RecordRewardBalanceBelowThreshold")
	m.rewardBalanceBelowThreshold.WithLabelValues(m.network).Set(1)
}

// UpdateRewardUnclaimed sets the staker's unclaimed rewards amount, in STRK
func (m *Metrics) UpdateRewardUnclaimed(amount float64) {
	m.logger.Debug("UpdateRewardUnclaimed", zap.Float64("amount", amount))
	m.rewardUnclaimed.WithLabelValues(m.network).Set(amount)
}

// RecordRewardClaimSubmitted increments the reward claim submitted counter
func (m *Metrics) RecordRewardClaimSubmitted() {
	m.logger.Debug("RecordRewardClaimSubmitted")
	m.rewardClaimSubmittedCount.WithLabelValues(m.network).Inc()
	m.lastRewardClaimTimestamp.WithLabelValues(m.network).Set(float64(time.Now().Unix()))
}

func (m *Metrics) RecordRewardClaimConfirmed() {
	m.logger.Debug("RecordRewardClaimConfirmed")
	m.rewardClaimConfirmedCount.WithLabelValues(m.network).Inc()
}

// RecordRewardClaimFailure increments the reward claim failure counter
func (m *Metrics) RecordRewardClaimFailure() {
	m.logger.Debug("RecordRewardClaimFailure")
	m.rewardClaimFailureCount.WithLabelValues(m.network).Inc()
}

// RecordRewardClaimMaxFeeExceeded increments the counter of reward claims skipped because
// the estimated fee exceeded the claim max fee
func (m *Metrics) RecordRewardClaimMaxFeeExceeded() {
	m.logger.Debug("RecordRewardClaimMaxFeeExceeded")
	m.rewardClaimMaxFeeExceededCount.WithLabelValues(m.network).Inc()
}
