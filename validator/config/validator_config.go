package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

var (
	errSignerBothKeysAndURL = errors.New(
		"both private key and external signer URL set in signer configuration where only one is allowed",
	)
	errSignerNoKeysOrURL = errors.New(
		"neither private key nor external url set in signer configuration",
	)

	errRewardAddressNotSet  = errors.New("address is not set in reward configuration")
	errRewardBothKeysAndURL = errors.New(
		"both private key and external signer URL set in reward configuration where only one is allowed",
	)
	errRewardNoKeysOrURL = errors.New(
		"neither private key nor external url set in reward configuration",
	)
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
	if s.PrivKey != "" && s.ExternalURL != "" {
		return errSignerBothKeysAndURL
	}
	if s.PrivKey == "" && s.ExternalURL == "" {
		return errSignerNoKeysOrURL
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

// Addresses of the contracts the validator interacts with. An empty address
// means the default one for the chain is used
type Contracts struct {
	Attest  string `json:"attest"`
	Staking string `json:"staking"`
}

func ContractsFromEnv() Contracts {
	return Contracts{
		Attest:  os.Getenv("ATTEST_CONTRACT_ADDRESS"),
		Staking: os.Getenv("STAKING_CONTRACT_ADDRESS"),
	}
}

// Merge its missing fields with data from other contracts
func (c *Contracts) Fill(other *Contracts) {
	if isZero(c.Attest) {
		c.Attest = other.Attest
	}
	if isZero(c.Staking) {
		c.Staking = other.Staking
	}
}

// Metrics server configuration. A nil field means the option was not set, which
// is different from it being set to its zero value
type Metrics struct {
	Enabled *bool   `json:"enabled"`
	Host    *string `json:"host"`
	Port    *string `json:"port"`
}

// The port can be written either as a string or as a number
func (m *Metrics) UnmarshalJSON(data []byte) error {
	// A type without methods, so that unmarshalling it doesn't call this again
	type plain Metrics
	aux := struct {
		*plain
		// Shadows the port of the embedded type
		Port json.RawMessage `json:"port"`
	}{plain: (*plain)(m), Port: nil}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	port, err := stringOrNumber(aux.Port)
	if err != nil {
		return fmt.Errorf("metrics.port: %w", err)
	}
	m.Port = port

	return nil
}

func MetricsFromEnv() (Metrics, error) {
	var enabled *bool
	if value := os.Getenv("VALIDATOR_METRICS_ENABLED"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return Metrics{}, fmt.Errorf(
				"invalid VALIDATOR_METRICS_ENABLED env var, not a valid boolean: %w",
				err,
			)
		}
		enabled = &parsed
	}

	var host *string
	if value := os.Getenv("VALIDATOR_METRICS_HOST"); value != "" {
		host = &value
	}

	var port *string
	if value := os.Getenv("VALIDATOR_METRICS_PORT"); value != "" {
		port = &value
	}

	return Metrics{
		Enabled: enabled,
		Host:    host,
		Port:    port,
	}, nil
}

// Merge its missing fields with data from other metrics
func (m *Metrics) Fill(other *Metrics) {
	if m.Enabled == nil {
		m.Enabled = other.Enabled
	}
	if m.Host == nil {
		m.Host = other.Host
	}
	if m.Port == nil {
		m.Port = other.Port
	}
}

// The pointer fields are nil when the option was not set, which is different
// from it being set to its zero value
type Config struct {
	Provider  Provider  `json:"provider"`
	Signer    Signer    `json:"signer"`
	Reward    Reward    `json:"reward"`
	Contracts Contracts `json:"contracts"`
	Metrics   Metrics   `json:"metrics"`
	// Either a positive integer or the key word 'infinite'
	MaxRetries *string `json:"maxRetries"`
	// Balance, in STRK, below which a warning is triggered
	BalanceThreshold *float64 `json:"balanceThreshold"`
	LogLevel         *string  `json:"logLevel"`
	// Max tip, in FRI per L2 gas unit, to pay for the attestation transaction. It's a
	// pointer so that an explicit 0 (never pay a tip) can be told apart from a value not
	// set, and isn't overridden by a lower priority config source
	MaxTip *uint64 `json:"maxTip"`
}

// The max retries can be written either as a string or as a number
func (c *Config) UnmarshalJSON(data []byte) error {
	// A type without methods, so that unmarshalling it doesn't call this again
	type plain Config
	aux := struct {
		*plain
		// Shadows the max retries of the embedded type
		MaxRetries json.RawMessage `json:"maxRetries"`
	}{plain: (*plain)(c), MaxRetries: nil}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	maxRetries, err := stringOrNumber(aux.MaxRetries)
	if err != nil {
		return fmt.Errorf("maxRetries: %w", err)
	}
	c.MaxRetries = maxRetries

	return nil
}

// Reads a JSON value written either as a string or as a number, keeping the
// number as it was written. It returns nil when the value is missing or null
func stringOrNumber(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return &text, nil
	}

	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return nil, errors.New("expected a string or a number")
	}
	text = number.String()

	return &text, nil
}

func FromEnv() (Config, error) {
	reward, err := RewardFromEnv()
	if err != nil {
		return Config{}, err
	}
	metrics, err := MetricsFromEnv()
	if err != nil {
		return Config{}, err
	}

	var maxRetries *string
	if value := os.Getenv("VALIDATOR_MAX_RETRIES"); value != "" {
		maxRetries = &value
	}

	var balanceThreshold *float64
	if value := os.Getenv("VALIDATOR_BALANCE_THRESHOLD"); value != "" {
		var parsed float64
		parsed, err = strconv.ParseFloat(value, 64)
		if err != nil {
			return Config{}, fmt.Errorf(
				"invalid VALIDATOR_BALANCE_THRESHOLD env var, not a valid number: %w",
				err,
			)
		}
		balanceThreshold = &parsed
	}

	var logLevel *string
	if value := os.Getenv("VALIDATOR_LOG_LEVEL"); value != "" {
		logLevel = &value
	}

	var maxTip *uint64
	if value := os.Getenv("VALIDATOR_MAX_TIP"); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf(
				"invalid VALIDATOR_MAX_TIP env var, not a valid integer: %w",
				err,
			)
		}
		maxTip = &parsed
	}

	return Config{
		Provider:         ProviderFromEnv(),
		Signer:           SignerFromEnv(),
		Reward:           reward,
		Contracts:        ContractsFromEnv(),
		Metrics:          metrics,
		MaxRetries:       maxRetries,
		BalanceThreshold: balanceThreshold,
		LogLevel:         logLevel,
		MaxTip:           maxTip,
	}, nil
}

// Function to load and parse the JSON file
func FromFile(filePath string) (Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return Config{}, fmt.Errorf("cannot read config file: %w", err)
	}

	config, err := FromData(data)
	if err != nil {
		return Config{}, fmt.Errorf("cannot parse config file %s: %w", filePath, err)
	}

	return config, nil
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
	c.Contracts.Fill(&other.Contracts)
	c.Metrics.Fill(&other.Metrics)
	if c.MaxRetries == nil {
		c.MaxRetries = other.MaxRetries
	}
	if c.BalanceThreshold == nil {
		c.BalanceThreshold = other.BalanceThreshold
	}
	if c.LogLevel == nil {
		c.LogLevel = other.LogLevel
	}
	if c.MaxTip == nil {
		c.MaxTip = other.MaxTip
	}
}

// Sets default values for the fields not provided by any config source
func (c *Config) SetDefaults() {
	c.Reward.SetDefaults()
}

// Returns the max tip, or max uint64 (unlimited) if not set
func (c *Config) MaxTipOrDefault() uint64 {
	if c.MaxTip == nil {
		return math.MaxUint64
	}

	return *c.MaxTip
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

type Source struct {
	Name   string
	Config *Config
}

func (c *Config) CheckSources(sources ...Source) error {
	err := c.Check()
	switch {
	case errors.Is(err, errSignerBothKeysAndURL):
		return fmt.Errorf("%w (%s)", err, signingSources(sources, signerSigning))
	case errors.Is(err, errRewardBothKeysAndURL):
		return fmt.Errorf("%w (%s)", err, signingSources(sources, rewardSigning))
	default:
		return err
	}
}

func signerSigning(c *Config) (privKey, url string) {
	return c.Signer.PrivKey, c.Signer.ExternalURL
}

func rewardSigning(c *Config) (privKey, url string) {
	return c.Reward.PrivKey, c.Reward.ExternalURL
}

func signingSources(sources []Source, signing func(*Config) (privKey, url string)) string {
	privKeySources := make([]string, 0, len(sources))
	urlSources := make([]string, 0, len(sources))
	for _, source := range sources {
		privKey, url := signing(source.Config)
		if privKey != "" {
			privKeySources = append(privKeySources, source.Name)
		}
		if url != "" {
			urlSources = append(urlSources, source.Name)
		}
	}

	return fmt.Sprintf(
		"private key set by %s, external URL set by %s",
		strings.Join(privKeySources, " and "),
		strings.Join(urlSources, " and "),
	)
}

func isZero[T comparable](v T) bool {
	var x T

	return v == x
}
