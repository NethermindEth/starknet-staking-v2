package validator

import (
	"context"
	"math/big"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/curve"
	"github.com/NethermindEth/starknet.go/hash"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
	"github.com/cockroachdb/errors"
	"lukechampine.com/uint128"
)

//go:generate mockgen -destination=./mocks/mock_account.go -package=mocks github.com/NethermindEth/starknet-staking-v2 Accounter
type Accounter interface {
	// Methods from account.Account
	GetTransactionStatus(ctx context.Context, transactionHash *felt.Felt) (*rpc.TxnStatusResp, error)
	BuildAndSendInvokeTxn(ctx context.Context, functionCalls []rpc.InvokeFunctionCall, multiplier float64) (*rpc.AddInvokeTransactionResponse, error)
	Call(ctx context.Context, call rpc.FunctionCall, blockId rpc.BlockID) ([]*felt.Felt, error)
	BlockWithTxHashes(ctx context.Context, blockID rpc.BlockID) (interface{}, error)

	// Custom Methods
	//
	// Want to return `Address` type here but it means creating a separate pkg
	// because otherwise mockgen tries to import this "main" pkg in its mock file
	// which is not allowed.
	// I think we should put this "types" file into a different pkg to be able to:
	// 1. Return `Address` type here
	// 2. Use "go generate" mock for this interface (only generating mock using `mockgen` cmd works now)
	Address() *felt.Felt
}

// Represents an internal signer where we hold the private keys
type InternalSigner account.Account

func NewInternalSigner[Log Logger](
	provider *rpc.Provider, logger Log, signer *Signer,
) (InternalSigner, error) {
	// todo(rdr): do we need to check the private key has maximum size
	privateKey, ok := new(big.Int).SetString(signer.PrivKey, 0)
	if !ok {
		return InternalSigner{}, errors.Errorf("private key %s into a big int", privateKey)
	}

	publicKey, _, err := curve.Curve.PrivateToPoint(privateKey)
	if err != nil {
		return InternalSigner{}, errors.New("Cannot derive public key from private key")
	}

	ks := account.SetNewMemKeystore(publicKey.String(), privateKey)

	accountAddr := AddressFromString(signer.OperationalAddress)
	account, err := account.NewAccount(provider, accountAddr.Felt(), publicKey.String(), ks, 2)
	if err != nil {
		return InternalSigner{}, errors.Errorf("Cannot create validator account: %s", err)
	}

	logger.Debugw("Validator account has been set up", "address", accountAddr.String())
	return InternalSigner(*account), nil
}

func (v *InternalSigner) GetTransactionStatus(ctx context.Context, transactionHash *felt.Felt) (*rpc.TxnStatusResp, error) {
	return ((*account.Account)(v)).Provider.GetTransactionStatus(ctx, transactionHash)
}

func (v *InternalSigner) BuildAndSendInvokeTxn(
	ctx context.Context,
	functionCalls []rpc.InvokeFunctionCall,
	multiplier float64,
) (*rpc.AddInvokeTransactionResponse, error) {
	return ((*account.Account)(v)).BuildAndSendInvokeTxn(ctx, functionCalls, multiplier)
}

func (v *InternalSigner) Call(ctx context.Context, call rpc.FunctionCall, blockId rpc.BlockID) ([]*felt.Felt, error) {
	return ((*account.Account)(v)).Provider.Call(ctx, call, blockId)
}

func (v *InternalSigner) BlockWithTxHashes(ctx context.Context, blockID rpc.BlockID) (interface{}, error) {
	return ((*account.Account)(v)).Provider.BlockWithTxHashes(ctx, blockID)
}

func (v *InternalSigner) Address() *felt.Felt {
	return v.AccountAddress
}

// Used as a wrapper around an exgernal signer implementation
type ExternalSigner struct {
	*rpc.Provider
	OperationalAddress Address
	ExternalSignerUrl  string
	ChainId            felt.Felt
}

func NewExternalSigner(provider *rpc.Provider, signer *Signer) (ExternalSigner, error) {
	chainID, err := provider.ChainID(context.Background())
	if err != nil {
		return ExternalSigner{}, err
	}
	chainId := new(felt.Felt).SetBytes([]byte(chainID))

	return ExternalSigner{
		Provider:           provider,
		OperationalAddress: AddressFromString(signer.OperationalAddress),
		ExternalSignerUrl:  signer.ExternalUrl,
		ChainId:            *chainId,
	}, nil
}

func (s *ExternalSigner) BuildAndSendInvokeTxn(
	ctx context.Context,
	functionCalls []rpc.InvokeFunctionCall,
	multiplier float64,
) (*rpc.AddInvokeTransactionResponse, error) {
	nonce, err := s.Nonce(ctx, rpc.WithBlockTag("pending"), s.Address())
	if err != nil {
		return nil, err
	}

	fnCallData := utils.InvokeFuncCallsToFunctionCalls(functionCalls)
	formattedCallData := account.FmtCallDataCairo2(fnCallData)

	// Building and signing the txn, as it needs a signature to estimate the fee
	broadcastInvokeTxnV3 := utils.BuildInvokeTxn(
		s.Address(),
		nonce,
		formattedCallData,
		makeResourceBoundsMapWithZeroValues(),
	)
	if err := SignInvokeTx(&broadcastInvokeTxnV3.InvokeTxnV3, &s.ChainId, s.ExternalSignerUrl); err != nil {
		return nil, err
	}

	// Estimate txn fee
	estimateFee, err := s.EstimateFee(ctx, []rpc.BroadcastTxn{broadcastInvokeTxnV3}, []rpc.SimulationFlag{}, rpc.WithBlockTag("pending"))
	if err != nil {
		return nil, err
	}
	txnFee := estimateFee[0]
	fillEmptyFeeEstimation(ctx, &txnFee, s.Provider) // temporary
	broadcastInvokeTxnV3.ResourceBounds = utils.FeeEstToResBoundsMap(txnFee, multiplier)

	// Signing the txn again with the estimated fee, as the fee value is used in the txn hash calculation
	if err := SignInvokeTx(&broadcastInvokeTxnV3.InvokeTxnV3, &s.ChainId, s.ExternalSignerUrl); err != nil {
		return nil, err
	}

	txRes, err := s.AddInvokeTransaction(ctx, broadcastInvokeTxnV3)
	if err != nil {
		return nil, err
	}

	return txRes, nil
}

func (s *ExternalSigner) Address() *felt.Felt {
	return s.OperationalAddress.Felt()
}

func SignInvokeTx(invokeTxnV3 *rpc.InvokeTxnV3, chainId *felt.Felt, externalSignerUrl string) error {
	hash, err := hash.TransactionHashInvokeV3(invokeTxnV3, chainId)
	if err != nil {
		return err
	}

	signResp, err := SignTxHash(hash, externalSignerUrl)
	if err != nil {
		return err
	}

	invokeTxnV3.Signature = signResp.Signature
	return nil
}

// When there's no transaction in the pending block, the L1DataGasConsumed and L1DataGasPrice fields are comming empty.
// This is causing the transaction to fail with the error:
// "55 Account validation failed: Max L1DataGas amount (0) is lower than the minimal gas amount: 128"
// This function fills the empty fields.
//
// TODO: remove this function once the issue is fixed in the RPC
func fillEmptyFeeEstimation(ctx context.Context, feeEstimation *rpc.FeeEstimation, provider rpc.RpcProvider) {
	if feeEstimation.L1DataGasConsumed.IsZero() {
		// default value for L1DataGasConsumed in most cases
		feeEstimation.L1DataGasConsumed = new(felt.Felt).SetUint64(224)
	}
	if feeEstimation.L1DataGasPrice.IsZero() {
		// getting the L1DataGasPrice from the latest block as reference
		result, _ := provider.BlockWithTxHashes(ctx, rpc.WithBlockTag("latest"))
		block := result.(*rpc.BlockTxHashes)
		feeEstimation.L1DataGasPrice = block.L1DataGasPrice.PriceInFRI
	}
}

func makeResourceBoundsMapWithZeroValues() rpc.ResourceBoundsMapping {
	return rpc.ResourceBoundsMapping{
		L1Gas: rpc.ResourceBounds{
			MaxAmount:       "0x0",
			MaxPricePerUnit: "0x0",
		},
		L1DataGas: rpc.ResourceBounds{
			MaxAmount:       "0x0",
			MaxPricePerUnit: "0x0",
		},
		L2Gas: rpc.ResourceBounds{
			MaxAmount:       "0x0",
			MaxPricePerUnit: "0x0",
		},
	}
}

// I believe all these functions down here should be methods
// Postponing for now to not affect test code

func FetchEpochInfo[Account Accounter](account Account) (EpochInfo, error) {

	functionCall := rpc.FunctionCall{
		ContractAddress:    StakingContract.Felt(),
		EntryPointSelector: utils.GetSelectorFromNameFelt("get_attestation_info_by_operational_address"),
		Calldata:           []*felt.Felt{account.Address()},
	}

	result, err := account.Call(context.Background(), functionCall, rpc.BlockID{Tag: "latest"})
	if err != nil {
		return EpochInfo{}, entrypointInternalError("get_attestation_info_by_operational_address", err)
	}

	if len(result) != 5 {
		return EpochInfo{}, entrypointResponseError("get_attestation_info_by_operational_address")
	}

	stake := result[1].Bits()
	return EpochInfo{
		StakerAddress:             Address(*result[0]),
		Stake:                     uint128.New(stake[0], stake[1]),
		EpochLen:                  result[2].Uint64(),
		EpochId:                   result[3].Uint64(),
		CurrentEpochStartingBlock: BlockNumber(result[4].Uint64()),
	}, nil
}

func FetchAttestWindow[Account Accounter](account Account) (uint64, error) {
	result, err := account.Call(
		context.Background(),
		rpc.FunctionCall{
			ContractAddress:    AttestContract.Felt(),
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
func FetchValidatorBalance[Account Accounter](account Account) (Balance, error) {
	result, err := account.Call(
		context.Background(),
		rpc.FunctionCall{
			ContractAddress:    StrkTokenContract.Felt(),
			EntryPointSelector: utils.GetSelectorFromNameFelt("balanceOf"),
			Calldata:           []*felt.Felt{account.Address()},
		},
		rpc.BlockID{Tag: "latest"},
	)

	if err != nil {
		return Balance{}, entrypointInternalError("balanceOf", err)
	}

	if len(result) != 1 {
		return Balance{}, entrypointResponseError("balanceOf")
	}

	return Balance(*result[0]), nil
}

// I believe this functions returns many things, it should probably be grouped under
// a unique type
func FetchEpochAndAttestInfo[Account Accounter, Log Logger](account Account, logger Log) (EpochInfo, AttestInfo, error) {
	epochInfo, err := FetchEpochInfo(account)
	if err != nil {
		return EpochInfo{}, AttestInfo{}, err
	}
	logger.Infow(
		"Fetched epoch info",
		"epoch ID", epochInfo.EpochId,
		"epoch starting block", epochInfo.CurrentEpochStartingBlock,
		"epoch ending block", epochInfo.CurrentEpochStartingBlock+BlockNumber(epochInfo.EpochLen),
	)

	attestWindow, windowErr := FetchAttestWindow(account)
	if windowErr != nil {
		return EpochInfo{}, AttestInfo{}, windowErr
	}

	blockNum := ComputeBlockNumberToAttestTo(account, &epochInfo, attestWindow)

	attestInfo := AttestInfo{
		TargetBlock: blockNum,
		WindowStart: blockNum + BlockNumber(MIN_ATTESTATION_WINDOW),
		WindowEnd:   blockNum + BlockNumber(attestWindow),
	}

	logger.Infow("Computed target block to attest to", "epoch ID", epochInfo.EpochId, "attestation info", attestInfo)
	return epochInfo, attestInfo, nil
}

func InvokeAttest[Account Accounter](account Account, attest *AttestRequired) (
	*rpc.AddInvokeTransactionResponse, error,
) {
	calls := []rpc.InvokeFunctionCall{{
		ContractAddress: AttestContract.Felt(),
		FunctionName:    "attest",
		CallData:        []*felt.Felt{attest.BlockHash.Felt()},
	}}

	return account.BuildAndSendInvokeTxn(context.Background(), calls, FEE_ESTIMATION_MULTIPLIER)
}
