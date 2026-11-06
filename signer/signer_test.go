package signer

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestNew(t *testing.T) {
	t.Run("Malformed private key is never part of the error", func(t *testing.T) {
		const malformedKey = "0xNOT-A-KEY-dummy"

		_, err := New(malformedKey, log.NewNopZapLogger())
		require.ErrorContains(t, err, "invalid private key")
		require.NotContains(t, err.Error(), malformedKey)
	})

	t.Run("Private key out of range", func(t *testing.T) {
		_, err := New("0x0", log.NewNopZapLogger())
		require.ErrorContains(t, err, "invalid private key")
	})

	t.Run("Valid private key", func(t *testing.T) {
		_, err := New("0x123", log.NewNopZapLogger())
		require.NoError(t, err)
	})
}

// Returns a signer for the test key 0x123 logging at debug level into the returned logs,
// and a test server for its routes
func newTestSigner(t *testing.T) (*observer.ObservedLogs, *httptest.Server) {
	t.Helper()

	core, logs := observer.New(zapcore.DebugLevel)
	signer, err := New("0x123", log.NewZapLoggerWithCore(core))
	require.NoError(t, err)

	server := httptest.NewServer(signer.router())
	t.Cleanup(server.Close)

	return logs, server
}

// Sends `method` request with `body` to the sign endpoint of `server`. Returns the
// response status code and body
func sendSignRequest(
	t *testing.T, server *httptest.Server, method, body string,
) (statusCode int, respBody string) {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(), method, server.URL+SignEndpoint, bytes.NewBufferString(body),
	)
	require.NoError(t, err)
	resp, err := server.Client().Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	respBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, string(respBytes)
}

func validSignRequest(t *testing.T) string {
	t.Helper()

	zeroBounds := rpc.ResourceBounds{MaxAmount: "0x0", MaxPricePerUnit: "0x0"}
	request := Request{
		BroadcastInvokeTxnV3: &rpc.BroadcastInvokeTxnV3{
			Type:          rpc.TransactionTypeInvoke,
			SenderAddress: felt.NewUnsafeFromString[felt.Felt]("0x456"),
			Calldata:      []*felt.Felt{felt.NewUnsafeFromString[felt.Felt]("0x1")},
			Version:       rpc.TransactionV3,
			Signature:     []*felt.Felt{},
			Nonce:         felt.NewUnsafeFromString[felt.Felt]("0x1"),
			ResourceBounds: &rpc.ResourceBoundsMapping{
				L1Gas:     zeroBounds,
				L1DataGas: zeroBounds,
				L2Gas:     zeroBounds,
			},
			Tip:                   "0x0",
			PayMasterData:         []*felt.Felt{},
			AccountDeploymentData: []*felt.Felt{},
			NonceDataMode:         rpc.DAModeL1,
			FeeMode:               rpc.DAModeL1,
		},
		ChainID: new(felt.Felt).SetBytes([]byte("SN_SEPOLIA")),
	}
	body, err := json.Marshal(request)
	require.NoError(t, err)

	return string(body)
}

func TestSignHandler(t *testing.T) {
	t.Run("Valid request is signed", func(t *testing.T) {
		_, server := newTestSigner(t)

		statusCode, body := sendSignRequest(t, server, http.MethodPost, validSignRequest(t))
		require.Equal(t, http.StatusOK, statusCode, body)

		var resp Response
		require.NoError(t, json.Unmarshal([]byte(body), &resp))
		require.NotNil(t, resp.Signature[0])
		require.NotNil(t, resp.Signature[1])
	})

	t.Run("Request without transaction or chain id gets a 400 response", func(t *testing.T) {
		_, server := newTestSigner(t)

		for _, body := range []string{
			`{}`,
			`{"chain_id": "0x534e5f5345504f4c4941"}`,
			`{"transaction": {"type": "INVOKE"}}`,
		} {
			statusCode, respBody := sendSignRequest(t, server, http.MethodPost, body)
			require.Equal(t, http.StatusBadRequest, statusCode, body)
			require.Contains(t, respBody, "both `transaction` and `chain_id` are required")
		}
	})

	t.Run("Malformed JSON gets a 400 response", func(t *testing.T) {
		_, server := newTestSigner(t)

		statusCode, respBody := sendSignRequest(t, server, http.MethodPost, "not json")
		require.Equal(t, http.StatusBadRequest, statusCode)
		require.Contains(t, respBody, "failed to decode request body")
	})

	t.Run("Methods other than POST get a 405 response", func(t *testing.T) {
		_, server := newTestSigner(t)

		for _, method := range []string{http.MethodGet, http.MethodPut} {
			statusCode, _ := sendSignRequest(t, server, method, validSignRequest(t))
			require.Equal(t, http.StatusMethodNotAllowed, statusCode, method)
		}
	})

	t.Run("Request is logged at debug level", func(t *testing.T) {
		logs, server := newTestSigner(t)
		body := validSignRequest(t)

		statusCode, _ := sendSignRequest(t, server, http.MethodPost, body)
		require.Equal(t, http.StatusOK, statusCode)

		entries := logs.FilterMessage("receiving http request").AllUntimed()
		require.Len(t, entries, 1)
		fields := entries[0].ContextMap()
		require.Equal(t, http.MethodPost, fields["method"])
		require.Equal(t, SignEndpoint, fields["path"])
		require.NotEmpty(t, fields["remoteAddress"])
		require.Equal(t, int64(len(body)), fields["contentLength"])
		require.NotContains(t, fields, "requestError")
	})
}
