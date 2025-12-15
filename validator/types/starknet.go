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

func (a *Address) String() string {
	return (*felt.Felt)(a).String()
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
