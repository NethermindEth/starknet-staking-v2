package main_test

import (
	"sync"
	"testing"

	"github.com/NethermindEth/juno/utils"
	main "github.com/NethermindEth/starknet-staking-v2"
	"github.com/NethermindEth/starknet.go/rpc"
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

		attestation, exists := pendingAttestations[main.BlockNumber(currentBlockHeader.BlockNumber+main.MIN_ATTESTATION_WINDOW)]
		require.Equal(t, true, exists)
		require.Equal(t, main.AttestRequiredWithValidity{
			AttestRequired: main.AttestRequired{
				BlockHash: main.BlockHash(*currentBlockHeader.BlockHash),
			},
			UntilBlockNumber: main.BlockNumber(currentBlockHeader.BlockNumber + attestationWindow),
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
			UntilBlockNumber: main.BlockNumber(1 + 20),
		}
		pendingAttestations[main.BlockNumber(1+main.MIN_ATTESTATION_WINDOW)] = scheduledAttest

		activeAttestations := make(map[main.BlockNumber][]main.AttestRequired)

		// Current block number is not the beginning of any attestation window (1 block before here)
		currentBlockNumber := main.BlockNumber(10)

		// Call
		main.MovePendingAttestationsToActive(pendingAttestations, activeAttestations, currentBlockNumber)

		// Assert pending attestations hasn't changed
		require.Equal(t, 1, len(pendingAttestations))
		expectedScheduledAttest, exists := pendingAttestations[main.BlockNumber(1+main.MIN_ATTESTATION_WINDOW)]
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
			UntilBlockNumber: main.BlockNumber(1 + 20),
		}
		pendingAttestations[main.BlockNumber(1+main.MIN_ATTESTATION_WINDOW)] = scheduledAttest

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
