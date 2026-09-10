package validator

import (
	"context"
	"slices"
	"strings"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet.go/client"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
	"go.uber.org/zap"
)

var supportedSpecVersions = []string{"0.10.2", "0.10.3", "0.10.4"}

// Returns a new Starknet.Go RPC Provider
func NewProvider(
	ctx context.Context,
	providerURL string,
	logger log.Logger,
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
		logger.Warn(
			"node implements a different JSON-RPC specification than Starknet.Go targets",
			zap.String("providerUrl", providerURL),
			zap.String("nodeSpecVersion", nodeSpecVersion),
		)
	} else if err != nil {
		return nil, errors.Errorf("cannot create RPC provider at %s: %w", providerURL, err)
	}

	logger.Info("connected to RPC", zap.String("providerURL", providerURL))

	return provider, nil
}

// Returns a Go channel where BlockHeaders are received
func SubscribeToBlockHeaders(
	ctx context.Context,
	wsProviderURL string,
	logger log.Logger,
) (
	*rpc.WsProvider,
	chan *rpc.BlockHeader,
	*client.ClientSubscription,
	error,
) {
	logger.Debug("initialising websocket connection", zap.String("wsProviderUrl", wsProviderURL))
	// This needs a timeout or something
	wsProvider, err := rpc.NewWebsocketProvider(ctx, wsProviderURL)
	if err != nil {
		return nil, nil, nil, errors.Errorf("dialling WS provider at %s: %w", wsProviderURL, err)
	}

	logger.Debug("Subscribing to new block headers...")
	headersFeed := make(chan *rpc.BlockHeader)
	clientSubscription, err := wsProvider.SubscribeNewHeads(
		ctx, headersFeed, new(rpc.SubscriptionBlockID).WithLatestTag(),
	)
	if err != nil {
		return nil, nil, nil, errors.Errorf("subscribing to new block headers: %w", err)
	}

	logger.Info(
		"subscribed to new block header",
		zap.String("subscription ID", clientSubscription.ID()),
	)

	return wsProvider, headersFeed, clientSubscription, nil
}
