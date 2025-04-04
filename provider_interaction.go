package main

import (
	"context"

	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
)

// Returns a new Starknet.Go RPC Provider
func NewProvider[Log Logger](providerUrl string, logger Log) (*rpc.Provider, error) {
	provider, err := rpc.NewProvider(providerUrl)
	if err != nil {
		return nil, errors.Errorf("Error creating RPC provider at %s: %s", providerUrl, err)
	}

	// Connection check
	_, err = provider.ChainID(context.Background())
	if err != nil {
		return nil, errors.Errorf("Error connecting to RPC provider at %s: %s", providerUrl, err)
	}

	logger.Infow("Successfully connected to RPC provider", "providerUrl", "*****")
	return provider, nil
}

// Returns a Go channel where BlockHeaders are received
func BlockHeaderSubscription[Log Logger](wsProviderUrl string, logger Log) (
	*rpc.WsProvider, chan *rpc.BlockHeader, error,
) {
	// Initialize connection to WS provider
	wsProvider, err := rpc.NewWebsocketProvider(wsProviderUrl)
	if err != nil {
		return nil, nil, errors.Errorf("Error dialing the WS provider at %s: %s", wsProviderUrl, err)
	}

	headersFeed := make(chan *rpc.BlockHeader)
	clientSubscription, err := wsProvider.SubscribeNewHeads(context.Background(), headersFeed, rpc.BlockID{Tag: "latest"})
	if err != nil {
		return nil, nil, errors.Errorf("Error subscribing to new block headers: %s", err)
	}

	logger.Infow("Successfully subscribed to new block headers", "Subscription ID", clientSubscription.ID())
	return wsProvider, headersFeed, nil
}
