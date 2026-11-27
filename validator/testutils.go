package validator

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
)

type Method struct {
	Name   string `json:"method"`
	Params []any  `json:"params"`
}

type EnvVariable struct {
	HTTPProviderURL string
	WSProviderURL   string
}

func LoadEnv(t *testing.T) (EnvVariable, error) {
	t.Helper()

	_, err := os.Stat(".env")
	if err == nil {
		if err = godotenv.Load(".env"); err != nil {
			return EnvVariable{}, errors.Join(errors.New("error loading '.env' file"), err)
		}
	}

	base := os.Getenv("HTTP_PROVIDER_URL")
	if base == "" {
		return EnvVariable{}, errors.New("failed to load HTTP_PROVIDER_URL, empty string")
	}

	wsProviderURL := os.Getenv("WS_PROVIDER_URL")
	if wsProviderURL == "" {
		return EnvVariable{}, errors.New("failed to load WS_PROVIDER_URL, empty string")
	}

	return EnvVariable{base, wsProviderURL}, nil
}

// Configures the responses of the server started by `MockRPCServer`. Every result is a
// raw JSON value.
type MockRPC struct {
	// Results by method name. `starknet_specVersion` and `starknet_chainId` (Sepolia)
	// are answered by default, since creating an `rpc.Provider` requires them, and so
	// are `starknet_blockHashAndNumber` (block 100) and `starknet_syncing` (synced),
	// since the validator waits for the node to sync and then watches it
	Results map[string]string
	// Block number the feeder gateway answers `get_block` with. The node's default
	// latest block (100) when zero, which makes the node synced
	ChainHead uint64
	// HTTP status the feeder gateway answers with instead, when set
	FeederStatus int
	// `starknet_call` results by entry point selector name
	CallResults map[string]string
	Errors      map[string]string
	CallErrors  map[string]string
	// Called on a websocket upgrade request, which is always refused
	OnWSUpgrade func()
	OnRequest   func(method string, params []json.RawMessage)
}

const mockLatestBlock = 100

// Points the validator at the server at rawURL as the feeder gateway, until the test
// ends. It is registered for Sepolia, the chain id `MockRPCServer` answers with, and
// every other chain is removed so no test can reach a real feeder gateway
func UseFeeder(t *testing.T, rawURL string) {
	t.Helper()

	feederURL, err := url.Parse(rawURL)
	require.NoError(t, err)

	feederURLs := FeederURLs
	FeederURLs = map[string]*url.URL{"SN_SEPOLIA": feederURL}
	t.Cleanup(func() { FeederURLs = feederURLs })
}

// Starts a JSON-RPC server answering with the results configured in `mock`, closed on
// test cleanup. It answers as a feeder gateway as well. Any other request gets a
// JSON-RPC error instead of failing the test, since requests from background goroutines
// can arrive after it ends.
func MockRPCServer(t *testing.T, mock MockRPC) *httptest.Server {
	t.Helper()

	results := map[string]string{
		"starknet_specVersion": `"v0.10.2"`,
		"starknet_chainId":     `"0x534e5f5345504f4c4941"`, // SN_SEPOLIA
		"starknet_syncing":     "false",
		"starknet_blockHashAndNumber": fmt.Sprintf(
			`{"block_hash": "0x1", "block_number": %d}`, mockLatestBlock,
		),
	}
	maps.Copy(results, mock.Results)

	callResults := make(map[felt.Felt]string, len(mock.CallResults))
	for name, result := range mock.CallResults {
		callResults[*snGoUtils.GetSelectorFromNameFelt(name)] = result
	}
	callErrors := make(map[felt.Felt]string, len(mock.CallErrors))
	for name, rpcError := range mock.CallErrors {
		callErrors[*snGoUtils.GetSelectorFromNameFelt(name)] = rpcError
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			if mock.OnWSUpgrade != nil {
				mock.OnWSUpgrade()
			}
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		// The feeder gateway is the only one queried with GET requests
		if r.Method == http.MethodGet {
			if mock.FeederStatus != 0 {
				w.WriteHeader(mock.FeederStatus)

				return
			}
			chainHead := mock.ChainHead
			if chainHead == 0 {
				chainHead = mockLatestBlock
			}
			_, _ = fmt.Fprintf(w, `{"block_hash": "0x1", "block_number": %d}`, chainHead)

			return
		}

		var req struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}
		if mock.OnRequest != nil {
			mock.OnRequest(req.Method, req.Params)
		}

		result, ok := results[req.Method]
		rpcError, isError := mock.Errors[req.Method]
		if req.Method == "starknet_call" && len(req.Params) > 0 {
			var call struct {
				Selector felt.Felt `json:"entry_point_selector"`
			}
			if err := json.Unmarshal(req.Params[0], &call); err == nil {
				result, ok = callResults[call.Selector]
				rpcError, isError = callErrors[call.Selector]
			}
		}

		if isError {
			_, _ = fmt.Fprintf(w, `{"jsonrpc": "2.0", "id": %s, "error": %s}`, req.ID, rpcError)

			return
		}

		if !ok {
			_, _ = fmt.Fprintf(
				w,
				`{"jsonrpc": "2.0", "id": %s, "error": {"code": -32601, "message": %q}}`,
				req.ID, "unexpected request: "+req.Method,
			)

			return
		}
		_, _ = fmt.Fprintf(w, `{"jsonrpc": "2.0", "id": %s, "result": %s}`, req.ID, result)
	}))
	t.Cleanup(server.Close)

	return server
}

func SepoliaValidationContracts(t *testing.T) *types.ValidationContracts {
	t.Helper()

	addresses := new(config.ContractAddresses).SetDefaults("SN_SEPOLIA")
	contracts, err := types.ValidationContractsFromAddresses(addresses)
	require.NoError(t, err)

	return &contracts
}
