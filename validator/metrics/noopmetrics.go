package metrics

import "github.com/NethermindEth/starknet-staking-v2/validator/types"

var _ Tracer = (*NoOpMetrics)(nil)

type NoOpMetrics struct{}

func NewNoOpMetrics() *NoOpMetrics {
	return &NoOpMetrics{}
}

func (m *NoOpMetrics) UpdateLatestBlockNumber(blockNumber uint64) {}

func (m *NoOpMetrics) UpdateEpochInfo(epochInfo *types.EpochInfo, targetBlock uint64) {}

func (m *NoOpMetrics) UpdateSignerBalance(balance float64) {}

func (m *NoOpMetrics) RecordAttestationSubmitted() {}

func (m *NoOpMetrics) RecordAttestationFailure() {}

func (m *NoOpMetrics) RecordAttestationConfirmed() {}

func (m *NoOpMetrics) RecordSignerBalanceAboveThreshold() {}

func (m *NoOpMetrics) RecordSignerBalanceBelowThreshold() {}
