package validator

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet.go/client"
	"github.com/NethermindEth/starknet.go/client/rpcerr"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
	"go.uber.org/zap"
)

var supportedSpecVersions = []string{"0.10.2", "0.10.3", "0.10.4"}

const specVersionTimeout = 30 * time.Second

var ErrTransient = errors.New("transient error")

type transientError struct {
	err error
}

func (e transientError) Error() string { return e.err.Error() }

func (e transientError) Unwrap() error { return e.err }

func (e transientError) Is(target error) bool { return target == ErrTransient }

func transient(err error) error {
	return transientError{err: err}
}

func markIfTransient(err error) error {
	var rpcErr *rpcerr.RPCError
	if errors.As(err, &rpcErr) && rpcErr.Code == rpcerr.InternalError {
		return transient(err)
	}

	return err
}

// Returns a new Starknet.Go RPC Provider
func NewProvider(
	ctx context.Context,
	providerURL string,
	logger log.Logger,
) (*rpc.Provider, error) {
	nodeSpecVersion, err := fetchSpecVersion(ctx, providerURL)
	if err != nil {
		return nil, err
	}
	if versionErr := checkSpecVersion(providerURL, nodeSpecVersion); versionErr != nil {
		return nil, versionErr
	}

	provider, err := rpc.NewProvider(ctx, providerURL)
	if errors.Is(err, rpc.ErrIncompatibleVersion) {
		logger.Warn(
			"node implements a different JSON-RPC specification than Starknet.Go targets",
			zap.String("providerUrl", providerURL),
			zap.String("nodeSpecVersion", nodeSpecVersion),
		)
	} else if err != nil {
		return nil, markIfTransient(
			errors.Errorf("cannot create RPC provider at %s: %w", providerURL, err),
		)
	}

	logger.Info("connected to RPC", zap.String("providerURL", providerURL))

	return provider, nil
}

func fetchSpecVersion(ctx context.Context, providerURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, specVersionTimeout)
	defer cancel()

	rpcClient, err := client.DialOptions(ctx, providerURL)
	if err != nil {
		return "", providerRequestError(providerURL, err)
	}
	defer rpcClient.Close()

	var nodeSpecVersion string
	err = rpcClient.CallContextWithSliceArgs(ctx, &nodeSpecVersion, "starknet_specVersion")
	if err != nil {
		return "", providerRequestError(providerURL, err)
	}

	return nodeSpecVersion, nil
}

func providerRequestError(providerURL string, err error) error {
	var httpErr client.HTTPError
	var urlErr *url.Error
	var rpcErr client.Error

	switch {
	case errors.As(err, &httpErr):
		reachErr := errors.Errorf(
			"cannot reach RPC provider at %s: HTTP %s", providerURL, httpErr.Status,
		)
		if httpErr.StatusCode == http.StatusNotFound {
			reachErr = errors.Errorf("%w (check the URL path)", reachErr)
		}
		if isRetryableHTTPStatus(httpErr.StatusCode) {
			return transient(reachErr)
		}

		return reachErr
	case errors.As(err, &urlErr):
		return transient(errors.Errorf(
			"cannot reach RPC provider at %s: %s", providerURL, describeNetError(urlErr.Err),
		))
	case errors.Is(err, context.DeadlineExceeded):
		return transient(errors.Errorf(
			"cannot reach RPC provider at %s: no answer after %s", providerURL, specVersionTimeout,
		))
	case errors.As(err, &rpcErr):
		answerErr := errors.Errorf(
			"RPC provider at %s answered the spec version request with error %d: %s",
			providerURL, rpcErr.ErrorCode(), rpcErr.Error(),
		)
		if rpcErr.ErrorCode() == rpcerr.InternalError {
			return transient(answerErr)
		}

		return answerErr
	default:
		return errors.Errorf("cannot create RPC provider at %s: %w", providerURL, err)
	}
}

func isRetryableHTTPStatus(statusCode int) bool {
	return statusCode >= http.StatusInternalServerError ||
		statusCode == http.StatusRequestTimeout ||
		statusCode == http.StatusTooManyRequests
}

func describeNetError(err error) string {
	var dnsErr *net.DNSError
	var netErr net.Error

	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.As(err, &dnsErr):
		return fmt.Sprintf("cannot resolve host %s (%s)", dnsErr.Name, dnsErr.Err)
	case errors.As(err, &netErr) && netErr.Timeout():
		return "connection timed out"
	default:
		return err.Error()
	}
}

func checkSpecVersion(providerURL, nodeSpecVersion string) error {
	version, err := semver.StrictNewVersion(strings.TrimPrefix(nodeSpecVersion, "v"))
	if err == nil && version.Prerelease() == "" {
		release := fmt.Sprintf("%d.%d.%d", version.Major(), version.Minor(), version.Patch())
		if slices.Contains(supportedSpecVersions, release) {
			return nil
		}
	}

	return errors.Errorf(
		"node at %s implements JSON-RPC specification %s, this tool requires %s",
		providerURL, nodeSpecVersion, strings.Join(supportedSpecVersions, " or "),
	)
}

func checkWSProviderURL(wsProviderURL string) error {
	parsedURL, err := url.Parse(wsProviderURL)
	if err != nil {
		return errors.Errorf("invalid WS provider URL %q: %w", wsProviderURL, err)
	}
	if parsedURL.Scheme != "ws" && parsedURL.Scheme != "wss" {
		return errors.Errorf(
			"WS provider URL must use ws:// or wss://, got %q", wsProviderURL,
		)
	}

	return nil
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
