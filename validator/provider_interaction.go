package validator

import (
	"context"
	"strconv"
	"strings"

	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
)

// Returns a new Starknet.Go RPC Provider
func NewProvider[Logger utils.Logger](providerUrl string, logger Logger) (*rpc.Provider, error) {
	provider, err := rpc.NewProvider(providerUrl)
	if err != nil {
		return nil, errors.Errorf("Error creating RPC provider at %s: %s", providerUrl, err)
	}

	// Connection check
	_, err = provider.ChainID(context.Background())
	if err != nil {
		return nil, errors.Errorf("Error connecting to RPC provider at %s: %s", providerUrl, err)
	}

	logger.Infof("Connected to RPC at %s", providerUrl)
	return provider, nil
}

// Returns a Go channel where BlockHeaders are received
func BlockHeaderSubscription[Logger utils.Logger](wsProviderUrl string, logger Logger) (
	*rpc.WsProvider, chan *rpc.BlockHeader, error,
) {
	logger.Debugw("Initializing websocket connection", "wsProviderUrl", wsProviderUrl)
	// This needs a timeout or something
	wsProvider, err := rpc.NewWebsocketProvider(wsProviderUrl)
	if err != nil {
		return nil, nil, errors.Errorf("Error dialing the WS provider at %s: %s", wsProviderUrl, err)
	}

	logger.Debugw("Subscribing to new block headers")
	headersFeed := make(chan *rpc.BlockHeader)
	clientSubscription, err := wsProvider.SubscribeNewHeads(
		context.Background(), headersFeed, rpc.BlockID{Tag: "latest"},
	)
	if err != nil {
		return nil, nil, errors.Errorf("Error subscribing to new block headers: %s", err)
	}

	logger.Infof("Subscribed to new block headers", "Subscription ID", clientSubscription.ID())
	return wsProvider, headersFeed, nil
}

func providerSynced(provider *rpc.Provider, logger utils.Logger) error {
	syncStatus, err := provider.Syncing(context.Background())
	if err != nil {
		return err
	}
	// not checking for false condition since if it exists it can only be false
	if syncStatus.SyncStatus != nil {
		return errors.New("provider is not syncing")
	}

	println("aaaaaaaa")
	println(*syncStatus.SyncStatus)
	println(syncStatus.CurrentBlockNum)
	println(syncStatus.HighestBlockNum)

	currentBlockNum := strings.TrimPrefix(string(syncStatus.CurrentBlockNum), "0x")
	currentBlock, err := strconv.ParseUint(currentBlockNum, 16, 64)
	if err != nil {
		return err
	}

	highestBlockNum := strings.TrimPrefix(string(syncStatus.HighestBlockNum), "0x")
	highestBlock, err := strconv.ParseUint(highestBlockNum, 16, 64)
	if err != nil {
		return err
	}

	difference := highestBlock - currentBlock

	if difference > 3 {
		logger.Warnw(
			"provider node is behind the network",
			"blocks behind", difference,
			"highest block", highestBlock,
			"current block", currentBlock,
		)
	} else {
		logger.Infof("provider is in sync")
	}

	return nil
}
