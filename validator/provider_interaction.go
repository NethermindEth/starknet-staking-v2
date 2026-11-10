package validator

import (
	"context"
	"maps"
	"net/url"
	"regexp"
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
				"cannot read spec version of node at %s: %w", redactURL(providerURL), specErr,
			)
		}
		if !slices.Contains(supportedSpecVersions, strings.TrimPrefix(nodeSpecVersion, "v")) {
			return nil, errors.Errorf(
				"node at %s implements JSON-RPC specification %s, this tool requires %s",
				redactURL(providerURL),
				nodeSpecVersion,
				strings.Join(supportedSpecVersions, " or "),
			)
		}
		logger.Warn(
			"node implements a different JSON-RPC specification than Starknet.Go targets",
			zap.String("providerURL", redactURL(providerURL)),
			zap.String("nodeSpecVersion", nodeSpecVersion),
		)
	} else if err != nil {
		return nil, errors.Errorf(
			"cannot create RPC provider at %s: %w", redactURL(providerURL), err,
		)
	}

	logger.Info("connected to RPC", zap.String("providerURL", redactURL(providerURL)))

	return provider, nil
}

const (
	redactedText            = "REDACTED"
	minSecretPathSegmentLen = 16
)

func redactURL(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid URL>"
	}

	redacted := false
	if parsedURL.User != nil {
		parsedURL.User = url.User(redactedText)
		redacted = true
	}
	if parsedURL.RawQuery != "" {
		keys := slices.Sorted(maps.Keys(parsedURL.Query()))
		for i, key := range keys {
			keys[i] = key + "=" + redactedText
		}
		parsedURL.RawQuery = strings.Join(keys, "&")
		redacted = true
	}
	segments := strings.Split(parsedURL.Path, "/")
	for i, segment := range segments {
		if len(segment) >= minSecretPathSegmentLen {
			segments[i] = redactedText
			redacted = true
		}
	}
	if !redacted {
		return rawURL
	}
	parsedURL.Path = strings.Join(segments, "/")
	parsedURL.RawPath = ""
	parsedURL.Fragment = ""

	return parsedURL.String()
}

var urlInText = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'\\]+`)

func redactURLsIn(text string) string {
	return urlInText.ReplaceAllStringFunc(text, func(match string) string {
		trimmed := strings.TrimRight(match, ":,.;)]")
		if _, err := url.Parse(trimmed); err != nil {
			return match
		}

		return redactURL(trimmed) + match[len(trimmed):]
	})
}

type redactedURLsError struct {
	err error
}

func (e redactedURLsError) Error() string { return redactURLsIn(e.err.Error()) }

func (e redactedURLsError) Unwrap() error { return e.err }

func redactErrorURLs(err error) error {
	if err == nil {
		return nil
	}

	return redactedURLsError{err: err}
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
	logger.Debug(
		"initialising websocket connection", zap.String("wsProviderURL", redactURL(wsProviderURL)),
	)
	// This needs a timeout or something
	wsProvider, err := rpc.NewWebsocketProvider(ctx, wsProviderURL)
	if err != nil {
		return nil, nil, nil, errors.Errorf(
			"dialling WS provider at %s: %w", redactURL(wsProviderURL), err,
		)
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
		zap.String("subscriptionID", clientSubscription.ID()),
	)

	return wsProvider, headersFeed, clientSubscription, nil
}
