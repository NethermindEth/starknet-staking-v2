package validator

import (
	"context"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet.go/client"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
)

// Returns a new Starknet.Go RPC Provider
func NewProvider[Logger utils.Logger](
	ctx context.Context,
	providerURL string,
	logger Logger,
) (*rpc.Provider, error) {
	provider, err := rpc.NewProvider(ctx, providerURL)
	if err != nil {
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
