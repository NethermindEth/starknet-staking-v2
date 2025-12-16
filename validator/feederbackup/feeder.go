package feederbackup

import (
	"context"
	"fmt"

	"github.com/NethermindEth/juno/clients/feeder"
	"github.com/NethermindEth/juno/starknet"
	junoUtils "github.com/NethermindEth/juno/utils"
)

type Client = feeder.Client

// Feeder is a struct representing a feeder instance, used as a backup plan
// in case the node is not correctly syncing the chain.
type Feeder struct {
	client            *Client
	latestBlock       *starknet.Block
	latestBlockNumber uint64
}

// Create a new Feeder instance.
func NewFeeder(clientURL string, logger *junoUtils.ZapLogger) *Feeder {
	return &Feeder{ //nolint:exhaustruct // Using default values
		client: feeder.NewClient(clientURL).WithLogger(logger),
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
