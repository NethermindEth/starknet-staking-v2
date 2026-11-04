package validator

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
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
	// are answered by default, since creating an `rpc.Provider` requires them
	Results map[string]string
	// `starknet_call` results by entry point selector name
	CallResults map[string]string
	// Called on a websocket upgrade request, which is always refused
	OnWSUpgrade func()
}

// Starts a JSON-RPC server answering with the results configured in `mock`, closed on
// test cleanup. Any other request gets a JSON-RPC error instead of failing the test,
// since requests from background goroutines can arrive after it ends.
func MockRPCServer(t *testing.T, mock MockRPC) *httptest.Server {
	t.Helper()

	results := map[string]string{
		"starknet_specVersion": `"v0.10.2"`,
		"starknet_chainId":     `"0x534e5f5345504f4c4941"`, // SN_SEPOLIA
	}
	maps.Copy(results, mock.Results)

	callResults := make(map[felt.Felt]string, len(mock.CallResults))
	for name, result := range mock.CallResults {
		callResults[*snGoUtils.GetSelectorFromNameFelt(name)] = result
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			if mock.OnWSUpgrade != nil {
				mock.OnWSUpgrade()
			}
			w.WriteHeader(http.StatusServiceUnavailable)

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

		result, ok := results[req.Method]
		if req.Method == "starknet_call" && len(req.Params) > 0 {
			var call struct {
				Selector felt.Felt `json:"entry_point_selector"`
			}
			if err := json.Unmarshal(req.Params[0], &call); err == nil {
				result, ok = callResults[call.Selector]
			}
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
