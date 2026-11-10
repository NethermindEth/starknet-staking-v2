package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator"
	"go.uber.org/zap"
)

const maxReleaseCheckTime = 30 * time.Second

type GitHubRelease struct {
	TagName string `json:"tag_name"`
}

func getLatestRelease(ctx context.Context) (string, error) {
	const url = "https://api.github.com/repos/NethermindEth/starknet-staking-v2/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest release: %w", err)
	}
	//nolint:errcheck // Ignoring close error is acceptable in this context
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status: %s", resp.Status)
	}

	var release GitHubRelease
	err = json.NewDecoder(resp.Body).Decode(&release)
	if err != nil {
		return "", fmt.Errorf("failed to parse release info: %w", err)
	}

	return release.TagName, nil
}

func needsUpdate(currentVersion, latestVersion string) (bool, error) {
	// keeping this condition here because if we are on a development build
	// we can check that the upgrader is being triggered correctly
	if currentVersion == "dev" {
		return true, nil
	}

	currentVer, err := semver.NewVersion(currentVersion)
	if err != nil {
		return false, fmt.Errorf("cannot parse current version: %w", err)
	}

	latestVer, err := semver.NewVersion(latestVersion)
	if err != nil {
		return false, fmt.Errorf("cannot parse latest version: %w", err)
	}

	// Don't trigger updates from a stable version to a pre-release version
	if currentVer.Prerelease() == "" && latestVer.Prerelease() != "" {
		return false, nil
	}

	return latestVer.GreaterThan(currentVer), nil
}

func trackLatestRelease(ctx context.Context, logger log.Logger) {
	trackRelease(ctx, logger, getLatestRelease, time.Hour)
}

func trackRelease(
	ctx context.Context,
	logger log.Logger,
	latestRelease func(context.Context) (string, error),
	interval time.Duration,
) {
	timer := time.NewTimer(time.Millisecond) // Don't wait the first time
	defer timer.Stop()

	failing := false
	checkFailed := func(err error) {
		if failing {
			logger.Debug("cannot check for a new release", errorField(err))

			return
		}
		failing = true
		logger.Warn(
			"cannot check for a new release",
			errorField(err),
			zap.Stringer("nextCheckIn", interval),
		)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			timer.Reset(interval)

			checkCtx, cancelCheck := context.WithTimeout(ctx, min(interval, maxReleaseCheckTime))
			latestVersion, err := latestRelease(checkCtx)
			cancelCheck()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				checkFailed(fmt.Errorf("cannot get latest release: %w", err))

				continue
			}

			currentVersion := validator.Version
			needsUpdate, err := needsUpdate(currentVersion, latestVersion)
			if err != nil {
				checkFailed(fmt.Errorf("error checking if update is needed: %w", err))

				continue
			}
			failing = false

			if needsUpdate {
				const releasesURL = "https://github.com/NethermindEth/starknet-staking-v2/releases"
				logger.Warn(
					"new release available. Update your tool to the latest version",
					zap.String("currentVersion", currentVersion),
					zap.String("latestVersion", latestVersion),
					zap.String("releasesURL", releasesURL),
				)
			}
		}
	}
}
