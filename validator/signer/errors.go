package signer

import (
	"errors"
	"fmt"
	"strings"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/utils"
)

func entrypointInternalError(entrypointName string, err error) error {
	return errors.New("Error when calling entrypoint `" + entrypointName + "`: " + err.Error())
}

func entrypointResponseError(entrypointName string, result []*felt.Felt) error {
	stringArr := utils.FeltArrToStringArr(result)
	stringErr := "[" + strings.Join(stringArr, ", ") + "]"
	return fmt.Errorf(
		"invalid response from entrypoint %s. Response: %s", entrypointName, stringErr,
	)
}
