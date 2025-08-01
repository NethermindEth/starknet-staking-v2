package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet.go/client"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
)

var ChainID string

// Returns a new Starknet.Go RPC Provider
func NewProvider[Logger utils.Logger](providerUrl string, logger Logger) (*rpc.Provider, error) {
	provider, err := rpc.NewProvider(providerUrl)
	if err != nil {
		return nil, errors.Errorf("cannot create RPC provider at %s: %s", providerUrl, err)
	}

	// Connection check
	ChainID, err = provider.ChainID(context.Background())
	if err != nil {
		return nil, errors.Errorf("cannot connect to RPC provider at %s: %s", providerUrl, err)
	}

	logger.Infof("Connected to RPC at %s", providerUrl)
	return provider, nil
}

// Returns a Go channel where BlockHeaders are received
func SubscribeToBlockHeaders[Logger utils.Logger](wsProviderUrl string, logger Logger) (
	*rpc.WsProvider,
	chan *rpc.BlockHeader,
	*client.ClientSubscription,
	error,
) {
	logger.Debugw("Initialising websocket connection", "wsProviderUrl", wsProviderUrl)
	// This needs a timeout or something
	wsProvider, err := rpc.NewWebsocketProvider(wsProviderUrl)
	if err != nil {
		return nil, nil, nil, errors.Errorf("dialling WS provider at %s: %s", wsProviderUrl, err)
	}

	logger.Debugw("Subscribing to new block headers...")
	headersFeed := make(chan *rpc.BlockHeader)
	clientSubscription, err := wsProvider.SubscribeNewHeads(
		context.Background(), headersFeed, rpc.BlockID{Tag: "latest"},
	)
	if err != nil {
		return nil, nil, nil, errors.Errorf("subscribing to new block headers: %s", err)
	}

	logger.Infof("Subscribed to new block header. Subscription ID: %s", clientSubscription.ID())
	return wsProvider, headersFeed, clientSubscription, nil
}

type ProviderNode uint

const (
	Juno ProviderNode = iota
	Pathfinder
	Other
)

func GetProviderNodeType[L utils.Logger](providerURL string, logger L) ProviderNode {
	errs := make([]error, 0, 2)

	resp, err := rpcVersionRequest(providerURL, "juno_version")
	if err == nil {
		ver, err := semver.NewVersion(resp)
		if err == nil {
			logger.Infof("connected to Juno %s", ver)
			return Juno
		}
	}
	errs = append(errs, err)

	// repeat the procedure with pathfinder. Small duplication, I don't mind
	resp, err = rpcVersionRequest(providerURL, "pathfinder_version")
	if err == nil {
		ver, err := semver.NewVersion(resp)
		if err == nil {
			logger.Infof("connected to Pathfinder %s", ver)
			return Pathfinder
		}
	}
	errs = append(errs, err)

	logger.Warnw("coulnd't identify connected node")
	logger.Debugw(
		"error while trying to identify if connected to either Juno or Pathfinder",
		"Juno", errs[0],
		"Pathfinder", errs[1],
	)
	return Other

}

func rpcVersionRequest(providerURL string, methodName string) (string, error) {
	const rawJson = `{
		"id": 1,
		"jsonrpc": 2.0,
		"method": %s
	}`
	rpcReq := fmt.Sprintf(rawJson, methodName)

	req, err := http.NewRequest(
		"POST",
		providerURL,
		strings.NewReader(rpcReq),
	)
	if err != nil {
		return "", err
	}

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	//nolint:errcheck // this program is meant to be short lived and the error is of little danger
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	type expectedRPCResponse struct {
		Result *string `json:"result,omitempty"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	var rpcResp expectedRPCResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}

	if rpcResp.Result != nil {
		return *rpcResp.Result, nil
	}
	if rpcResp.Error != nil &&
		rpcResp.Error.Code != -32601 &&
		rpcResp.Error.Message != "Message not found" {
		return "", nil
	}

	return "", fmt.Errorf("unexpected response for request %s: %s", rpcReq, body)
}

func JunoSynced[L utils.Logger](providerURL string, logger L) bool {
	resp, err := http.Get(providerURL + "/ready/sync")
	if err != nil {
		logger.Debugw(
			"trying to check if Juno is synced",
			"error", err.Error(),
		)
	}

	//nolint:errcheck // this program is meant to be short lived and the error is of little danger
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func OtherSynced[L utils.Logger](providerURL string) bool {
	return true
}
