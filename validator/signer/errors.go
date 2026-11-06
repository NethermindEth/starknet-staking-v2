package signer

import (
	"fmt"
	"strings"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/utils"
)

func entrypointInternalError(entrypointName string, err error) error {
	return fmt.Errorf("%s: %w", "Error when calling entrypoint `"+entrypointName+"`", err)
}

func entrypointResponseError(entrypointName string, result []*felt.Felt) error {
	stringArr := utils.FeltArrToStringArr(result)
	stringErr := "[" + strings.Join(stringArr, ", ") + "]"

	return fmt.Errorf(
		"invalid response from entrypoint `%s`. Response: %s", entrypointName, stringErr,
	)
}
