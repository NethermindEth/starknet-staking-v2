package signer

import (
	"context"
	"math/big"

	"github.com/NethermindEth/juno/core/felt"
	junoUtils "github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/curve"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
)

var _ Signer = (*InternalSigner)(nil)

type InternalSigner struct {
	Account account.Account
	// If the account used represents a braavos account
	braavos             bool
	validationContracts ValidationContracts
}

func NewInternalSigner(
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

	publicKey, _, err := curve.Curve.PrivateToPoint(privateKey)
	if err != nil {
		return InternalSigner{}, errors.New("cannot derive public key from private key")
	}

	publicKeyStr := publicKey.String()
	ks := account.SetNewMemKeystore(publicKeyStr, privateKey)

	accountAddr := types.AddressFromString(signer.OperationalAddress)
	account, err := account.NewAccount(provider, accountAddr.Felt(), publicKeyStr, ks, 2)
	if err != nil {
		return InternalSigner{}, errors.Errorf("cannot create internal signer: %s", err)
	}

	chainIdStr, err := provider.ChainID(context.Background())
	if err != nil {
		return InternalSigner{}, err
	}
	validationContracts := types.ValidationContractsFromAddresses(
		addresses.SetDefaults(chainIdStr),
	)
	logger.Infof("Validation contracts: %s", validationContracts.String())

	logger.Debugw("internal signer has been set up", "address", accountAddr.String())
	return InternalSigner{
		Account:             *account,
		braavos:             braavos,
		validationContracts: validationContracts,
	}, nil
}

func (s *InternalSigner) GetTransactionStatus(
	ctx context.Context, transactionHash *felt.Felt,
) (*rpc.TxnStatusResult, error) {
	return s.Account.Provider.GetTransactionStatus(ctx, transactionHash)
}

func (s *InternalSigner) BuildAndSendInvokeTxn(
	ctx context.Context,
	functionCalls []rpc.InvokeFunctionCall,
	multiplier float64,
) (*rpc.AddInvokeTransactionResponse, error) {
	return s.Account.BuildAndSendInvokeTxn(ctx, functionCalls, multiplier, s.braavos)
}

func (s *InternalSigner) Call(
	ctx context.Context, call rpc.FunctionCall, blockId rpc.BlockID,
) ([]*felt.Felt, error) {
	return s.Account.Provider.Call(ctx, call, blockId)
}

func (s *InternalSigner) BlockWithTxHashes(
	ctx context.Context, blockID rpc.BlockID,
) (any, error) {
	return s.Account.Provider.BlockWithTxHashes(ctx, blockID)
}

func (s *InternalSigner) Address() *Address {
	return (*Address)(s.Account.Address)
}

func (s *InternalSigner) ValidationContracts() *ValidationContracts {
	return &s.validationContracts
}
