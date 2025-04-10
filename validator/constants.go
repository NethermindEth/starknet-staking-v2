package validator

const (
	STAKING_CONTRACT_ADDRESS          = "0x034370fc9931c636ab07b16ada82d60f05d32993943debe2376847e0921c1162"
	ATTEST_CONTRACT_ADDRESS           = "0x04862e05d00f2d0981c4a912269c21ad99438598ab86b6e70d1cee267caaa78d"
	STRK_CONTRACT_ADDRESS             = "0x04718f5a0fc34cc1af16a1cdee98ffb20c31f5cd61d6ab07201858f4287c938d"
	ETH_CONTRACT_ADDRESS              = "0x049d36570d4e46f48e99674bd3fcc84644ddd6b96f7c741b1562b82f9e004dc7"
	MIN_ATTESTATION_WINDOW    uint64  = 11
	DEFAULT_MAX_RETRIES       int     = 10
	FEE_ESTIMATION_MULTIPLIER float64 = 1.5
)

var (
	StakingContract   = AddressFromString(STAKING_CONTRACT_ADDRESS)
	AttestContract    = AddressFromString(ATTEST_CONTRACT_ADDRESS)
	StrkTokenContract = AddressFromString(STRK_CONTRACT_ADDRESS)
)
