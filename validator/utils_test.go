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
