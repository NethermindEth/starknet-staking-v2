package validator

import (
	"bytes"
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
