package main

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NethermindEth/juno/utils/log"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const v130 = "1.3.0"

func TestNeedsUpdate(t *testing.T) {
	otherVer := []struct {
		currentVer string
		otherVer   string
		update     bool
	}{
		{
			currentVer: v130,
			otherVer:   "0.4.0",
			update:     false,
		},
		{
			currentVer: v130,
			otherVer:   "1.2.9",
			update:     false,
		},
		{
			currentVer: v130,
			otherVer:   "1.3.0-rc.0",
			update:     false,
		},
		{
			currentVer: v130,
			otherVer:   v130,
			update:     false,
		},
		{
			currentVer: v130,
			otherVer:   "1.3.1",
			update:     true,
		},
		{
			currentVer: v130,
			otherVer:   "2.0.0-rc.1",
			update:     false,
		},
		{
			currentVer: v130,
			otherVer:   "2.0.0-beta.1",
			update:     false,
		},
		{
			currentVer: "1.3.0-rc.1",
			otherVer:   "1.3.0-rc.2",
			update:     true,
		},
		{
			currentVer: "1.3.0-rc.3",
			otherVer:   "1.3.0-rc.1",
			update:     false,
		},
		{
			currentVer: "1.3.0-rc.3",
			otherVer:   "1.4.0-rc.1",
			update:     true,
		},
		{
			currentVer: v130,
			otherVer:   "2.0.0",
			update:     true,
		},
		{
			currentVer: "dev",
			otherVer:   "0.0.0",
			update:     true,
		},
	}

	for _, val := range otherVer {
		t.Run(fmt.Sprintf("from %s to %s", val.currentVer, val.otherVer), func(t *testing.T) {
			update, err := needsUpdate(val.currentVer, val.otherVer)
			require.NoError(t, err)
			require.Equal(t, val.update, update)
		})
	}
}

func TestTrackRelease(t *testing.T) {
	const failedChecks = 3

	core, logs := observer.New(zapcore.DebugLevel)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var calls atomic.Int32
	latestRelease := func(context.Context) (string, error) {
		if calls.Add(1) <= failedChecks {
			return "", errors.New("github unreachable")
		}

		return "v99.0.0", nil
	}

	done := make(chan struct{})
	go func() {
		trackRelease(ctx, log.NewZapLoggerWithCore(core), latestRelease, time.Millisecond)
		close(done)
	}()

	require.Eventually(t, func() bool {
		return logs.FilterMessage("new release available. Update your tool to the latest version").
			Len() > 0
	}, 5*time.Second, time.Millisecond)
	cancel()
	<-done

	require.Greater(t, calls.Load(), int32(failedChecks))
	failures := logs.FilterMessage("cannot check for a new release").AllUntimed()
	require.Len(t, failures, failedChecks)
	require.Equal(t, zapcore.WarnLevel, failures[0].Level)
	for _, failure := range failures[1:] {
		require.Equal(t, zapcore.DebugLevel, failure.Level)
	}
	require.Equal(
		t, "cannot get latest release: github unreachable", failures[0].ContextMap()["error"],
	)
}
