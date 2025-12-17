package config

import (
	"encoding/json"
	"errors"
	"os"
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
	if s.External() {
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

func (s *Signer) External() bool {
	return s.ExternalURL != ""
}

type Config struct {
	Provider Provider `json:"provider"`
	Signer   Signer   `json:"signer"`
}

func FromEnv() Config {
	return Config{
		Provider: ProviderFromEnv(),
		Signer:   SignerFromEnv(),
	}
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
}

// Verifies its data is appropiatly set
func (c *Config) Check() error {
	if err := c.Provider.Check(); err != nil {
		return err
	}
	if err := c.Signer.Check(); err != nil {
		return err
	}

	return nil
}

func isZero[T comparable](v T) bool {
	var x T

	return v == x
}
