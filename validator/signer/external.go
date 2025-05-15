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
	*rpc.Provider
	operationalAddress  Address
	chainId             felt.Felt
	url                 string
	validationContracts ValidationContracts
	// If the account used represents a braavos account
	braavos bool
}

func NewExternalSigner(
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
		Provider:            provider,
		operationalAddress:  types.AddressFromString(signer.OperationalAddress),
		url:                 signer.ExternalURL,
		chainId:             *chainId,
		validationContracts: validationContracts,
		braavos:             braavos,
	}, nil
}

func (s *ExternalSigner) BuildAndSendInvokeTxn(
	ctx context.Context,
	functionCalls []rpc.InvokeFunctionCall,
	multiplier float64,
) (*rpc.AddInvokeTransactionResponse, error) {
	nonce, err := s.Nonce(ctx, rpc.WithBlockTag("pending"), s.Address().Felt())
	if err != nil {
		return nil, err
	}

	fnCallData := utils.InvokeFuncCallsToFunctionCalls(functionCalls)
	formattedCallData := account.FmtCallDataCairo2(fnCallData)

	// Building and signing the txn, as it needs a signature to estimate the fee
	broadcastInvokeTxnV3 := utils.BuildInvokeTxn(
		s.Address().Felt(),
		nonce,
		formattedCallData,
		makeResourceBoundsMapWithZeroValues(),
	)

	if s.braavos {
		// Braavos require the use of the query bit txn version for fee estimation.
		// The query bit txn version is used for custom validation logic from wallets/accounts when estimating fee/simulating txns
		broadcastInvokeTxnV3.Version = rpc.TransactionV3WithQueryBit
	}

	if err := SignInvokeTx(broadcastInvokeTxnV3, &s.chainId, s.url); err != nil {
		return nil, err
	}

	// Estimate txn fee
	estimateFee, err := s.EstimateFee(
		ctx,
		[]rpc.BroadcastTxn{broadcastInvokeTxnV3},
		[]rpc.SimulationFlag{},
		rpc.WithBlockTag("pending"),
	)
	if err != nil {
		return nil, err
	}
	txnFee := estimateFee[0]
	broadcastInvokeTxnV3.ResourceBounds = utils.FeeEstToResBoundsMap(txnFee, multiplier)

	// assuring the signed txn version will be rpc.TransactionV3, since queryBit txn version is only used for estimation/simulation
	broadcastInvokeTxnV3.Version = rpc.TransactionV3

	// Signing the txn again with the estimated fee,
	// as the fee value is used in the txn hash calculation
	if err := SignInvokeTx(broadcastInvokeTxnV3, &s.chainId, s.url); err != nil {
		return nil, err
	}

	return s.AddInvokeTransaction(ctx, broadcastInvokeTxnV3)
}

func (s *ExternalSigner) Address() *Address {
	return &s.operationalAddress
}

func (s *ExternalSigner) ValidationContracts() *ValidationContracts {
	return &s.validationContracts
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

func makeResourceBoundsMapWithZeroValues() *rpc.ResourceBoundsMapping {
	return &rpc.ResourceBoundsMapping{
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
