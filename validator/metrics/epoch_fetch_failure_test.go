package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/stretchr/testify/require"
)

func TestRecordEpochInfoFetchFailure(t *testing.T) {
	m := NewMetrics("localhost:0", "SN_SEPOLIA", log.NewNopZapLogger())
	m.RecordEpochInfoFetchFailure()
	m.RecordEpochInfoFetchFailure()

	recorder := httptest.NewRecorder()
	m.server.Handler.ServeHTTP(
		recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil),
	)

	body, err := io.ReadAll(recorder.Result().Body)
	require.NoError(t, err)
	require.Contains(
		t,
		string(body),
		`validator_attestation_epoch_info_fetch_failure_count{network="SN_SEPOLIA"} 2`,
	)
}
