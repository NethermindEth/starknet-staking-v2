package feederbackup

import (
	"context"
	"fmt"
	"time"

	"github.com/NethermindEth/juno/clients/feeder"
	"github.com/NethermindEth/juno/starknet"
	junoUtils "github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
)

type Client = feeder.Client

// Feeder is a struct representing a feeder instance, used as a backup plan
// in case the node is not correctly syncing the chain.
type Feeder struct {
	client            *Client
	latestBlock       *starknet.Block
	latestBlockNumber uint64

	pollingTicker *time.Ticker
}

// Create a new Feeder instance using the feeder URL.
func NewFeeder(clientURL string, logger *junoUtils.ZapLogger) *Feeder {
	return &Feeder{ //nolint:exhaustruct // Using default values
		client: feeder.NewClient(clientURL).WithLogger(logger),
		// @todo I'll edit this to test. Remember to revert this before merging.
		pollingTicker: time.NewTicker(5 * time.Second),
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

// Fetch the latest block directly from the feeder and store
// it in the Feeder struct.
func (f *Feeder) Fetch(ctx context.Context) error {
	block, err := f.client.Block(ctx, "latest")
	if err != nil {
		return fmt.Errorf("failed to fetch latest block from feeder: %w", err)
	}
	f.latestBlock = block
	f.latestBlockNumber = block.Number

	return nil
}

func (f *Feeder) LatestBlockNumber() uint64 {
	return f.latestBlockNumber
}

func (f *Feeder) LatestBlock() *starknet.Block {
	return f.latestBlock
}

// Clean the latest block data from the Feeder struct.
// Does not reset the latest block number.
func (f *Feeder) CleanBlock() {
	f.latestBlock = nil
}

// Get the channel of the polling ticker.
// It should return every [constants.FeederPollingInterval] seconds.
func (f *Feeder) Tick() <-chan time.Time {
	return f.pollingTicker.C
}
