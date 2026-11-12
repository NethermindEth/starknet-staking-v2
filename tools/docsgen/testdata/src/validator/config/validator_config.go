package config

import (
	"os"
	"strconv"
)

type Provider struct {
	HTTP string `json:"http"`
}

func ProviderFromEnv() Provider {
	return Provider{HTTP: os.Getenv("PROVIDER_HTTP_URL")}
}

type Reward struct {
	ClaimThreshold uint64 `json:"claimThreshold"`
	ClaimMaxFee    uint64 `json:"claimMaxFee,omitempty"`
	Braavos        *bool  `json:"braavos"`
}

func RewardFromEnv() (Reward, error) {
	var claimThreshold uint64
	if value := os.Getenv("REWARD_CLAIM_THRESHOLD"); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return Reward{}, err
		}
		claimThreshold = parsed
	}

	var braavos *bool
	if value := os.Getenv("REWARD_BRAAVOS_ACCOUNT"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return Reward{}, err
		}
		braavos = &parsed
	}

	return Reward{
		ClaimThreshold: claimThreshold,
		ClaimMaxFee:    parseFee(os.Getenv("REWARD_CLAIM_MAX_FEE")),
		Braavos:        braavos,
	}, nil
}

type Config struct {
	Provider Provider `json:"provider"`
	Reward   Reward   `json:"reward"`
}

func FromEnv() (Config, error) {
	reward, err := RewardFromEnv()
	if err != nil {
		return Config{}, err
	}

	return Config{Provider: ProviderFromEnv(), Reward: reward}, nil
}
