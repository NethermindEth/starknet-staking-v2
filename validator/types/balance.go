package types

import (
	"math/big"

	"github.com/NethermindEth/juno/core/felt"
)

type Balance big.Int

func NewBalance(low, high *felt.Felt) Balance {
	lowB := low.BigInt(new(big.Int))
	highB := high.BigInt(new(big.Int))

	highB = highB.Lsh(highB, 128)
	lowB = lowB.Add(lowB, highB)

	return Balance(*lowB)
}

func (b *Balance) Text(base int) string {
	return (*big.Int)(b).Text(base)
}

func (b *Balance) BigFloat() *big.Float {
	return new(big.Float).SetInt((*big.Int)(b))
}

// Divides the balance by 1e18 (wei strk unit) and turns it into a float.
// If it doesn't fit, the max float value without loss of precision is returned instead
func (b *Balance) Float() float64 {
	weiUnit := new(big.Float).SetUint64(1e18)
	bigF := b.BigFloat()
	bigF = bigF.Quo(bigF, weiUnit)

	f, _ := bigF.Float64()
	return f
	// if math.IsInf(f, 0) || math.IsNaN(f) {
	// 	return math.MaxFloat64, false
	// }
	// return f, true
}
