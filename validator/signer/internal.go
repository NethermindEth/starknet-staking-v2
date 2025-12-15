package signer

import (
	"context"
	"fmt"
	"math/big"

	"github.com/NethermindEth/juno/core/felt"
	junoUtils "github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/constants"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/curve"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
	"github.com/cockroachdb/errors"
)

var _ Signer = (*InternalSigner)(nil)

type InternalSigner struct {
	ctx     context.Context
	Account account.Account
	// If the account used represents a braavos account
	braavos             bool
	validationContracts types.ValidationContracts
}

func NewInternalSigner(
	ctx context.Context,
	provider *rpc.Provider,
	logger *junoUtils.ZapLogger,
	signer *config.Signer,
	addresses *config.ContractAddresses,
	braavos bool,
) (InternalSigner, error) {
	privateKey, ok := new(big.Int).SetString(signer.PrivKey, 0)
	if !ok {
		return InternalSigner{},
			errors.Errorf("cannot turn private key %s into a big int", privateKey)
	}

	publicKey, _ := curve.PrivateKeyToPoint(privateKey)
	publicKeyStr := publicKey.String()
	ks := account.SetNewMemKeystore(publicKeyStr, privateKey)

	accountAddr := types.AddressFromString(signer.OperationalAddress)
	acc, err := account.NewAccount(provider, accountAddr.Felt(), publicKeyStr, ks, 2)
	if err != nil {
		return InternalSigner{}, errors.Errorf("cannot create internal signer: %w", err)
	}

	chainIDStr, err := provider.ChainID(ctx)
	if err != nil {
		return InternalSigner{}, err
	}
	validationContracts := types.ValidationContractsFromAddresses(
		addresses.SetDefaults(chainIDStr),
	)
	logger.Infof("Validation contracts: %s", validationContracts.String())

	logger.Debugw("internal signer has been set up", "address", accountAddr.String())

	return InternalSigner{
		ctx:                 ctx,
		Account:             *acc,
		braavos:             braavos,
		validationContracts: validationContracts,
	}, nil
}

func (s *InternalSigner) TransactionStatus(transactionHash *felt.Felt) (
	*rpc.TxnStatusResult, error,
) {
	return s.Account.Provider.TransactionStatus(s.ctx, transactionHash)
}

func (s *InternalSigner) BuildAttestTransaction(
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

	nonce, err := s.Account.Nonce(s.ctx)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, fmt.Errorf("failed to update the account nonce: %w", err)
	}

	defaultResources := makeDefaultResources()

	tip, err := rpc.EstimateTip(s.ctx, s.Account.Provider, constants.FeeEstimationMultiplier)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, fmt.Errorf("failed to estimate tip: %w", err)
	}

	// Taken from starknet.go `utils.BuildInvokeTxn`
	attestTransaction := rpc.BroadcastInvokeTxnV3{
		Type:                  rpc.TransactionTypeInvoke,
		SenderAddress:         s.Account.Address,
		Calldata:              calldata,
		Version:               rpc.TransactionV3,
		Signature:             []*felt.Felt{},
		Nonce:                 nonce,
		ResourceBounds:        &defaultResources,
		Tip:                   tip,
		PayMasterData:         []*felt.Felt{},
		AccountDeploymentData: []*felt.Felt{},
		NonceDataMode:         rpc.DAModeL1,
		FeeMode:               rpc.DAModeL1,
	}

	return attestTransaction, nil
}

func (s *InternalSigner) EstimateFee(txn *rpc.BroadcastInvokeTxnV3) (rpc.FeeEstimation, error) {
	if s.braavos {
		// Braavos require the use of the query bit txn version for fee estimation.
		// The query bit txn version is used for custom validation logic from wallets/accounts
		// when estimating fee/simulating txns
		txn.Version = rpc.TransactionV3WithQueryBit
		_, err := s.SignTransaction(txn)
		if err != nil {
			return rpc.FeeEstimation{}, err
		}
	}

	estimateFee, err := s.Account.Provider.EstimateFee(
		s.ctx,
		[]rpc.BroadcastTxn{txn},
		[]rpc.SimulationFlag{},
		rpc.WithBlockTag(rpc.BlockTagPreConfirmed),
	)
	if s.braavos {
		// Revert the transaction version back.
		// No need to re-sign
		txn.Version = rpc.TransactionV3
	}
	if err != nil {
		return rpc.FeeEstimation{}, err
	}

	return estimateFee[0], nil
}

func (s *InternalSigner) SignTransaction(
	txn *rpc.BroadcastInvokeTxnV3,
) (*rpc.BroadcastInvokeTxnV3, error) {
	return txn, s.Account.SignInvokeTransaction(s.ctx, txn)
}

func (s *InternalSigner) InvokeTransaction(
	txn *rpc.BroadcastInvokeTxnV3,
) (rpc.AddInvokeTransactionResponse, error) {
	return s.Account.Provider.AddInvokeTransaction(s.ctx, txn)
}

func (s *InternalSigner) Call(
	call rpc.FunctionCall, blockID rpc.BlockID,
) ([]*felt.Felt, error) {
	return s.Account.Provider.Call(s.ctx, call, blockID)
}

func (s *InternalSigner) BlockWithTxHashes(blockID rpc.BlockID) (any, error) {
	return s.Account.Provider.BlockWithTxHashes(s.ctx, blockID)
}

func (s *InternalSigner) Address() *types.Address {
	return (*types.Address)(s.Account.Address)
}

func (s *InternalSigner) ValidationContracts() *types.ValidationContracts {
	return &s.validationContracts
}

func (s *InternalSigner) Nonce() (*felt.Felt, error) {
	return s.Account.Nonce(s.ctx)
}
