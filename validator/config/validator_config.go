package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
)

var (
	errRewardAddressNotSet        = errors.New("address is not set in reward configuration")
	errRewardBothKeysAndURL       = errors.New("both private key and external signer URL set in reward configuration where only one is allowed")
	errRewardNoKeysOrURL          = errors.New("you must set either a private key or an external signer URL in reward configuration")
	errRewardClaimThresholdNotSet = errors.New(
		"claim threshold must be set to a positive value in reward configuration",
	)
)

type Provider struct {
	HTTP string `json:"http"`
	WS   string `json:"ws"`
}

func ProviderFromEnv() Provider {
	return Provider{
		HTTP: os.Getenv("PROVIDER_HTTP_URL"),
		WS:   os.Getenv("PROVIDER_WS_URL"),
	}
}

func (p *Provider) Check() error {
	if p.HTTP == "" {
		return errors.New("http provider url not set in provider configuration")
	}
	if p.WS == "" {
		return errors.New("ws provider url not set in provider configuration")
	}

	return nil
}

// Merge its missing fields with data from other provider
func (p *Provider) Fill(other *Provider) {
	if isZero(p.HTTP) {
		p.HTTP = other.HTTP
	}
	if isZero(p.WS) {
		p.WS = other.WS
	}
}

type Signer struct {
	ExternalURL        string `json:"url"`
	PrivKey            string `json:"privateKey"`
	OperationalAddress string `json:"operationalAddress"`
}

func (s *Signer) Check() error {
	if s.OperationalAddress == "" {
		return errors.New("operational address is not set in signer configuration")
	}
	if s.IsExternal() {
		return nil
	}
	if s.PrivKey == "" {
		return errors.New("neither private key nor external url set in signer configuration")
	}

	return nil
}

func SignerFromEnv() Signer {
	return Signer{
		ExternalURL:        os.Getenv("SIGNER_EXTERNAL_URL"),
		PrivKey:            os.Getenv("SIGNER_PRIVATE_KEY"),
		OperationalAddress: os.Getenv("SIGNER_OPERATIONAL_ADDRESS"),
	}
}

// Merge its missing fields with data from other signer
func (s *Signer) Fill(other *Signer) {
	if isZero(s.ExternalURL) {
		s.ExternalURL = other.ExternalURL
	}
	if isZero(s.PrivKey) {
		s.PrivKey = other.PrivKey
	}
	if isZero(s.OperationalAddress) {
		s.OperationalAddress = other.OperationalAddress
	}
}

func (s *Signer) IsExternal() bool {
	return s.ExternalURL != ""
}

type Reward struct {
	ExternalURL string `json:"url"`
	PrivKey     string `json:"privateKey"`
	Address     string `json:"address"`
	// Amount of unclaimed rewards, in STRK, that triggers an automatic claim
	ClaimThreshold uint64 `json:"claimThreshold"`
	// Max fee, in FRI, to pay for the claim transaction
	ClaimMaxFee uint64 `json:"claimMaxFee"`
}

func RewardFromEnv() (Reward, error) {
	var claimThreshold uint64
	if value := os.Getenv("REWARD_CLAIM_THRESHOLD"); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return Reward{}, fmt.Errorf(
				"invalid REWARD_CLAIM_THRESHOLD env var, not a valid integer: %w",
				err,
			)
		}
		claimThreshold = parsed
	}

	var claimMaxFee uint64
	if value := os.Getenv("REWARD_CLAIM_MAX_FEE"); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return Reward{}, fmt.Errorf(
				"invalid REWARD_CLAIM_MAX_FEE env var, not a valid integer: %w",
				err,
			)
		}
		claimMaxFee = parsed
	}

	return Reward{
		ExternalURL:    os.Getenv("REWARD_EXTERNAL_URL"),
		PrivKey:        os.Getenv("REWARD_PRIVATE_KEY"),
		Address:        os.Getenv("REWARD_ADDRESS"),
		ClaimThreshold: claimThreshold,
		ClaimMaxFee:    claimMaxFee,
	}, nil
}

// Reward configuration is optional, but if any field is set, it requires the address
// and exactly one signing method: either a private key or an external url
func (r *Reward) Check() error {
	if !r.IsSet() {
		return nil
	}
	if r.Address == "" {
		return errRewardAddressNotSet
	}
	if r.PrivKey != "" && r.ExternalURL != "" {
		return errRewardBothKeysAndURL
	}
	if r.PrivKey == "" && r.ExternalURL == "" {
		return errRewardNoKeysOrURL
	}
	if r.ClaimThreshold == 0 {
		return errRewardClaimThresholdNotSet
	}

	return nil
}

func (r *Reward) IsSet() bool {
	return r.Address != "" ||
		r.PrivKey != "" ||
		r.ExternalURL != "" ||
		r.ClaimThreshold != 0 ||
		r.ClaimMaxFee != 0
}

// Sets default values for the fields not provided. It should be called after
// all config sources are merged, otherwise the defaults would take priority
func (r *Reward) SetDefaults() {
	if !r.IsSet() {
		return
	}
	if isZero(r.ClaimMaxFee) {
		r.ClaimMaxFee = math.MaxUint64
	}
}

func (r *Reward) IsExternal() bool {
	return r.ExternalURL != ""
}

// Merge its missing fields with data from other reward
func (r *Reward) Fill(other *Reward) {
	if isZero(r.ExternalURL) {
		r.ExternalURL = other.ExternalURL
	}
	if isZero(r.PrivKey) {
		r.PrivKey = other.PrivKey
	}
	if isZero(r.Address) {
		r.Address = other.Address
	}
	if isZero(r.ClaimThreshold) {
		r.ClaimThreshold = other.ClaimThreshold
	}
	if isZero(r.ClaimMaxFee) {
		r.ClaimMaxFee = other.ClaimMaxFee
	}
}

type Config struct {
	Provider Provider `json:"provider"`
	Signer   Signer   `json:"signer"`
	Reward   Reward   `json:"reward"`
}

func FromEnv() (Config, error) {
	reward, err := RewardFromEnv()
	if err != nil {
		return Config{}, err
	}

	return Config{
		Provider: ProviderFromEnv(),
		Signer:   SignerFromEnv(),
		Reward:   reward,
	}, nil
}

// Function to load and parse the JSON file
func FromFile(filePath string) (Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return Config{}, err
	}

	return FromData(data)
}

func FromData(data []byte) (Config, error) {
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, err
	}

	return config, nil
}

// Fills its missing fields with data from other config
func (c *Config) Fill(other *Config) {
	c.Provider.Fill(&other.Provider)
	c.Signer.Fill(&other.Signer)
	c.Reward.Fill(&other.Reward)
}

// Sets default values for the fields not provided by any config source
func (c *Config) SetDefaults() {
	c.Reward.SetDefaults()
}

// Verifies its data is appropiatly set
func (c *Config) Check() error {
	if err := c.Provider.Check(); err != nil {
		return fmt.Errorf("provider: %w", err)
	}
	if err := c.Signer.Check(); err != nil {
		return fmt.Errorf("signer: %w", err)
	}
	if err := c.Reward.Check(); err != nil {
		return fmt.Errorf("reward: %w", err)
	}

	return nil
}

func isZero[T comparable](v T) bool {
	var x T

	return v == x
}
