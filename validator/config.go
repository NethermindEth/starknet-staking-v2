package validator

import (
	"encoding/json"
	"errors"
	"math/big"
	"os"

	"github.com/NethermindEth/juno/core/crypto"
	"github.com/NethermindEth/juno/core/felt"
)

func isZero[T comparable](v T) bool {
	var x T
	return v == x
}

type Provider struct {
	Http string `json:"http"`
	Ws   string `json:"ws"`
}

// Merge its missing fields with data from other provider
func (p *Provider) Fill(other *Provider) {
	if isZero(p.Http) {
		p.Http = other.Http
	}
	if isZero(p.Ws) {
		p.Ws = other.Ws
	}
}

type Signer struct {
	ExternalUrl        string `json:"url"`
	PrivKey            string `json:"privateKey"`
	OperationalAddress string `json:"operationalAddress"`
}

// Merge its missing fields with data from other signer
func (s *Signer) Fill(other *Signer) {
	if isZero(s.ExternalUrl) {
		s.ExternalUrl = other.ExternalUrl
	}
	if isZero(s.PrivKey) {
		s.PrivKey = other.PrivKey
	}
	if isZero(s.OperationalAddress) {
		s.OperationalAddress = other.OperationalAddress
	}
}

func (s *Signer) External() bool {
	return s.ExternalUrl != ""
}

type Config struct {
	Provider Provider `json:"provider"`
	Signer   Signer   `json:"signer"`
}

// Function to load and parse the JSON file
func ConfigFromFile(filePath string) (Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return Config{}, err
	}
	return ConfigFromData(data)
}

func ConfigFromData(data []byte) (Config, error) {
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
	if err := checkProvider(&c.Provider); err != nil {
		return err
	}
	if err := checkSigner(&c.Signer); err != nil {
		return err
	}
	return nil
}

func checkProvider(provider *Provider) error {
	if provider.Http == "" {
		return errors.New("http provider url not set in provider config")
	}
	if provider.Ws == "" {
		return errors.New("ws provider url not set in provder config")
	}
	return nil
}

func checkSigner(signer *Signer) error {
	if signer.OperationalAddress == "" {
		return errors.New("operational address is not set in signer config")
	}
	if signer.External() {
		return nil
	}
	if signer.PrivKey == "" {
		return errors.New("neither private key nor url properties set in signer config")
	}
	return nil
}

func ComputeBlockNumberToAttestTo(epochInfo *EpochInfo, attestWindow uint64) BlockNumber {
	hash := crypto.PoseidonArray(
		new(felt.Felt).SetBigInt(epochInfo.Stake.Big()),
		new(felt.Felt).SetUint64(epochInfo.EpochId),
		epochInfo.StakerAddress.Felt(),
	)

	hashBigInt := new(big.Int)
	hashBigInt = hash.BigInt(hashBigInt)

	blockOffset := new(big.Int)
	blockOffset = blockOffset.Mod(hashBigInt, big.NewInt(int64(epochInfo.EpochLen-attestWindow)))

	return BlockNumber(epochInfo.CurrentEpochStartingBlock.Uint64() + blockOffset.Uint64())
}
