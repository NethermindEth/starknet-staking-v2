package types

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"

	"github.com/NethermindEth/juno/core/felt"
)

var hexFeltRegexp = regexp.MustCompile(`^0[xX][0-9a-fA-F]{1,64}$`)

const hexBase = 16

var starkCurveOrder, _ = new(big.Int).SetString(
	"800000000000010ffffffffffffffffb781126dcae7b2321e66a241adc64d2f", hexBase,
)

var (
	errPrivateKeyNotHex = errors.New(
		"it must be a 0x-prefixed hexadecimal number of at most 64 digits " +
			"(the value isn't shown, check it for quotes, spaces or a trailing newline)",
	)
	errPrivateKeyOutOfRange = errors.New(
		"it must be greater than 0 and lower than the Stark curve order",
	)
)

type Address felt.Felt

func (a *Address) Felt() *felt.Felt {
	return (*felt.Felt)(a)
}

func AddressFromString(addrStr string) (Address, error) {
	adr, err := felt.NewFromString[felt.Felt](addrStr)
	if err != nil {
		return Address{}, fmt.Errorf("cannot turn string `%s` into an address: %w", addrStr, err)
	}

	return Address(*adr), nil
}

func PrivateKeyFromString(keyStr string) (*big.Int, error) {
	if !hexFeltRegexp.MatchString(keyStr) {
		return nil, errPrivateKeyNotHex
	}
	key, _ := new(big.Int).SetString(keyStr[2:], hexBase)
	if key.Sign() == 0 || key.Cmp(starkCurveOrder) >= 0 {
		return nil, errPrivateKeyOutOfRange
	}

	return key, nil
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

func (a Address) MarshalText() ([]byte, error) {
	return felt.Felt(a).MarshalText()
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
