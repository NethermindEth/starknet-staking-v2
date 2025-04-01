package main

import (
	"math/big"

	"github.com/NethermindEth/juno/core/crypto"
	"github.com/NethermindEth/juno/core/felt"
)

type AccountData struct {
	address string
	privKey string
	pubKey  string
}

func NewAccountData(address string, privKey string, pubKey string) AccountData {
	return AccountData{
		address,
		privKey,
		pubKey,
	}
}

// struct should be (un)marshallable
type Config struct {
	providerUrl string
	accountData AccountData
}

func LoadConfig(path string) Config {
	return Config{}
}

func main() {
	config := LoadConfig("some path")
	Attest(&config)
}

func ComputeBlockNumberToAttestTo[Account Accounter](account Account, attestationInfo EpochInfo, attestationWindow uint64) BlockNumber {
	accountAddress := account.Address()
	hash := crypto.PoseidonArray(
		new(felt.Felt).SetBigInt(attestationInfo.Stake.Big()),
		new(felt.Felt).SetUint64(attestationInfo.EpochId),
		accountAddress,
	)

	var hashBigInt *big.Int = new(big.Int)
	hashBigInt = hash.BigInt(hashBigInt)

	var blockOffsetBigInt *big.Int = new(big.Int)
	blockOffsetBigInt = blockOffsetBigInt.Mod(hashBigInt, big.NewInt(int64(attestationInfo.EpochLen-attestationWindow)))

	return BlockNumber(attestationInfo.CurrentEpochStartingBlock.Uint64() + blockOffsetBigInt.Uint64())
}
