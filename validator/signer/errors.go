package signer

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/client/rpcerr"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
)

const (
	epochInfoEntrypoint    = "get_attestation_info_by_operational_address"
	attestWindowEntrypoint = "attestation_window"
	balanceOfEntrypoint    = "balance_of"
	stakerInfoEntrypoint   = "get_staker_info_v1"
	attestEntrypoint       = "attest"
	claimRewardsEntrypoint = "claim_rewards"
	executeEntrypoint      = "__execute__"
	validateEntrypoint     = "__validate__"
)

const (
	stakingContractName = "staking"
	attestContractName  = "attestation"
	strkContractName    = "STRK"
)

const (
	byteArrayWordBytes  = 31
	byteArrayFixedFelts = 3
	feltListSeparator   = ", "
	emptyRevertReason   = "empty revert reason"
	hexDigits           = "0123456789abcdefABCDEF"
	stakerNotFound      = "Staker does not exist"
)

var byteArrayMagic = felt.NewUnsafeFromString[felt.Felt](
	"0x46a6158a16a947e5916b2a2ca68501a45e93d7110e81aa2d6438b1c57c879a3",
)

var entrypointNames = selectorNames(
	epochInfoEntrypoint,
	attestWindowEntrypoint,
	balanceOfEntrypoint,
	stakerInfoEntrypoint,
	attestEntrypoint,
	claimRewardsEntrypoint,
	executeEntrypoint,
	validateEntrypoint,
)

func selectorNames(names ...string) map[felt.Felt]string {
	selectors := make(map[felt.Felt]string, len(names))
	for _, name := range names {
		selectors[*utils.GetSelectorFromNameFelt(name)] = name
	}

	return selectors
}

type contractRevertError struct {
	reason string
	text   string
	err    error
}

func (e *contractRevertError) Error() string { return e.text }

func (e *contractRevertError) Unwrap() error { return e.err }

func ReadableContractError(err error) error {
	return readableContractError(err, nil)
}

func readableContractError(err error, call *rpc.FunctionCall) error {
	var rpcErr *rpcerr.RPCError
	if !errors.As(err, &rpcErr) {
		return err
	}
	revert, ok := executionError(rpcErr)
	if !ok {
		return err
	}
	leaf, frame := innermostRevert(revert)
	reason, ok := describeRevertReason(leaf)
	text, raw := err.Error(), rpcErr.Error()
	if !ok || !strings.Contains(text, raw) {
		return err
	}

	readable := reason
	if location := describeFrame(frame, call); location != "" {
		readable += " (" + location + ")"
	}

	return &contractRevertError{
		reason: reason,
		text:   strings.Replace(text, raw, readable, 1),
		err:    err,
	}
}

func executionError(rpcErr *rpcerr.RPCError) (*rpc.ContractExecutionError, bool) {
	switch data := rpcErr.Data.(type) {
	case *rpc.ContractErrData:
		if data != nil {
			return &data.RevertError, true
		}
	case *rpc.TransactionExecErrData:
		if data != nil {
			return &data.ExecutionError, true
		}
	}

	return nil, false
}

func innermostRevert(
	revert *rpc.ContractExecutionError,
) (string, *rpc.ContractExecutionErrorInner) {
	var frame *rpc.ContractExecutionErrorInner
	for revert != nil && revert.ContractExecErrInner != nil {
		frame = revert.ContractExecErrInner
		revert = frame.Error
	}
	if revert == nil {
		return "", frame
	}

	return revert.Message, frame
}

func describeFrame(frame *rpc.ContractExecutionErrorInner, call *rpc.FunctionCall) string {
	if frame == nil || isCalledFrame(frame, call) {
		return ""
	}

	var parts []string
	if frame.ContractAddress != nil {
		parts = append(parts, "contract "+frame.ContractAddress.String())
	}
	if frame.Selector != nil {
		if name, ok := entrypointNames[*frame.Selector]; ok {
			parts = append(parts, "entrypoint "+name)
		} else {
			parts = append(parts, "selector "+frame.Selector.String())
		}
	}

	return strings.Join(parts, feltListSeparator)
}

func isCalledFrame(frame *rpc.ContractExecutionErrorInner, call *rpc.FunctionCall) bool {
	if call == nil || frame.ContractAddress == nil || frame.Selector == nil {
		return false
	}

	return frame.ContractAddress.Equal(call.ContractAddress) &&
		frame.Selector.Equal(call.EntryPointSelector)
}

func describeRevertReason(leaf string) (string, bool) {
	text := strings.Join(strings.Fields(leaf), " ")
	if text == "" {
		return "", false
	}
	if unquoted, ok := unquote(text); ok {
		if unquoted == "" {
			return emptyRevertReason, true
		}

		return unquoted, true
	}
	felts, ok := parseFeltList(text)
	if !ok {
		return text, true
	}
	items, ok := panicItems(felts, strings.Split(text, feltListSeparator))
	if !ok {
		return text, true
	}
	if len(items) == 1 {
		return items[0].text, true
	}

	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = item.text
		if item.decoded {
			parts[i] = "'" + item.text + "'"
		}
	}

	return strings.Join(parts, feltListSeparator), true
}

func unquote(text string) (string, bool) {
	unquoted, ok := strings.CutPrefix(text, `"`)
	if !ok {
		return "", false
	}
	unquoted, ok = strings.CutSuffix(unquoted, `"`)
	if !ok || strings.Contains(unquoted, `"`) {
		return "", false
	}

	return unquoted, true
}

type panicItem struct {
	text    string
	decoded bool
}

func panicItems(felts []*felt.Felt, raw []string) ([]panicItem, bool) {
	items := make([]panicItem, 0, len(felts))
	for i := 0; i < len(felts); {
		if felts[i].Equal(byteArrayMagic) {
			if decoded, size, ok := decodeByteArray(felts[i+1:]); ok {
				items = append(items, panicItem{text: decoded, decoded: true})
				i += 1 + size

				continue
			}
			if i == 0 {
				return nil, false
			}
		}
		if decoded, ok := shortString(felts[i]); ok {
			items = append(items, panicItem{text: decoded, decoded: true})
		} else {
			items = append(items, panicItem{text: raw[i], decoded: false})
		}
		i++
	}

	return items, true
}

func parseFeltList(text string) ([]*felt.Felt, bool) {
	parts := strings.Split(text, feltListSeparator)
	felts := make([]*felt.Felt, len(parts))
	for i, part := range parts {
		digits, ok := strings.CutPrefix(part, "0x")
		if !ok || digits == "" || strings.Trim(digits, hexDigits) != "" {
			return nil, false
		}
		value, err := new(felt.Felt).SetString(part)
		if err != nil {
			return nil, false
		}
		felts[i] = value
	}

	return felts, true
}

func decodeByteArray(data []*felt.Felt) (decoded string, size int, ok bool) {
	var fullWords uint64
	for size = byteArrayFixedFelts; size <= len(data); size++ {
		if data[0].Equal(new(felt.Felt).SetUint64(fullWords)) {
			decoded, ok = byteArrayString(data[:size])

			return decoded, size, ok
		}
		fullWords++
	}

	return "", 0, false
}

func byteArrayString(data []*felt.Felt) (string, bool) {
	pendingWord, pendingLen := data[len(data)-2], data[len(data)-1]
	for _, word := range data[1 : len(data)-2] {
		if byteLength(word) > byteArrayWordBytes {
			return "", false
		}
	}
	maxPendingLen := new(felt.Felt).SetUint64(byteArrayWordBytes - 1)
	if pendingLen.Cmp(maxPendingLen) > 0 || byteLength(pendingWord) > pendingLen.Uint64() {
		return "", false
	}

	decoded, err := utils.ByteArrFeltToString(data)
	if err != nil || !utf8.ValidString(decoded) {
		return "", false
	}
	if decoded = strings.Join(strings.Fields(decoded), " "); decoded == "" {
		return emptyRevertReason, true
	}

	return decoded, true
}

func byteLength(value *felt.Felt) uint64 {
	var length uint64
	significant := false
	for _, b := range value.Bytes() {
		significant = significant || b != 0
		if significant {
			length++
		}
	}

	return length
}

func shortString(value *felt.Felt) (string, bool) {
	bytes := value.Bytes()
	text := strings.TrimLeft(string(bytes[:]), "\x00")
	if text == "" {
		return "", false
	}
	for i := range len(text) {
		if text[i] < ' ' || text[i] > '~' {
			return "", false
		}
	}

	return text, true
}

func contractCallError(contractName string, call *rpc.FunctionCall, err error) error {
	name, ok := entrypointNames[*call.EntryPointSelector]
	if !ok {
		name = call.EntryPointSelector.String()
	}

	return fmt.Errorf(
		"calling %s on the %s contract %s failed: %w",
		name, contractName, call.ContractAddress, readableContractError(err, call),
	)
}

func epochInfoCallError(signer Signer, call *rpc.FunctionCall, err error) error {
	callErr := contractCallError(stakingContractName, call, err)
	var revertErr *contractRevertError
	if !errors.As(callErr, &revertErr) || !strings.Contains(revertErr.reason, stakerNotFound) {
		return callErr
	}
	network := ""
	if chainID := signer.ValidationContracts().ChainID; chainID != "" {
		network = " on " + chainID
	}

	return fmt.Errorf(
		"%w; the operational address %s is not registered as a staker%s "+
			"(check --signer-op-address and --staking-contract-address)",
		callErr, signer.Address(), network,
	)
}

func entrypointResponseError(entrypointName string, result []*felt.Felt) error {
	stringArr := utils.FeltArrToStringArr(result)
	stringErr := "[" + strings.Join(stringArr, ", ") + "]"

	return fmt.Errorf(
		"invalid response from entrypoint `%s`. Response: %s", entrypointName, stringErr,
	)
}
