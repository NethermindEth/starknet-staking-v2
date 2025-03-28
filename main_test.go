package main_test

import (
	"testing"

	"github.com/NethermindEth/juno/utils"
	main "github.com/NethermindEth/starknet-staking-v2"
	"github.com/NethermindEth/starknet-staking-v2/mocks"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/sourcegraph/conc"
	"github.com/stretchr/testify/require"
)

func TestSchedulePendingAttestations(t *testing.T) {
	t.Run("Not at block number to attest to", func(t *testing.T) {
		// Setup
		currentBlockHeader := rpc.BlockHeader{
			BlockNumber: 1,
			BlockHash:   utils.HexToFelt(t, "0x123"),
		}
		blockNumberToAttestTo := main.BlockNumber(2)
		pendingAttestations := make(map[main.BlockNumber]main.AttestRequiredWithValidity)
		attestationWindow := uint64(20)

		main.SchedulePendingAttestations(&currentBlockHeader, blockNumberToAttestTo, pendingAttestations, attestationWindow)

		// Assert
		require.Equal(t, 0, len(pendingAttestations))
	})

	t.Run("At block number to attest to registers attestation in map", func(t *testing.T) {
		// Setup
		currentBlockHeader := rpc.BlockHeader{
			BlockNumber: 1,
			BlockHash:   utils.HexToFelt(t, "0x123"),
		}
		blockNumberToAttestTo := main.BlockNumber(1)
		pendingAttestations := make(map[main.BlockNumber]main.AttestRequiredWithValidity)
		attestationWindow := uint64(20)

		main.SchedulePendingAttestations(&currentBlockHeader, blockNumberToAttestTo, pendingAttestations, attestationWindow)

		// Assert
		require.Equal(t, 1, len(pendingAttestations))

		attestation, exists := pendingAttestations[main.BlockNumber(currentBlockHeader.BlockNumber+main.MIN_ATTESTATION_WINDOW-1)]
		require.Equal(t, true, exists)
		require.Equal(t, main.AttestRequiredWithValidity{
			AttestRequired: main.AttestRequired{
				BlockHash: main.BlockHash(*currentBlockHeader.BlockHash),
			},
			Until: main.BlockNumber(currentBlockHeader.BlockNumber + attestationWindow),
		}, attestation)
	})
}

func TestMovePendingAttestationsToActive(t *testing.T) {
	t.Run("Not at beginning of any attestation window", func(t *testing.T) {
		// Setup
		pendingAttestations := make(map[main.BlockNumber]main.AttestRequiredWithValidity)
		scheduledAttest := main.AttestRequiredWithValidity{
			AttestRequired: main.AttestRequired{
				BlockHash: main.BlockHash(*utils.HexToFelt(t, "0x123")),
			},
			Until: main.BlockNumber(1 + 20),
		}
		pendingAttestations[main.BlockNumber(1+main.MIN_ATTESTATION_WINDOW-1)] = scheduledAttest

		activeAttestations := make(map[main.BlockNumber][]main.AttestRequired)

		// Current block number is not the beginning of any attestation window (1 block before here)
		currentBlockNumber := main.BlockNumber(10)

		// Call
		main.MovePendingAttestationsToActive(pendingAttestations, activeAttestations, currentBlockNumber)

		// Assert pending attestations hasn't changed
		require.Equal(t, 1, len(pendingAttestations))
		expectedScheduledAttest, exists := pendingAttestations[main.BlockNumber(1+main.MIN_ATTESTATION_WINDOW-1)]
		require.Equal(t, true, exists)
		require.Equal(t, scheduledAttest, expectedScheduledAttest)

		require.Equal(t, 0, len(activeAttestations))
	})

	t.Run("At beginning of some attestation window", func(t *testing.T) {
		// Setup
		pendingAttestations := make(map[main.BlockNumber]main.AttestRequiredWithValidity)
		scheduledAttest := main.AttestRequiredWithValidity{
			AttestRequired: main.AttestRequired{
				BlockHash: main.BlockHash(*utils.HexToFelt(t, "0x123")),
			},
			Until: main.BlockNumber(1 + 20),
		}
		pendingAttestations[main.BlockNumber(1+main.MIN_ATTESTATION_WINDOW-1)] = scheduledAttest

		activeAttestations := make(map[main.BlockNumber][]main.AttestRequired)

		// Current block number is the beginning of some attestation window
		currentBlockNumber := main.BlockNumber(11)

		// Call
		main.MovePendingAttestationsToActive(pendingAttestations, activeAttestations, currentBlockNumber)

		// Assert pending attest has moved to active
		require.Equal(t, 0, len(pendingAttestations))

		require.Equal(t, 1, len(activeAttestations))
		expectedActiveAttestations, exists := activeAttestations[main.BlockNumber(1+20)]
		require.Equal(t, true, exists)
		require.Equal(t, []main.AttestRequired{scheduledAttest.AttestRequired}, expectedActiveAttestations)
	})
}

func TestSendAllActiveAttestations(t *testing.T) {
	t.Run("Complete scenario: still active attestations & attestations to remove", func(t *testing.T) {
		// Setup
		activeAttestations := make(map[main.BlockNumber][]main.AttestRequired)

		// A set of active attestations
		attestationsUntilBlock21 := []main.AttestRequired{
			{
				BlockHash: main.BlockHash(*utils.HexToFelt(t, "0x123")),
			},
		}
		// Still within attest window (current block number is 14)
		activeAttestations[main.BlockNumber(21)] = attestationsUntilBlock21

		// Another set of active attestations
		attestationsUntilBlock15 := []main.AttestRequired{
			{
				BlockHash: main.BlockHash(*utils.HexToFelt(t, "0x456")),
			},
			{
				BlockHash: main.BlockHash(*utils.HexToFelt(t, "0x789")),
			},
		}
		// Still within attest window (current block number is 14)
		activeAttestations[main.BlockNumber(15)] = attestationsUntilBlock15

		// Set of attestations to remove
		attestationsToRemove := []main.AttestRequired{
			{
				BlockHash: main.BlockHash(*utils.HexToFelt(t, "0xabc")),
			},
			{
				BlockHash: main.BlockHash(*utils.HexToFelt(t, "0xdef")),
			},
		}
		// Attest window is now passed (max window bound is current block number)
		activeAttestations[main.BlockNumber(14)] = attestationsToRemove

		dispatcher := main.NewEventDispatcher[*mocks.MockAccount]()

		currentBlockNumber := main.BlockNumber(14)

		// Mock dispatcher: register received events
		receivedAttestRequired := make(map[main.AttestRequired]struct{})
		receivedAttestationsToRemove := make(map[main.BlockHash]struct{})
		wg := &conc.WaitGroup{}
		wg.Go(func() {
			registerReceivedEventsInDispatcher(t, &dispatcher, receivedAttestRequired, receivedAttestationsToRemove)
		})

		// Call
		main.SendAllActiveAttestations(activeAttestations, &dispatcher, currentBlockNumber)
		close(dispatcher.AttestRequired)
		close(dispatcher.AttestationsToRemove)

		// Wait for dispatcher to finish receiving & processing events
		wg.Wait()

		// Assert AttestRequired events
		expectedAttestRequired := make(map[main.AttestRequired]struct{}, 3)
		expectedAttestRequired[attestationsUntilBlock15[0]] = struct{}{}
		expectedAttestRequired[attestationsUntilBlock15[1]] = struct{}{}
		expectedAttestRequired[attestationsUntilBlock21[0]] = struct{}{}

		require.Equal(t, expectedAttestRequired, receivedAttestRequired)

		// Assert AttestationsToRemove events
		expectedAttestationsToRemove := make(map[main.BlockHash]struct{}, 2)
		expectedAttestationsToRemove[attestationsToRemove[0].BlockHash] = struct{}{}
		expectedAttestationsToRemove[attestationsToRemove[1].BlockHash] = struct{}{}

		require.Equal(t, expectedAttestationsToRemove, receivedAttestationsToRemove)

		// Assert active attestations to be removed have indeed been removed
		require.Equal(t, 2, len(activeAttestations))

		entryBlock15, exists := activeAttestations[main.BlockNumber(15)]
		require.True(t, exists)
		require.Equal(t, attestationsUntilBlock15, entryBlock15)

		entryBlock21, exists := activeAttestations[main.BlockNumber(21)]
		require.True(t, exists)
		require.Equal(t, attestationsUntilBlock21, entryBlock21)
	})
}

// Test helper function to register received events to assert on them
// Note: to exit this function, just close 1 of the 2 channels
func registerReceivedEventsInDispatcher[T main.Accounter](t *testing.T, dispatcher *main.EventDispatcher[T], receivedAttestRequired map[main.AttestRequired]struct{}, receivedAttestationsToRemove map[main.BlockHash]struct{}) {
	t.Helper()

	for {
		select {
		case attestRequired, ok := <-dispatcher.AttestRequired:
			if !ok {
				return
			}
			// register attestRequired event
			receivedAttestRequired[attestRequired] = struct{}{}
		case blockHashes, ok := <-dispatcher.AttestationsToRemove:
			if !ok {
				return
			}
			// register block hashes to remove
			for _, blockHash := range blockHashes {
				receivedAttestationsToRemove[blockHash] = struct{}{}
			}
		}
	}
}
