package validator

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	snGoUtils "github.com/NethermindEth/starknet.go/utils"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
)

type Method struct {
	Name   string `json:"method"`
	Params []any  `json:"params"`
}

type EnvVariable struct {
	HttpProviderUrl string
	WsProviderUrl   string
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

	wsProviderUrl := os.Getenv("WS_PROVIDER_URL")
	if wsProviderUrl == "" {
		return EnvVariable{}, errors.New("failed to load WS_PROVIDER_URL, empty string")
	}

	return EnvVariable{base, wsProviderUrl}, nil
}

func MockRPCServer(
	t *testing.T, operationalAddress *felt.Felt, serverInternalError string,
) *httptest.Server {
	t.Helper()

	mockRpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read and decode JSON body
		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		defer func() {
			err := r.Body.Close()
			require.NoError(t, err)
		}()

		var req Method
		err = json.Unmarshal(bodyBytes, &req)
		require.NoError(t, err)

		switch req.Name {
		case "starknet_chainId":
			const SN_SEPOLIA_ID = "0x534e5f5345504f4c4941"
			chainIdResponse := fmt.Sprintf(
				`{"jsonrpc": "2.0", "result": "%s", "id": 1}`, SN_SEPOLIA_ID,
			)
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(chainIdResponse))
			require.NoError(t, err)
		case "starknet_call":
			// Marshal the `Params` back into JSON
			paramsBytes, err := json.Marshal(req.Params[0])
			require.NoError(t, err)

			// Unmarshal `Params` into `FunctionCall`
			var fnCall rpc.FunctionCall
			err = json.Unmarshal(paramsBytes, &fnCall)
			require.NoError(t, err)

			// Just making sure it's the call expected
			expectedEpochInfoFnCall := rpc.FunctionCall{
				ContractAddress: utils.HexToFelt(t, constants.SEPOLIA_STAKING_CONTRACT_ADDRESS),
				EntryPointSelector: snGoUtils.GetSelectorFromNameFelt(
					"get_attestation_info_by_operational_address",
				),
				Calldata: []*felt.Felt{operationalAddress},
			}

			require.Equal(t, expectedEpochInfoFnCall, fnCall)

			w.WriteHeader(http.StatusInternalServerError)
			_, err = w.Write([]byte(serverInternalError))
			require.NoError(t, err)

		// Called when calling `rpc.NewProvider` in Starknet.Go
		case "starknet_specVersion":
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"jsonrpc": "2.0", "result": "v0.9.0", "id": 1}`))
			require.NoError(t, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, err = w.Write([]byte(`Should not get here, method: ` + req.Name))
			require.NoError(t, err)
		}
	}))

	return mockRpc
}

func SepoliaValidationContracts(t *testing.T) *types.ValidationContracts {
	t.Helper()

	addresses := new(config.ContractAddresses).SetDefaults("SN_SEPOLIA")
	contracts := types.ValidationContractsFromAddresses(addresses)
	return &contracts
}
