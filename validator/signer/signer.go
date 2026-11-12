package signer

import (
	"context"
	"fmt"
	"math/big"

	"github.com/NethermindEth/juno/core/crypto"
	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
	"go.uber.org/zap"
	"lukechampine.com/uint128"
)

//go:generate go tool mockgen -destination=../../mocks/mock_signer.go -package=mocks github.com/NethermindEth/starknet-staking-v2/validator/signer Signer
type Signer interface {
	// Methods from Starknet.go Account implementation
	TransactionStatus(
		ctx context.Context, transactionHash *felt.Felt,
	) (*rpc.TxnStatusResult, error)

	BuildAttestTransaction(
		ctx context.Context, blockHash *types.BlockHash,
	) (rpc.BroadcastInvokeTxnV3, error)
	EstimateFee(ctx context.Context, txn *rpc.BroadcastInvokeTxnV3) (rpc.FeeEstimation, error)
	SignTransaction(
		ctx context.Context, txn *rpc.BroadcastInvokeTxnV3,
	) (*rpc.BroadcastInvokeTxnV3, error)
	InvokeTransaction(
		ctx context.Context, txn *rpc.BroadcastInvokeTxnV3,
	) (rpc.AddInvokeTransactionResponse, error)

	Call(ctx context.Context, call rpc.FunctionCall, blockID rpc.BlockID) ([]*felt.Felt, error)
	BlockWithTxHashes(ctx context.Context, blockID rpc.BlockID) (any, error)

	// Property Access
	Nonce(ctx context.Context) (*felt.Felt, error)
	Address() *types.Address
	ValidationContracts() *types.ValidationContracts
}

// I believe all these functions down here should be methods
// Postponing for now to not affect test code

func FetchEpochInfo[S Signer](ctx context.Context, signer S) (types.EpochInfo, error) {
	functionCall := rpc.FunctionCall{
		ContractAddress:    signer.ValidationContracts().Staking.Felt(),
		EntryPointSelector: utils.GetSelectorFromNameFelt(epochInfoEntrypoint),
		Calldata:           []*felt.Felt{signer.Address().Felt()},
	}

	result, err := signer.Call(ctx, functionCall, rpc.WithBlockTag(rpc.BlockTagLatest))
	if err != nil {
		return types.EpochInfo{}, epochInfoCallError(signer, &functionCall, err)
	}

	if len(result) != 5 { //nolint:mnd // The expected result length
		return types.EpochInfo{}, entrypointResponseError(epochInfoEntrypoint, result)
	}

	stake := result[1].Bits()

	return types.EpochInfo{
		StakerAddress: types.Address(*result[0]),
		Stake:         uint128.New(stake[0], stake[1]),
		EpochLen:      result[2].Uint64(),
		EpochID:       result[3].Uint64(),
		StartingBlock: types.BlockNumber(result[4].Uint64()),
	}, nil
}

func FetchAttestWindow[S Signer](ctx context.Context, signer S) (uint64, error) {
	functionCall := rpc.FunctionCall{
		ContractAddress:    signer.ValidationContracts().Attest.Felt(),
		EntryPointSelector: utils.GetSelectorFromNameFelt(attestWindowEntrypoint),
		Calldata:           []*felt.Felt{},
	}
	result, err := signer.Call(ctx, functionCall, rpc.WithBlockTag(rpc.BlockTagLatest))
	if err != nil {
		return 0, contractCallError(attestContractName, &functionCall, err)
	}

	if len(result) != 1 {
		return 0, entrypointResponseError(attestWindowEntrypoint, result)
	}

	return result[0].Uint64(), nil
}

// Fetches the STRK balance of any account, using the signer only to perform the call
func FetchBalance[S Signer](
	ctx context.Context, signer S, address *types.Address,
) (types.Balance, error) {
	StrkTokenContract, err := types.AddressFromString(constants.StrkContractAddress)
	if err != nil {
		return types.Balance{}, fmt.Errorf("invalid STRK contract address: %w", err)
	}
	functionCall := rpc.FunctionCall{
		ContractAddress:    StrkTokenContract.Felt(),
		EntryPointSelector: utils.GetSelectorFromNameFelt(balanceOfEntrypoint),
		Calldata:           []*felt.Felt{address.Felt()},
	}
	result, err := signer.Call(ctx, functionCall, rpc.WithBlockTag(rpc.BlockTagLatest))
	if err != nil {
		return types.Balance{}, contractCallError(strkContractName, &functionCall, err)
	}

	if len(result) != 2 {
		return types.Balance{}, entrypointResponseError(balanceOfEntrypoint, result)
	}

	return types.NewBalance(result[0], result[1]), nil
}

func FetchEpochAndAttestInfo[S Signer](
	ctx context.Context, signer S, logger log.Logger,
) (types.EpochInfo, types.AttestInfo, error) {
	epochInfo, err := FetchEpochInfo(ctx, signer)
	if err != nil {
		return types.EpochInfo{}, types.AttestInfo{}, err
	}
	logger.Debug(
		"fetched epoch info",
		zap.Uint64("epochID", epochInfo.EpochID),
		zap.Uint64("epochStartingBlock", epochInfo.StartingBlock.Uint64()),
		zap.Uint64("epochEndingBlock",
			epochInfo.StartingBlock.Uint64()+epochInfo.EpochLen,
		),
	)
	attestWindow, windowErr := FetchAttestWindow(ctx, signer)
	if windowErr != nil {
		return types.EpochInfo{}, types.AttestInfo{}, windowErr
	}

	blockNum := ComputeBlockNumberToAttestTo(&epochInfo, attestWindow)

	//nolint:exhaustruct_v5 // Purposely not using the block hash
	attestInfo := types.AttestInfo{
		TargetBlock: blockNum,
		WindowStart: blockNum + types.BlockNumber(constants.MinAttestationWindow),
		WindowEnd:   blockNum + types.BlockNumber(attestWindow),
	}

	logger.Debug(
		"data received and parsed",
		zap.Any("epoch", epochInfo),
		zap.Any("attestation", attestInfo),
	)

	return epochInfo, attestInfo, nil
}

func estimateFee(
	ctx context.Context,
	signer Signer,
	provider rpc.RPCProvider,
	txn *rpc.BroadcastInvokeTxnV3,
) (rpc.FeeEstimation, error) {
	query := *txn
	if query.Version != rpc.TransactionV3WithQueryBit {
		query.Version = rpc.TransactionV3WithQueryBit
		if _, err := signer.SignTransaction(ctx, &query); err != nil {
			return rpc.FeeEstimation{}, err
		}
	}

	estimates, err := provider.EstimateFee(
		ctx,
		[]rpc.BroadcastTxn{&query},
		[]rpc.EstimateFeeFlag{},
		rpc.WithBlockTag(rpc.BlockTagPreConfirmed),
	)
	if err != nil {
		return rpc.FeeEstimation{}, err
	}

	return singleFeeEstimate(estimates)
}

func singleFeeEstimate(estimates []rpc.FeeEstimation) (rpc.FeeEstimation, error) {
	if len(estimates) != 1 {
		return rpc.FeeEstimation{}, fmt.Errorf(
			"invalid fee estimate response: expected 1 estimate, got %d", len(estimates),
		)
	}

	estimate := estimates[0]
	fields := []struct {
		name  string
		value *felt.Felt
	}{
		{name: "l1_gas_consumed", value: estimate.L1GasConsumed},
		{name: "l1_gas_price", value: estimate.L1GasPrice},
		{name: "l2_gas_consumed", value: estimate.L2GasConsumed},
		{name: "l2_gas_price", value: estimate.L2GasPrice},
		{name: "l1_data_gas_consumed", value: estimate.L1DataGasConsumed},
		{name: "l1_data_gas_price", value: estimate.L1DataGasPrice},
		{name: "overall_fee", value: estimate.OverallFee},
	}
	for _, field := range fields {
		if field.value == nil {
			return rpc.FeeEstimation{}, fmt.Errorf(
				"invalid fee estimate response: missing %s", field.name,
			)
		}
	}

	return estimate, nil
}

// Caps the tip to maxTip. Returns the resulting tip and whether it was capped
func capTip(tip rpc.U64, maxTip uint64) (rpc.U64, bool, error) {
	uintTip, err := tip.ToUint64()
	if err != nil {
		return "", false, fmt.Errorf("invalid tip %q: %w", tip, err)
	}
	if uintTip <= maxTip {
		return tip, false, nil
	}

	return rpc.U64(fmt.Sprintf("0x%x", maxTip)), true, nil
}

// Estimates the tip for the attestation transaction, capped to maxTip. A warning is
// logged if the estimated tip is above maxTip
func estimateAttestTip(
	ctx context.Context, provider rpc.RPCProvider, maxTip uint64, logger log.Logger,
) (rpc.U64, error) {
	if maxTip == 0 {
		return "0x0", nil
	}

	estimatedTip, err := estimateTip(ctx, provider)
	if err != nil {
		return "", fmt.Errorf("estimate tip: %w", err)
	}

	tip, capped, err := capTip(estimatedTip, maxTip)
	if err != nil {
		return "", fmt.Errorf("cap tip: %w", err)
	}
	if capped {
		logger.Warn(
			"Estimated tip exceeds the max tip, using the max tip instead",
			zap.String("estimatedTip", string(estimatedTip)),
			zap.String("maxTip", string(tip)),
		)
	}

	return tip, nil
}

func ComputeBlockNumberToAttestTo(
	epochInfo *types.EpochInfo,
	attestWindow uint64,
) types.BlockNumber {
	hash := crypto.PoseidonElems(
		new(felt.Felt).SetBigInt(epochInfo.Stake.Big()),
		new(felt.Felt).SetUint64(epochInfo.EpochID),
		epochInfo.StakerAddress.Felt(),
	)

	hashBigInt := new(big.Int)
	hashBigInt = hash.BigInt(hashBigInt)

	blockOffset := new(big.Int)
	blockOffset = blockOffset.Mod(
		hashBigInt,
		new(big.Int).SetUint64(epochInfo.EpochLen-attestWindow),
	)

	return types.BlockNumber(epochInfo.StartingBlock.Uint64() + blockOffset.Uint64())
}
