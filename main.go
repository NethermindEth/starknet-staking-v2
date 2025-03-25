package main

import (
	"fmt"
	"sync"

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

func main() {
	var config Config // read from somewhere

	provider := NewProvider(config.providerUrl)

	validatorAccount := NewValidatorAccount(provider, &config.accountData)

	dispatcher := NewEventDispatcher()
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go dispatcher.Dispatch(&validatorAccount, make(map[BlockHash]AttestationStatus), wg)
	// I have to make sure this function closes at the end

	// ------

	// Subscribe to the block headers
	blockHeaderFeed := make(chan *rpc.BlockHeader) // could maybe make it buffered to allow for margin?
	go subscribeToBlockHeader(config.providerUrl, blockHeaderFeed)

	// Initially, fetch necessary info
	//
	// Note 1 (attest info): No need to listen to real-time events, and fetching once per epoch should work,
	// as any important updates (ie, related to stake & epoch_length) are effective only from the next epoch!
	//
	// Note 2 (attest window): Depending on the expected behaviour of attestation window, we might have to listen to `AttestationWindowChanged` event
	attestationInfo, attestationWindow, blockNumberToAttestTo, err := fetchEpochInfo(&validatorAccount)
	if err != nil {
		// TODO: implement a retry mechanism ?
	}

	// Attestations waiting for their window (only 1 / block at most as MIN_ATTESTATION_WINDOW is constant)
	pendingAttestations := make(map[BlockNumber]AttestRequiredWithValidity)

	// Attestations in their sending window
	activeAttestations := make(map[BlockNumber][]AttestRequired)

	for blockHeader := range blockHeaderFeed {
		fmt.Println("Block header:", blockHeader)

		// Re-fetch epoch info on new epoch (validity guaranteed for 1 epoch even if updates are made)
		if blockHeader.BlockNumber == attestationInfo.CurrentEpochStartingBlock.ToUint64()+attestationInfo.EpochLen {
			previousEpochInfo := attestationInfo

			attestationInfo, attestationWindow, blockNumberToAttestTo, err = fetchEpochInfo(&validatorAccount)
			if err != nil {
				// TODO: implement a retry mechanism ?
			}

			// Sanity check
			if attestationInfo.EpochId != previousEpochInfo.EpochId+1 ||
				attestationInfo.CurrentEpochStartingBlock.ToUint64() != previousEpochInfo.CurrentEpochStartingBlock.ToUint64()+previousEpochInfo.EpochLen {
				// TODO: give more details concerning the epoch info
				fmt.Printf("Wrong epoch change: from %d to %d", previousEpochInfo.EpochId, attestationInfo.EpochId)
				// TODO: what should we do ?
			}
		}

		schedulePendingAttestations(blockHeader, blockNumberToAttestTo, pendingAttestations, attestationWindow)

		movePendingAttestationsToActive(pendingAttestations, activeAttestations, BlockNumber(blockHeader.BlockNumber))

		sendAllActiveAttestations(activeAttestations, &dispatcher, BlockNumber(blockHeader.BlockNumber))
	}

	// --> I think we don't need to listen to stake events, we can get it when fetching AttestationInfo
	//
	// I also need to check if the staked amount of the validator changes
	// The solution here is to subscribe to a possible event emitting
	// If it happens, send a StakeUpdated event with the necessary information

	// I'd also like to check the balance of the address from time to time to verify
	// that they have enough money for the next 10 attestations (value modifiable by user)
	// Once it goes below it, the console should start giving warnings
	// This the least prio but we should implement nonetheless

	// Should also track re-org and check if the re-org means we have to attest again or not
}

func fetchEpochInfo(account Accounter) (AttestationInfo, uint64, BlockNumber, error) {
	attestationInfo, attestInfoErr := fetchAttestationInfo(account)
	if attestInfoErr != nil {
		return AttestationInfo{}, 0, 0, attestInfoErr
	}

	attestationWindow, windowErr := fetchAttestationWindow(account)
	if windowErr != nil {
		return AttestationInfo{}, 0, 0, windowErr
	}

	blockNumberToAttestTo := computeBlockNumberToAttestTo(account, attestationInfo, attestationWindow)

	return attestationInfo, attestationWindow, blockNumberToAttestTo, nil
}

func computeBlockNumberToAttestTo(account Accounter, attestationInfo AttestationInfo, attestationWindow uint64) BlockNumber {
	startingBlock := attestationInfo.CurrentEpochStartingBlock.ToUint64() + attestationInfo.EpochLen

	// TODO: might be hash(stake, hash(epoch_id, address))
	// or should we use PoseidonArray instead ?
	accountAddress := account.Address()
	hash := crypto.Poseidon(
		crypto.Poseidon(
			new(felt.Felt).SetBigInt(attestationInfo.Stake.Big()),
			new(felt.Felt).SetUint64(attestationInfo.EpochId),
		),
		&accountAddress,
	)
	// TODO: hash (felt) will most likely not fit into a uint64 --> use big.Int in that case ?
	blockOffset := hash.Uint64() % (attestationInfo.EpochLen - attestationWindow)

	return BlockNumber(startingBlock + blockOffset)
}

func schedulePendingAttestations(
	currentBlockHeader *rpc.BlockHeader,
	blockNumberToAttestTo BlockNumber,
	pendingAttestations map[BlockNumber]AttestRequiredWithValidity,
	attestationWindow uint64,
) {
	// If we are at the block number to attest to
	if BlockNumber(currentBlockHeader.BlockNumber) == blockNumberToAttestTo {
		// Schedule the attestation to be sent starting at the beginning of attestation window
		pendingAttestations[BlockNumber(currentBlockHeader.BlockNumber+MIN_ATTESTATION_WINDOW)] = AttestRequiredWithValidity{
			AttestRequired: AttestRequired{
				BlockHash: BlockHash(*currentBlockHeader.BlockHash),
			},
			untilBlockNumber: BlockNumber(currentBlockHeader.BlockNumber + attestationWindow),
		}
	}
}

func movePendingAttestationsToActive(
	pendingAttestations map[BlockNumber]AttestRequiredWithValidity,
	activeAttestations map[BlockNumber][]AttestRequired,
	currentBlockNumber BlockNumber,
) {
	// If we are at the beginning of some attestation window
	if pending, pendingExists := pendingAttestations[currentBlockNumber]; pendingExists {
		// Initialize map for attestations active until end of the window
		if _, activeExists := activeAttestations[pending.untilBlockNumber]; !activeExists {
			activeAttestations[pending.untilBlockNumber] = make([]AttestRequired, 1)
		}

		// Move pending attestation to active
		activeAttestations[pending.untilBlockNumber] = append(activeAttestations[pending.untilBlockNumber], pending.AttestRequired)

		// Remove from pending
		delete(pendingAttestations, currentBlockNumber)
	}
}

func sendAllActiveAttestations(
	activeAttestations map[BlockNumber][]AttestRequired,
	dispatcher *EventDispatcher,
	currentBlockNumber BlockNumber,
) {
	for untilBlockNumber, attestations := range activeAttestations {
		if currentBlockNumber <= untilBlockNumber {
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
