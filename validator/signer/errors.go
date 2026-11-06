package signer

import (
	"fmt"
	"strings"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/utils"
)

func entrypointInternalError(entrypointName string, err error) error {
	// Wrapping keeps the RPC error inspectable, so callers can tell a node outage from a
	// contract error
	//nolint:staticcheck // ST1005: the existing message text is kept unchanged
	return fmt.Errorf("Error when calling entrypoint `%s`: %w", entrypointName, err)
}

func entrypointResponseError(entrypointName string, result []*felt.Felt) error {
	stringArr := utils.FeltArrToStringArr(result)
	stringErr := "[" + strings.Join(stringArr, ", ") + "]"

	return fmt.Errorf(
		"invalid response from entrypoint `%s`. Response: %s", entrypointName, stringErr,
	)
}
