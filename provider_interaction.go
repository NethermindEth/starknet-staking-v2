package main

import (
	"context"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet.go/rpc"
)

// Returns a new Starknet.Go RPC Provider
func NewProvider[Logger utils.Logger](providerUrl string, logger Logger) *rpc.Provider {
	provider, err := rpc.NewProvider(providerUrl)
	if err != nil {
		logger.Fatalf("Error connecting to RPC provider at %s: %s", providerUrl, err)
	}

	logger.Infow("Successfully connected to RPC provider", "providerUrl", providerUrl)
	return provider
}

// Returns a Go channel where BlockHeaders are received
func BlockHeaderSubscription[Logger utils.Logger](providerUrl string, logger Logger) (
	*rpc.WsProvider, chan *rpc.BlockHeader,
) {
	// Take the providerUrl parts (host & port) and build the ws url
	wsProviderUrl := "ws://" + "localhost" + ":" + "6061" + "/v0_8"

	// Initialize connection to WS provider
	wsProvider, err := rpc.NewWebsocketProvider(wsProviderUrl)
	if err != nil {
		logger.Fatalf("Error dialing the WS provider: %s", err)
	}

	headersFeed := make(chan *rpc.BlockHeader)
	clientSubscription, err := wsProvider.SubscribeNewHeads(context.Background(), headersFeed, nil)
	if err != nil {
		logger.Fatalf("Error subscribing to new block headers: %s", err)
	}

	logger.Infow("Successfully subscribed to new block headers", "Subscription ID", clientSubscription.ID())
	return wsProvider, headersFeed
}
