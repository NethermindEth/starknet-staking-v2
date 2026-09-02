package types

import (
	"fmt"

	"github.com/NethermindEth/juno/core/felt"
)

type Address felt.Felt

func (a *Address) Felt() *felt.Felt {
	return (*felt.Felt)(a)
}

func AddressFromString(addrStr string) Address {
	adr, err := new(felt.Felt).SetString(addrStr)
	if err != nil {
		panic(fmt.Sprintf("cannot turn string `%s` into an address: %s", addrStr, err.Error()))
	}

	return Address(*adr)
}

// Convert the address to a string with the "0x" prefix and the length of 66.
func (a *Address) String() string {
	length := 66
	hexStr := (*felt.Felt)(a).String()

	// Check if the hex value is already of the desired length
	if len(hexStr) >= length {
		return hexStr
	}

	// Extract the hex value without the "0x" prefix
	hexValue := hexStr[2:]
	// Pad zeros after the "0x" prefix
	paddedHexValue := fmt.Sprintf("%0*s", length-2, hexValue)
	// Add back the "0x" prefix to the padded hex value
	paddedHexStr := "0x" + paddedHexValue

	return paddedHexStr
}

func (a *Address) UnmarshalJSON(data []byte) error {
	var f felt.Felt
	if err := f.UnmarshalJSON(data); err != nil {
		return err
	}
	*a = Address(f)

	return nil
}

func (a Address) MarshalJSON() ([]byte, error) {
	return (*felt.Felt)(&a).MarshalJSON()
}

type BlockHash felt.Felt

func (b *BlockHash) Felt() *felt.Felt {
	return (*felt.Felt)(b)
}

func (b *BlockHash) String() string {
	return (*felt.Felt)(b).String()
}

type BlockNumber uint64

// Delete this method
func (b *BlockNumber) Uint64() uint64 {
	return uint64(*b)
}
