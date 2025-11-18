package signer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/NethermindEth/juno/core/felt"
	junoUtils "github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet-staking-v2/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
)

var _ Signer = (*ExternalSigner)(nil)

// Used as a wrapper around an exgernal signer implementation
type ExternalSigner struct {
	ctx                 context.Context
	Provider            *rpc.Provider
	operationalAddress  types.Address
	chainId             felt.Felt
	url                 string
	validationContracts types.ValidationContracts
	// If the account used represents a braavos account
	braavos bool
}

func NewExternalSigner(
	ctx context.Context,
	provider *rpc.Provider,
	logger *junoUtils.ZapLogger,
	signer *config.Signer,
	addresses *config.ContractAddresses,
	braavos bool,
) (ExternalSigner, error) {
	chainIdStr, err := provider.ChainID(context.Background())
	if err != nil {
		return ExternalSigner{}, err
	}
	chainId := new(felt.Felt).SetBytes([]byte(chainIdStr))

	validationContracts := types.ValidationContractsFromAddresses(addresses.SetDefaults(chainIdStr))
	logger.Infof("validation contracts: %s", validationContracts.String())

	return ExternalSigner{
		ctx:                 ctx,
		Provider:            provider,
		operationalAddress:  types.AddressFromString(signer.OperationalAddress),
		url:                 signer.ExternalURL,
		chainId:             *chainId,
		validationContracts: validationContracts,
		braavos:             braavos,
	}, nil
}

func (s *ExternalSigner) BuildAttestTransaction(
	blockhash *types.BlockHash,
) (rpc.BroadcastInvokeTxnV3, error) {
	invokeCall := []rpc.InvokeFunctionCall{{
		ContractAddress: s.ValidationContracts().Attest.Felt(),
		FunctionName:    "attest",
		CallData:        []*felt.Felt{blockhash.Felt()},
	}}
	call := utils.InvokeFuncCallsToFunctionCalls(invokeCall)
	calldata := account.FmtCallDataCairo2(call)
	defaultResources := makeDefaultResources()

	nonce, err := s.Provider.Nonce(s.ctx, rpc.WithBlockTag(rpc.BlockTagPreConfirmed), s.Address().Felt())
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, err
	}

	tip, err := rpc.EstimateTip(s.ctx, s.Provider, 1.5)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, fmt.Errorf("failed to estimate tip: %w", err)
	}

	// Taken from starknet.go `utils.BuildInvokeTxn`
	attestTransaction := rpc.BroadcastInvokeTxnV3{
		Type:                  rpc.TransactionTypeInvoke,
		SenderAddress:         s.Address().Felt(),
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

func (s *ExternalSigner) EstimateFee(
	txn *rpc.BroadcastInvokeTxnV3,
) (rpc.FeeEstimation, error) {
	if s.braavos {
		// Braavos require the use of the query bit txn version for fee estimation.
		// The query bit txn version is used for custom validation logic from wallets/accounts
		// when estimating fee/simulating txns
		txn.Version = rpc.TransactionV3WithQueryBit
		_, err := s.SignTransaction(txn)
		if err != nil {
			return rpc.FeeEstimation{}, nil
		}
	}

	estimateFee, err := s.Provider.EstimateFee(
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

func (s *ExternalSigner) SignTransaction(
	txn *rpc.BroadcastInvokeTxnV3,
) (*rpc.BroadcastInvokeTxnV3, error) {
	return txn, SignInvokeTx(txn, &s.chainId, s.url)
}

func (s *ExternalSigner) InvokeTransaction(
	txn *rpc.BroadcastInvokeTxnV3,
) (rpc.AddInvokeTransactionResponse, error) {
	return s.Provider.AddInvokeTransaction(s.ctx, txn)
}

func (s *ExternalSigner) TransactionStatus(transactionHash *felt.Felt) (
	*rpc.TxnStatusResult, error,
) {
	return s.Provider.TransactionStatus(s.ctx, transactionHash)
}

func (s *ExternalSigner) BlockWithTxHashes(blockID rpc.BlockID) (any, error) {
	return s.Provider.BlockWithTxHashes(s.ctx, blockID)
}

func (s *ExternalSigner) Call(
	call rpc.FunctionCall, blockId rpc.BlockID,
) ([]*felt.Felt, error) {
	return s.Provider.Call(s.ctx, call, blockId)
}

func (s *ExternalSigner) Address() *types.Address {
	return &s.operationalAddress
}

func (s *ExternalSigner) ValidationContracts() *types.ValidationContracts {
	return &s.validationContracts
}

func (s *ExternalSigner) Nonce() (*felt.Felt, error) {
	return s.Provider.Nonce(s.ctx, rpc.WithBlockTag(rpc.BlockTagPreConfirmed), s.Address().Felt())
}

func SignInvokeTx(invokeTxnV3 *rpc.BroadcastInvokeTxnV3, chainId *felt.Felt, externalSignerUrl string) error {
	signResp, err := HashAndSignTx(invokeTxnV3, chainId, externalSignerUrl)
	if err != nil {
		return err
	}

	invokeTxnV3.Signature = []*felt.Felt{
		signResp.Signature[0],
		signResp.Signature[1],
	}

	return nil
}

func HashAndSignTx(invokeTxnV3 *rpc.BroadcastInvokeTxnV3, chainId *felt.Felt, externalSignerUrl string) (signer.Response, error) {
	// Create request body
	reqBody := signer.Request{InvokeTxnV3: invokeTxnV3, ChainId: chainId}
	jsonData, err := json.Marshal(&reqBody)
	if err != nil {
		return signer.Response{}, err
	}

	signEndPoint := externalSignerUrl + signer.SIGN_ENDPOINT
	resp, err := http.Post(signEndPoint, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return signer.Response{}, err
	}
	defer func() { _ = resp.Body.Close() }() // Intentionally ignoring the error, will fix in future

	// Read and decode response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return signer.Response{}, err
	}

	// Check if status code indicates an error (non-2xx)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return signer.Response{},
			fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var signResp signer.Response
	return signResp, json.Unmarshal(body, &signResp)
}

func makeDefaultResources() rpc.ResourceBoundsMapping {
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
