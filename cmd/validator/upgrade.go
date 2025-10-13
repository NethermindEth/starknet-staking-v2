package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator"
)

type GitHubRelease struct {
	TagName string `json:"tag_name"`
}

func getLatestRelease() (string, error) {
	const url = "https://api.github.com/repos/NethermindEth/starknet-staking-v2/releases/latest"
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest release: %w", err)
	}
	//nolint // Reason: ignoring close error is acceptable in this context
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

func needsUpdate(currentVersion string, latestVersion string) (bool, error) {
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

func trackLatestRelease(ctx context.Context, logger *utils.ZapLogger) {
	timer := time.NewTimer(time.Millisecond) // Don't wait the first time
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			latestVersion, err := getLatestRelease()
			if err != nil {
				logger.Debugf("cannot get latest release: %w", err)
				continue
			}

			currentVersion := validator.Version
			needsUpdate, err := needsUpdate(currentVersion, latestVersion)
			if err != nil {
				logger.Debug(err.Error())
				continue
			}

			if needsUpdate {
				const releasesUrl = "https://github.com/NethermindEth/starknet-staking-v2/releases"
				logger.Warnf(
					"new release available. Update your tool from %s to %s. %s",
					currentVersion,
					latestVersion,
					releasesUrl,
				)
			}
			timer.Reset(time.Hour)
		}
	}
}
