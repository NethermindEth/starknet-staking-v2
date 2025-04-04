package main

import (
	"context"

	"github.com/NethermindEth/starknet.go/rpc"
)

// Returns a new Starknet.Go RPC Provider
func NewProvider[Log Logger](providerUrl string, logger Log) *rpc.Provider {
	// TODO: test with a not connected url ! I think the creation works but after it might fail...
	// If so, then add a sanity check call to ChainID() for example
	provider, err := rpc.NewProvider(providerUrl)
	if err != nil {
		logger.Fatalf("Error connecting to RPC provider at %s: %s", providerUrl, err)
	}

	logger.Infow("Successfully connected to RPC provider", "providerUrl", "*****")
	return provider
}

// Returns a Go channel where BlockHeaders are received
func BlockHeaderSubscription[Log Logger](wsProviderUrl string, logger Log) (
	*rpc.WsProvider, chan *rpc.BlockHeader,
) {
	// Initialize connection to WS provider
	wsProvider, err := rpc.NewWebsocketProvider(wsProviderUrl)
	if err != nil {
		logger.Fatalf("Error dialing the WS provider at %s: %s", wsProviderUrl, err)
	}

	headersFeed := make(chan *rpc.BlockHeader)
	clientSubscription, err := wsProvider.SubscribeNewHeads(context.Background(), headersFeed, rpc.BlockID{Tag: "latest"})
	if err != nil {
		logger.Fatalf("Error subscribing to new block headers: %s", err)
	}

	logger.Infow("Successfully subscribed to new block headers", "Subscription ID", clientSubscription.ID())
	return wsProvider, headersFeed
}
