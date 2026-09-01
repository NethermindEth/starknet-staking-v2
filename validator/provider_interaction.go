package validator

import (
	"context"
	"slices"
	"strings"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet.go/client"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
)

// Starknet.Go only accepts the exact spec version it was built against, 0.10.2.
// 0.10.3 describes the same node API, so it is accepted too.
var supportedSpecVersions = []string{"0.10.2", "0.10.3"}

// Returns a new Starknet.Go RPC Provider
func NewProvider[Logger utils.Logger](
	ctx context.Context,
	providerURL string,
	logger Logger,
) (*rpc.Provider, error) {
	provider, err := rpc.NewProvider(ctx, providerURL)
	if errors.Is(err, rpc.ErrIncompatibleVersion) {
		nodeSpecVersion, specErr := provider.SpecVersion(ctx)
		if specErr != nil {
			return nil, errors.Errorf(
				"cannot read spec version of node at %s: %w", providerURL, specErr,
			)
		}
		if !slices.Contains(supportedSpecVersions, strings.TrimPrefix(nodeSpecVersion, "v")) {
			return nil, errors.Errorf(
				"node at %s implements JSON-RPC specification %s, this tool requires %s",
				providerURL, nodeSpecVersion, strings.Join(supportedSpecVersions, " or "),
			)
		}
		logger.Warnw(
			"node implements a different JSON-RPC specification than Starknet.Go targets",
			"providerUrl", providerURL,
			"nodeSpecVersion", nodeSpecVersion,
		)
	} else if err != nil {
		return nil, errors.Errorf("cannot create RPC provider at %s: %w", providerURL, err)
	}

	logger.Infof("connected to RPC at %s", providerURL)

	return provider, nil
}

// Returns a Go channel where BlockHeaders are received
func SubscribeToBlockHeaders[Logger utils.Logger](
	ctx context.Context,
	wsProviderURL string,
	logger Logger,
) (
	*rpc.WsProvider,
	chan *rpc.BlockHeader,
	*client.ClientSubscription,
	error,
) {
	logger.Debugw("initialising websocket connection", "wsProviderUrl", wsProviderURL)
	// This needs a timeout or something
	wsProvider, err := rpc.NewWebsocketProvider(ctx, wsProviderURL)
	if err != nil {
		return nil, nil, nil, errors.Errorf("dialling WS provider at %s: %w", wsProviderURL, err)
	}

	logger.Debugw("Subscribing to new block headers...")
	headersFeed := make(chan *rpc.BlockHeader)
	clientSubscription, err := wsProvider.SubscribeNewHeads(
		ctx, headersFeed, new(rpc.SubscriptionBlockID).WithLatestTag(),
	)
	if err != nil {
		return nil, nil, nil, errors.Errorf("subscribing to new block headers: %w", err)
	}

	logger.Infof("subscribed to new block header. Subscription ID: %s", clientSubscription.ID())

	return wsProvider, headersFeed, clientSubscription, nil
}
