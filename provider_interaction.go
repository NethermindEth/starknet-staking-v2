package main

import (
	"context"
	"log"

	"github.com/NethermindEth/starknet.go/rpc"
)

// Returns a new Starknet.Go RPC Provider
func NewProvider(providerUrl string) *rpc.Provider {
	provider, err := rpc.NewProvider(providerUrl)
	if err != nil {
		log.Fatalf("Error connecting to RPC provider at %s: %s", providerUrl, err)
	}
	return provider
}

// Returns a Go channel where BlockHeaders are received
func BlockHeaderSubscription(providerUrl string) (
	*rpc.WsProvider, chan *rpc.BlockHeader,
) {
	// Take the providerUrl parts (host & port) and build the ws url
	wsProviderUrl := "ws://" + "localhost" + ":" + "6061" + "/v0_8"

	// Initialize connection to WS provider
	wsProvider, err := rpc.NewWebsocketProvider(wsProviderUrl)
	if err != nil {
		log.Fatalf("Error dialing the WS provider: %s", err)
	}

	headersFeed := make(chan *rpc.BlockHeader)
	_, err = wsProvider.SubscribeNewHeads(context.Background(), headersFeed, nil)
	if err != nil {
		log.Fatalf("Error subscribing to new block headers: %s", err)
	}

	return wsProvider, headersFeed
}
