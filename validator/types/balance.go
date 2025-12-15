package types

import (
	"math/big"

	"github.com/NethermindEth/juno/core/felt"
)

// STRK balance represented in FRI
type Balance big.Int

func NewBalance(low, high *felt.Felt) Balance {
	lowB := low.BigInt(new(big.Int))
	highB := high.BigInt(new(big.Int))

	highB = highB.Lsh(highB, 128) //nolint:mnd // Shifting by 128 bits
	lowB = lowB.Add(lowB, highB)

	return Balance(*lowB)
}

func (b *Balance) Text(base int) string {
	return (*big.Int)(b).Text(base)
}

func (b *Balance) BigFloat() *big.Float {
	return new(big.Float).SetInt((*big.Int)(b))
}

// Returns the balance (represented in FRI) in STRK units as a float64.
// If it doesn't fit +Inf is returned
func (b *Balance) Strk() float64 {
	FRIUnit := new(big.Float).SetUint64(1e18) //nolint:mnd // 1e18 is the FRI unit
	bigF := b.BigFloat()
	bigF = bigF.Quo(bigF, FRIUnit)

	f, _ := bigF.Float64()

	return f
}
