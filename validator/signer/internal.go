package signer

import (
	"context"
	"fmt"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/curve"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
	"github.com/cockroachdb/errors"
	"go.uber.org/zap"
)

var _ Signer = (*InternalSigner)(nil)

type InternalSigner struct {
	Account             account.Account
	logger              log.Logger
	validationContracts types.ValidationContracts
	// Max tip, in FRI per L2 gas unit, to pay for the attestation transaction
	maxTip uint64
}

func NewInternalSigner(
	ctx context.Context,
	provider rpc.RPCProvider,
	logger log.Logger,
	signer *config.Signer,
	addresses *config.ContractAddresses,
	maxTip uint64,
) (InternalSigner, error) {
	privateKey, err := types.PrivateKeyFromString(signer.PrivKey)
	if err != nil {
		return InternalSigner{}, fmt.Errorf("invalid private key: %w", err)
	}

	publicKey, _ := curve.PrivateKeyToPoint(privateKey)
	publicKeyStr := publicKey.String()
	ks := account.SetNewMemKeystore(publicKeyStr, privateKey)

	accountAddr, err := types.AddressFromString(signer.OperationalAddress)
	if err != nil {
		return InternalSigner{}, fmt.Errorf("invalid operational address: %w", err)
	}
	acc, err := account.NewAccount(provider, accountAddr.Felt(), publicKeyStr, ks, 2)
	if err != nil {
		return InternalSigner{}, errors.Errorf("cannot create internal signer: %w", err)
	}

	chainIDStr, err := provider.ChainID(ctx)
	if err != nil {
		return InternalSigner{}, err
	}
	validationContracts, err := types.ValidationContractsFromAddresses(
		addresses.SetDefaults(chainIDStr),
	)
	if err != nil {
		return InternalSigner{}, fmt.Errorf("invalid staking contracts: %w", err)
	}

	logger.Debug(
		"internal signer has been set up",
		zap.String("address", accountAddr.String()),
	)

	return InternalSigner{
		Account:             *acc,
		logger:              logger,
		validationContracts: validationContracts,
		maxTip:              maxTip,
	}, nil
}

func (s *InternalSigner) TransactionStatus(
	ctx context.Context, transactionHash *felt.Felt,
) (
	*rpc.TxnStatusResult, error,
) {
	return s.Account.Provider.TransactionStatus(ctx, transactionHash)
}

func (s *InternalSigner) BuildAttestTransaction(
	ctx context.Context,
	blockhash *types.BlockHash,
) (rpc.BroadcastInvokeTxnV3, error) {
	calls := []rpc.InvokeFunctionCall{{
		ContractAddress: s.ValidationContracts().Attest.Felt(),
		FunctionName:    "attest",
		CallData:        []*felt.Felt{blockhash.Felt().Clone()},
	}}
	calldata, err := s.Account.FmtCalldata(utils.InvokeFuncCallsToFunctionCalls(calls))
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, fmt.Errorf("failed to format calldata: %w", err)
	}

	nonce, err := s.Account.Nonce(ctx)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, fmt.Errorf("failed to update the account nonce: %w", err)
	}

	defaultResources := makeDefaultResources()

	tip, err := estimateAttestTip(ctx, s.Account.Provider, s.maxTip, s.logger)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, err
	}

	// Taken from starknet.go `utils.BuildInvokeTxn`
	attestTransaction := rpc.BroadcastInvokeTxnV3{
		Type:                  rpc.TransactionTypeInvoke,
		SenderAddress:         s.Account.Address,
		Calldata:              calldata,
		Version:               rpc.TransactionV3WithQueryBit,
		Signature:             []*felt.Felt{},
		Nonce:                 nonce,
		ResourceBounds:        &defaultResources,
		Tip:                   tip,
		PayMasterData:         []*felt.Felt{},
		AccountDeploymentData: []*felt.Felt{},
		NonceDataMode:         rpc.DAModeL1,
		FeeMode:               rpc.DAModeL1,
		Proof:                 []string{},
		ProofFacts:            []*felt.Felt{},
	}

	return attestTransaction, nil
}

func (s *InternalSigner) EstimateFee(
	ctx context.Context, txn *rpc.BroadcastInvokeTxnV3,
) (rpc.FeeEstimation, error) {
	return estimateFee(ctx, s, s.Account.Provider, txn)
}

func (s *InternalSigner) SignTransaction(
	ctx context.Context,
	txn *rpc.BroadcastInvokeTxnV3,
) (*rpc.BroadcastInvokeTxnV3, error) {
	return txn, s.Account.SignInvokeTransaction(ctx, txn)
}

func (s *InternalSigner) InvokeTransaction(
	ctx context.Context,
	txn *rpc.BroadcastInvokeTxnV3,
) (rpc.AddInvokeTransactionResponse, error) {
	return s.Account.Provider.AddInvokeTransaction(ctx, txn)
}

func (s *InternalSigner) Call(
	ctx context.Context, call rpc.FunctionCall, blockID rpc.BlockID,
) ([]*felt.Felt, error) {
	return s.Account.Provider.Call(ctx, call, blockID)
}

func (s *InternalSigner) BlockWithTxHashes(
	ctx context.Context, blockID rpc.BlockID,
) (any, error) {
	return s.Account.Provider.BlockWithTxHashes(ctx, blockID)
}

func (s *InternalSigner) Address() *types.Address {
	return (*types.Address)(s.Account.Address)
}

func (s *InternalSigner) ValidationContracts() *types.ValidationContracts {
	return &s.validationContracts
}

func (s *InternalSigner) Nonce(ctx context.Context) (*felt.Felt, error) {
	return s.Account.Nonce(ctx)
}
