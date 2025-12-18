package validator

import (
	"context"
	"fmt"
	"time"

	"github.com/NethermindEth/juno/clients/feeder"
	junoUtils "github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
)

type Client = feeder.Client

// Feeder is a struct representing a feeder instance, used to
// fetch the latest block from the feeder.
type Feeder struct {
	client            *Client
	latestBlockNumber uint64

	pollingTicker *time.Ticker
}

// Create a new Feeder instance using the feeder URL.
func NewFeeder(clientURL string, logger *junoUtils.ZapLogger) *Feeder {
	return &Feeder{
		client:            feeder.NewClient(clientURL).WithLogger(logger),
		pollingTicker:     time.NewTicker(constants.FeederPollingInterval * time.Second),
		latestBlockNumber: 0,
	}
}

// Create a new Feeder instance using the [types.ValidationContracts] type
// to determine the feeder URL.
func NewFeederFromContracts(
	contracts *types.ValidationContracts,
	logger *junoUtils.ZapLogger,
) *Feeder {
	switch contracts.Attest.String() {
	case constants.SepoliaAttestContractAddress:
		logger.Debug("creating feeder for sepolia")

		return NewFeeder(constants.SepoliaFeederURL, logger)
	case constants.MainnetAttestContractAddress:
		logger.Debug("creating feeder for mainnet")

		return NewFeeder(constants.MainnetFeederURL, logger)
	default:
		logger.Errorw("unknown attest contract address, defaulting to mainnet feeder",
			"address", contracts.Attest.String())

		return NewFeeder(constants.MainnetFeederURL, logger) // Default to mainnet feeder
	}
}

// Fetch the latest block number directly from the feeder and store
// it in the Feeder struct.
func (f *Feeder) Fetch(ctx context.Context) error {
	block, err := f.client.Block(ctx, "latest")
	if err != nil {
		return fmt.Errorf("failed to fetch latest block from feeder: %w", err)
	}
	f.latestBlockNumber = block.Number

	return nil
}

func (f *Feeder) LatestBlockNumber() uint64 {
	return f.latestBlockNumber
}

// Get the channel of the polling ticker.
// It should return every [constants.FeederPollingInterval] seconds.
func (f *Feeder) Tick() <-chan time.Time {
	return f.pollingTicker.C
}
