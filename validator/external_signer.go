package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet-staking-v2/signer"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
)

// Used as a wrapper around an exgernal signer implementation
type ExternalSigner struct {
	*rpc.Provider
	OperationalAddress Address
	ChainId            felt.Felt
	Url                string
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
		Url:                signer.ExternalUrl,
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
	if err := SignInvokeTx(&broadcastInvokeTxnV3.InvokeTxnV3, &s.ChainId, s.Url); err != nil {
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

	// Signing the txn again with the estimated fee,
	// as the fee value is used in the txn hash calculation
	if err := SignInvokeTx(&broadcastInvokeTxnV3.InvokeTxnV3, &s.ChainId, s.Url); err != nil {
		return nil, err
	}

	return s.AddInvokeTransaction(ctx, broadcastInvokeTxnV3)
}

func (s *ExternalSigner) Address() *felt.Felt {
	return s.OperationalAddress.Felt()
}

func SignInvokeTx(invokeTxnV3 *rpc.InvokeTxnV3, chainId *felt.Felt, externalSignerUrl string) error {
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

func HashAndSignTx(invokeTxnV3 *rpc.InvokeTxnV3, chainId *felt.Felt, externalSignerUrl string) (signer.Response, error) {
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
