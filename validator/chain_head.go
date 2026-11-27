package validator

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/NethermindEth/juno/blockchain/networks"
	"github.com/NethermindEth/juno/clients/feeder"
	"github.com/NethermindEth/juno/starknet"
	"github.com/NethermindEth/starknet.go/client/rpcerr"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
)

const (
	latestBlockID = "latest"
	// The feeder client waits between tries, so a host that cannot reach the feeder
	// gateway is not held for long
	feederMaxRetries = 2
)

// Feeder gateway URL by chain id. A variable so tests can use a local server
var FeederURLs = map[string]*url.URL{
	networks.Mainnet.L2ChainID: networks.Mainnet.FeederURL,
	networks.Sepolia.L2ChainID: networks.Sepolia.FeederURL,
}

// Reads block headers from the sequencer, which knows the chain head
type ChainHeadReader interface {
	BlockHeader(ctx context.Context, blockID string) (starknet.BlockHeader, error)
}

// Returns a reader of the chain head of the given chain, or nil when the chain has no
// known feeder gateway
func NewChainHeadReader(chainID string) ChainHeadReader {
	feederURL, ok := FeederURLs[chainID]
	if !ok {
		return nil
	}

	return feeder.NewClient(
		feederURL,
		// The feeder client changes the timeout of its HTTP client, which is the default
		// one unless another is given
		feeder.WithHTTPClient(new(http.Client)),
		feeder.WithMaxRetries(feederMaxRetries),
	)
}

var ErrChainHead = errors.New("feeder gateway request failed")

// Reads the number of the latest block of the node and the chain head reported by the
// sequencer. A failure to read the chain head is an `ErrChainHead`
func readSyncProgress(
	ctx context.Context, provider rpc.RPCProvider, chainHead ChainHeadReader,
) (nodeBlock, headBlock uint64, err error) {
	block, err := provider.BlockHashAndNumber(ctx)
	var rpcErr *rpcerr.RPCError
	switch {
	case err == nil:
		nodeBlock = block.Number
	case errors.As(err, &rpcErr) && rpcErr.Code == rpc.ErrNoBlocks.Code:
		// The node has no blocks yet, which counts as block 0
	default:
		return 0, 0, err
	}

	header, err := chainHead.BlockHeader(ctx, latestBlockID)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %w", ErrChainHead, err)
	}

	return nodeBlock, header.Number, nil
}
