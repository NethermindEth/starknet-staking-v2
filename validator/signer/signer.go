package signer

import (
	"math/big"

	"github.com/NethermindEth/juno/core/crypto"
	"github.com/NethermindEth/juno/core/felt"
	junoUtils "github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
	"lukechampine.com/uint128"
)

//go:generate go tool mockgen -destination=../../mocks/mock_signer.go -package=mocks github.com/NethermindEth/starknet-staking-v2/validator/signer Signer
type Signer interface {
	// Methods from Starknet.go Account implementation
	GetTransactionStatus(transactionHash *felt.Felt) (*rpc.TxnStatusResult, error)

	BuildAttestTransaction(blockHash *types.BlockHash) (rpc.BroadcastInvokeTxnV3, error)
	EstimateFee(txn *rpc.BroadcastInvokeTxnV3) (rpc.FeeEstimation, error)
	SignTransaction(txn *rpc.BroadcastInvokeTxnV3) (*rpc.BroadcastInvokeTxnV3, error)
	InvokeTransaction(txn *rpc.BroadcastInvokeTxnV3) (*rpc.AddInvokeTransactionResponse, error)

	BuildAndSendInvokeTxn(
		functionCalls []rpc.InvokeFunctionCall, multiplier float64,
	) (*rpc.AddInvokeTransactionResponse, error)
	Call(call rpc.FunctionCall, blockId rpc.BlockID) ([]*felt.Felt, error)
	BlockWithTxHashes(blockID rpc.BlockID) (any, error)

	// Property Access
	Nonce() (*felt.Felt, error)
	Address() *types.Address
	ValidationContracts() *types.ValidationContracts
}

// I believe all these functions down here should be methods
// Postponing for now to not affect test code

func FetchEpochInfo[S Signer](signer S) (types.EpochInfo, error) {
	functionCall := rpc.FunctionCall{
		ContractAddress: signer.ValidationContracts().Staking.Felt(),
		EntryPointSelector: utils.GetSelectorFromNameFelt(
			"get_attestation_info_by_operational_address",
		),
		Calldata: []*felt.Felt{signer.Address().Felt()},
	}

	result, err := signer.Call(functionCall, rpc.BlockID{Tag: "latest"})
	if err != nil {
		return types.EpochInfo{},
			entrypointInternalError("get_attestation_info_by_operational_address", err)
	}

	if len(result) != 5 {
		return types.EpochInfo{},
			entrypointResponseError("get_attestation_info_by_operational_address")
	}

	stake := result[1].Bits()
	return types.EpochInfo{
		StakerAddress: types.Address(*result[0]),
		Stake:         uint128.New(stake[0], stake[1]),
		EpochLen:      result[2].Uint64(),
		EpochId:       result[3].Uint64(),
		StartingBlock: types.BlockNumber(result[4].Uint64()),
	}, nil
}

func FetchAttestWindow[S Signer](signer S) (uint64, error) {
	result, err := signer.Call(
		rpc.FunctionCall{
			ContractAddress:    signer.ValidationContracts().Attest.Felt(),
			EntryPointSelector: utils.GetSelectorFromNameFelt("attestation_window"),
			Calldata:           []*felt.Felt{},
		},
		rpc.BlockID{Tag: "latest"},
	)
	if err != nil {
		return 0, entrypointInternalError("attestation_window", err)
	}

	if len(result) != 1 {
		return 0, entrypointResponseError("attestation_window")
	}

	return result[0].Uint64(), nil
}

// For near future when tracking validator's balance
func FetchValidatorBalance[Account Signer](account Account) (types.Balance, error) {
	StrkTokenContract := types.AddressFromString(constants.STRK_CONTRACT_ADDRESS)
	result, err := account.Call(
		rpc.FunctionCall{
			ContractAddress:    StrkTokenContract.Felt(),
			EntryPointSelector: utils.GetSelectorFromNameFelt("balanceOf"),
			Calldata:           []*felt.Felt{account.Address().Felt()},
		},
		rpc.BlockID{Tag: "latest"},
	)
	if err != nil {
		return types.Balance{}, entrypointInternalError("balanceOf", err)
	}

	if len(result) != 1 {
		return types.Balance{}, entrypointResponseError("balanceOf")
	}

	return types.Balance(*result[0]), nil
}

func FetchEpochAndAttestInfo[S Signer](
	signer S, logger *junoUtils.ZapLogger,
) (types.EpochInfo, types.AttestInfo, error) {
	epochInfo, err := FetchEpochInfo(signer)
	if err != nil {
		return types.EpochInfo{}, types.AttestInfo{}, err
	}
	logger.Debugw(
		"Fetched epoch info",
		"epoch ID", epochInfo.EpochId,
		"epoch starting block", epochInfo.StartingBlock,
		"epoch ending block", epochInfo.StartingBlock+
			types.BlockNumber(epochInfo.EpochLen),
	)

	attestWindow, windowErr := FetchAttestWindow(signer)
	if windowErr != nil {
		return types.EpochInfo{}, types.AttestInfo{}, windowErr
	}

	blockNum := ComputeBlockNumberToAttestTo(&epochInfo, attestWindow)

	attestInfo := types.AttestInfo{
		TargetBlock: blockNum,
		WindowStart: blockNum + types.BlockNumber(constants.MIN_ATTESTATION_WINDOW),
		WindowEnd:   blockNum + types.BlockNumber(attestWindow),
	}

	logger.Infof(
		"Target block to attest to at %d. Attestation window: %d <> %d",
		attestInfo.TargetBlock.Uint64(),
		attestInfo.WindowStart.Uint64(),
		attestInfo.WindowEnd.Uint64(),
	)
	logger.Debugw(
		"Epoch and Attestation info",
		"Epoch", epochInfo,
		"Attestation", attestInfo,
	)
	return epochInfo, attestInfo, nil
}

func BuildAttest[S Signer](signer S, blockHash *types.BlockHash, multiplier float64) (
	rpc.BroadcastInvokeTxnV3, error,
) {
	txn, err := signer.BuildAttestTransaction(blockHash)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, err
	}

	_, err = signer.SignTransaction(&txn)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, err
	}

	estimate, err := signer.EstimateFee(&txn)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, err
	}
	txn.ResourceBounds = utils.FeeEstToResBoundsMap(estimate, multiplier)

	// patch for making sure txn.Version is correct
	txn.Version = rpc.TransactionV3

	_, err = signer.SignTransaction(&txn)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, err
	}

	return txn, nil
}

func InvokeAttest[S Signer](signer S, blockhash *types.BlockHash) (
	*rpc.AddInvokeTransactionResponse, error,
) {
	calls := []rpc.InvokeFunctionCall{{
		ContractAddress: signer.ValidationContracts().Attest.Felt(),
		FunctionName:    "attest",
		CallData:        []*felt.Felt{blockhash.Felt()},
	}}

	return signer.BuildAndSendInvokeTxn(
		calls, constants.FEE_ESTIMATION_MULTIPLIER,
	)
}

func ComputeBlockNumberToAttestTo(epochInfo *types.EpochInfo, attestWindow uint64) types.BlockNumber {
	hash := crypto.PoseidonArray(
		new(felt.Felt).SetBigInt(epochInfo.Stake.Big()),
		new(felt.Felt).SetUint64(epochInfo.EpochId),
		epochInfo.StakerAddress.Felt(),
	)

	hashBigInt := new(big.Int)
	hashBigInt = hash.BigInt(hashBigInt)

	blockOffset := new(big.Int)
	blockOffset = blockOffset.Mod(hashBigInt, big.NewInt(int64(epochInfo.EpochLen-attestWindow)))

	return types.BlockNumber(epochInfo.StartingBlock.Uint64() + blockOffset.Uint64())
}
