package validator

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// logNewEpoch and logBlock wrap the logger, so the caller annotation must
// point at the line that invoked them, not at the Info calls inside the
// helpers themselves.
func TestLogHelpersReportCallSite(t *testing.T) {
	var buf bytes.Buffer
	logger, err := log.NewZapLogger(
		log.NewLevel(log.INFO), log.WithWriter(&buf), log.WithColour(false),
	)
	require.NoError(t, err)

	epochInfo := types.EpochInfo{}
	attestInfo := types.AttestInfo{}

	// Capture this file and line; the helpers must be called on the two
	// lines immediately below so the expected offsets stay +1 and +2.
	_, file, line, _ := runtime.Caller(0)
	logNewEpoch(&epochInfo, &attestInfo, logger)
	logBlock(0, &epochInfo, &attestInfo, logger)

	file = filepath.Base(file)
	out := buf.String()

	require.Contains(t, out, fmt.Sprintf("%s:%d", file, line+1))
	require.Contains(t, out, fmt.Sprintf("%s:%d", file, line+2))
}

// recordingLogger is a log.Logger that is not a *log.ZapLogger, so skipCaller
// must return it unchanged and the helpers must still log through it.
type recordingLogger struct {
	*log.ZapLogger
	msgs []string
}

func (r *recordingLogger) Info(msg string, _ ...zap.Field) {
	r.msgs = append(r.msgs, msg)
}

func TestLogHelpersNonZapLoggerFallback(t *testing.T) {
	rec := &recordingLogger{ZapLogger: log.NewNopZapLogger()}

	require.Same(t, rec, skipCaller(rec))

	epochInfo := types.EpochInfo{}
	attestInfo := types.AttestInfo{}
	logNewEpoch(&epochInfo, &attestInfo, rec)
	logBlock(0, &epochInfo, &attestInfo, rec)

	// logNewEpoch emits two lines (epoch and attest info), logBlock emits one.
	require.Len(t, rec.msgs, 3)
	require.Equal(t, "epoch started", rec.msgs[0])
	require.Equal(t, "attest info", rec.msgs[1])
	require.Contains(t, rec.msgs[2], "block 0 received")
}

func TestRedactURL(t *testing.T) {
	testCases := []struct {
		url      string
		expected string
	}{
		{url: "http://localhost:6060/rpc/v0_10", expected: "http://localhost:6060/rpc/v0_10"},
		{url: "wss://localhost:6061/rpc/v0_10", expected: "wss://localhost:6061/rpc/v0_10"},
		{url: "wrong url", expected: "wrong url"},
		{
			url:      "https://starknet-mainnet.infura.io/v3/0123456789abcdef0123456789abcdef",
			expected: "https://starknet-mainnet.infura.io/v3/REDACTED",
		},
		{
			url:      "wss://node.example/rpc/v0_10?apikey=secret&network=mainnet",
			expected: "wss://node.example/rpc/v0_10?apikey=REDACTED&network=REDACTED",
		},
		{
			url:      "https://user:secret@node.example/rpc",
			expected: "https://REDACTED@node.example/rpc",
		},
		{url: "http://[::1", expected: "<invalid URL>"},
	}

	for _, tc := range testCases {
		t.Run(tc.url, func(t *testing.T) {
			require.Equal(t, tc.expected, redactURL(tc.url))
		})
	}
}

func TestRedactURLsIn(t *testing.T) {
	testCases := []struct {
		text     string
		expected string
	}{
		{
			text: `-32603 The error is not a valid RPC error: Post ` +
				`"http://node:6060/v3/0123456789abcdef0123456789abcdef?apikey=topsecret": EOF`,
			expected: `-32603 The error is not a valid RPC error: Post ` +
				`"http://node:6060/v3/REDACTED?apikey=REDACTED": EOF`,
		},
		{
			text:     "cannot reach RPC provider at http://127.0.0.1:1: connection refused",
			expected: "cannot reach RPC provider at http://127.0.0.1:1: connection refused",
		},
		{
			text:     `WS provider URL must use ws:// or wss://, got "http://localhost:6061"`,
			expected: `WS provider URL must use ws:// or wss://, got "http://localhost:6061"`,
		},
		{
			text:     "dialling WS provider at wss://user:pw@node/ws: refused",
			expected: "dialling WS provider at wss://REDACTED@node/ws: refused",
		},
	}

	for _, tc := range testCases {
		require.Equal(t, tc.expected, redactURLsIn(tc.text))
	}
}

func TestLogNewEpochFields(t *testing.T) {
	var buf bytes.Buffer
	logger, err := log.NewZapLogger(
		log.NewLevel(log.INFO), log.WithWriter(&buf), log.WithColour(false),
	)
	require.NoError(t, err)

	epochInfo := types.EpochInfo{EpochID: 79310, EpochLen: 231, StartingBlock: 17745944}
	logNewEpoch(&epochInfo, &types.AttestInfo{}, logger)

	out := buf.String()
	require.Contains(t, out, `"epochID": 79310,`)
	require.Contains(t, out, `"startBlock": 17745944,`)
	require.Contains(t, out, `"endBlock": 17746174}`)
}

func TestLogBlockCountsBlocks(t *testing.T) {
	epochInfo := types.EpochInfo{EpochID: 1, EpochLen: 100, StartingBlock: 1000}
	attestInfo := types.AttestInfo{TargetBlock: 1010, WindowStart: 1021, WindowEnd: 1050}

	testCases := []struct {
		block    uint64
		expected string
	}{
		{block: 1020, expected: "block 1020 received, 1 block to attest"},
		{block: 1019, expected: "block 1019 received, 2 blocks to attest"},
		{block: 1049, expected: "block 1049 received, 1 block before end of window"},
		{block: 1099, expected: "block 1099 received, 1 block for the next epoch"},
		{block: 1098, expected: "block 1098 received, 2 blocks for the next epoch"},
	}
	for _, tc := range testCases {
		rec := &recordingLogger{ZapLogger: log.NewNopZapLogger()}

		logBlock(tc.block, &epochInfo, &attestInfo, rec)

		require.Equal(t, []string{tc.expected}, rec.msgs)
	}
}

func TestLogBlockNumberField(t *testing.T) {
	var buf bytes.Buffer
	logger, err := log.NewZapLogger(
		log.NewLevel(log.INFO), log.WithWriter(&buf), log.WithColour(false),
	)
	require.NoError(t, err)

	logBlock(1020, &types.EpochInfo{}, &types.AttestInfo{WindowStart: 1021}, logger)

	require.Contains(
		t, buf.String(), `block 1020 received, 1 block to attest	{"blockNumber": 1020}`,
	)
}

func TestRedactErrorURLsKeepsTheErrorChain(t *testing.T) {
	err := redactErrorURLs(transient(errors.New(
		`Post "http://node/v3/0123456789abcdef0123456789abcdef": EOF`,
	)))

	require.ErrorIs(t, err, ErrTransient)
	require.Equal(t, `Post "http://node/v3/REDACTED": EOF`, err.Error())
	require.NoError(t, redactErrorURLs(nil))
}
