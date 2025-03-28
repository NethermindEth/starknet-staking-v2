package main

import (
	"math/big"

	"github.com/NethermindEth/juno/core/crypto"
	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet.go/rpc"
)

type AccountData struct {
	address string
	privKey string
	pubKey  string
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

func computeBlockNumberToAttestTo(account Accounter, attestationInfo EpochInfo, attestationWindow uint64) BlockNumber {
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

func SchedulePendingAttestations(
	currentBlockHeader *rpc.BlockHeader,
	blockNumberToAttestTo BlockNumber,
	pendingAttestations map[BlockNumber]AttestRequiredWithValidity,
	attestationWindow uint64,
) {
	// If we are at the block number to attest to
	if BlockNumber(currentBlockHeader.BlockNumber) == blockNumberToAttestTo {
		// Schedule the attestation to be sent starting at the beginning of attestation window
		pendingAttestations[BlockNumber(currentBlockHeader.BlockNumber+MIN_ATTESTATION_WINDOW-1)] = AttestRequiredWithValidity{
			AttestRequired: AttestRequired{
				BlockHash: BlockHash(*currentBlockHeader.BlockHash),
			},
			Until: BlockNumber(currentBlockHeader.BlockNumber + attestationWindow),
		}
	}
}

func MovePendingAttestationsToActive(
	pendingAttestations map[BlockNumber]AttestRequiredWithValidity,
	activeAttestations map[BlockNumber][]AttestRequired,
	currentBlockNumber BlockNumber,
) {
	// If we are at the beginning of some attestation window
	if pending, pendingExists := pendingAttestations[currentBlockNumber]; pendingExists {
		// Initialize map for attestations active until end of the window
		if _, activeExists := activeAttestations[pending.Until]; !activeExists {
			activeAttestations[pending.Until] = make([]AttestRequired, 0, 1)
		}

		// Move pending attestation to active
		activeAttestations[pending.Until] = append(activeAttestations[pending.Until], pending.AttestRequired)

		// Remove from pending
		delete(pendingAttestations, currentBlockNumber)
	}
}

func SendAllActiveAttestations[Account Accounter](
	activeAttestations map[BlockNumber][]AttestRequired,
	dispatcher *EventDispatcher[Account],
	currentBlockNumber BlockNumber,
) {
	for untilBlockNumber, attestations := range activeAttestations {
		if currentBlockNumber < untilBlockNumber {
			// Send attestations to dispatcher
			for _, attestation := range attestations {
				dispatcher.AttestRequired <- attestation
			}
		} else {
			// Notify dispatcher of attestations to remove
			dispatcher.AttestationsToRemove <- utils.Map(
				attestations,
				func(attestation AttestRequired) BlockHash {
					return attestation.BlockHash
				},
			)

			// Remove attestations from active
			delete(activeAttestations, untilBlockNumber)
		}
	}
}
