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
	const newerRelease = "v99.0.0"
	errUnreachable := errors.New("github unreachable")

	type check struct {
		release string
		err     error
	}

	startTracker := func(
		t *testing.T,
		interval time.Duration,
		latestRelease func(context.Context) (string, error),
	) (*observer.ObservedLogs, context.CancelFunc, <-chan struct{}) {
		t.Helper()

		core, logs := observer.New(zapcore.DebugLevel)
		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)

		done := make(chan struct{})
		go func() {
			trackRelease(ctx, log.NewZapLoggerWithCore(core), latestRelease, interval)
			close(done)
		}()

		return logs, cancel, done
	}

	t.Run("Checks go on after failures and successes", func(t *testing.T) {
		checks := []check{
			{err: errUnreachable},
			{err: errUnreachable},
			{release: newerRelease},
			{err: errUnreachable},
		}
		const minCalls = 6

		var calls atomic.Int32
		latestRelease := func(context.Context) (string, error) {
			call := int(calls.Add(1))
			if call <= len(checks) {
				return checks[call-1].release, checks[call-1].err
			}

			return newerRelease, nil
		}
		logs, cancel, done := startTracker(t, time.Millisecond, latestRelease)

		require.Eventually(t, func() bool {
			return calls.Load() >= minCalls
		}, 5*time.Second, time.Millisecond)
		cancel()
		<-done

		failures := logs.FilterMessage("cannot check for a new release").AllUntimed()
		require.Len(t, failures, 3)
		require.Equal(t, zapcore.WarnLevel, failures[0].Level)
		require.Equal(t, zapcore.DebugLevel, failures[1].Level)
		require.Equal(t, zapcore.WarnLevel, failures[2].Level)
		require.Equal(
			t, "cannot get latest release: github unreachable", failures[0].ContextMap()["error"],
		)
		require.Equal(t, "1ms", failures[0].ContextMap()["nextCheckIn"])
		require.NotZero(
			t,
			logs.FilterMessage("new release available. Update your tool to the latest version").
				Len(),
		)
	})

	t.Run("A check cancelled by the shutdown is not a failure", func(t *testing.T) {
		started := make(chan struct{})
		latestRelease := func(ctx context.Context) (string, error) {
			close(started)
			<-ctx.Done()

			return "", ctx.Err()
		}
		logs, cancel, done := startTracker(t, time.Hour, latestRelease)

		<-started
		cancel()
		<-done

		require.Zero(t, logs.FilterMessage("cannot check for a new release").Len())
	})

	t.Run("A check without an answer times out and is retried", func(t *testing.T) {
		var calls atomic.Int32
		latestRelease := func(ctx context.Context) (string, error) {
			calls.Add(1)
			<-ctx.Done()

			return "", ctx.Err()
		}
		logs, cancel, done := startTracker(t, 10*time.Millisecond, latestRelease)

		require.Eventually(t, func() bool {
			return calls.Load() >= 2
		}, 5*time.Second, time.Millisecond)
		cancel()
		<-done

		failures := logs.FilterMessage("cannot check for a new release").AllUntimed()
		require.NotEmpty(t, failures)
		require.Equal(
			t,
			"cannot get latest release: context deadline exceeded",
			failures[0].ContextMap()["error"],
		)
	})
}
