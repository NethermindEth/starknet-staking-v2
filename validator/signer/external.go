package signer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils/log"
	"github.com/NethermindEth/starknet-staking-v2/signer"
	"github.com/NethermindEth/starknet-staking-v2/validator/config"
	"github.com/NethermindEth/starknet-staking-v2/validator/types"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
)

var _ Signer = (*ExternalSigner)(nil)

const (
	externalSignerTimeout     = 15 * time.Second
	externalSignerDialTimeout = 5 * time.Second
	maxSignResponseExcerpt    = 200
)

var externalSignerClient = newExternalSignerClient(externalSignerTimeout)

func newExternalSignerClient(timeout time.Duration) *http.Client {
	client := new(http.Client)
	client.Timeout = timeout

	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		dialer := new(net.Dialer)
		dialer.Timeout = externalSignerDialTimeout
		clone := transport.Clone()
		clone.DialContext = dialer.DialContext
		client.Transport = clone
	}

	return client
}

// Used as a wrapper around an exgernal signer implementation
type ExternalSigner struct {
	Provider            rpc.RPCProvider
	logger              log.Logger
	operationalAddress  types.Address
	chainID             felt.Felt
	url                 string
	validationContracts types.ValidationContracts
	// Max tip, in FRI per L2 gas unit, to pay for the attestation transaction
	maxTip uint64
}

func NewExternalSigner(
	ctx context.Context,
	provider rpc.RPCProvider,
	logger log.Logger,
	sig *config.Signer,
	addresses *config.ContractAddresses,
	maxTip uint64,
) (ExternalSigner, error) {
	operationalAddress, err := types.AddressFromString(sig.OperationalAddress)
	if err != nil {
		return ExternalSigner{}, fmt.Errorf("invalid operational address: %w", err)
	}

	chainIDStr, err := provider.ChainID(ctx)
	if err != nil {
		return ExternalSigner{}, err
	}
	chainID := new(felt.Felt).SetBytes([]byte(chainIDStr))

	validationContracts, err := types.ValidationContractsFromAddresses(
		addresses.SetDefaults(chainIDStr),
	)
	if err != nil {
		return ExternalSigner{}, fmt.Errorf("invalid staking contracts: %w", err)
	}

	return ExternalSigner{
		Provider:            provider,
		logger:              logger,
		operationalAddress:  operationalAddress,
		url:                 sig.ExternalURL,
		chainID:             *chainID,
		validationContracts: validationContracts,
		maxTip:              maxTip,
	}, nil
}

func (s *ExternalSigner) BuildAttestTransaction(
	ctx context.Context,
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

	nonce, err := s.Provider.Nonce(
		ctx,
		rpc.WithBlockTag(rpc.BlockTagPreConfirmed),
		s.Address().Felt(),
	)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, err
	}

	tip, err := estimateAttestTip(ctx, s.Provider, s.maxTip, s.logger)
	if err != nil {
		return rpc.BroadcastInvokeTxnV3{}, err
	}

	// Taken from starknet.go `utils.BuildInvokeTxn`
	attestTransaction := rpc.BroadcastInvokeTxnV3{
		Type:                  rpc.TransactionTypeInvoke,
		SenderAddress:         s.Address().Felt(),
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

func (s *ExternalSigner) EstimateFee(
	ctx context.Context,
	txn *rpc.BroadcastInvokeTxnV3,
) (rpc.FeeEstimation, error) {
	return estimateFee(ctx, s, s.Provider, txn)
}

func (s *ExternalSigner) SignTransaction(
	ctx context.Context,
	txn *rpc.BroadcastInvokeTxnV3,
) (*rpc.BroadcastInvokeTxnV3, error) {
	return txn, SignInvokeTx(ctx, txn, &s.chainID, s.url)
}

func (s *ExternalSigner) InvokeTransaction(
	ctx context.Context,
	txn *rpc.BroadcastInvokeTxnV3,
) (rpc.AddInvokeTransactionResponse, error) {
	return s.Provider.AddInvokeTransaction(ctx, txn)
}

func (s *ExternalSigner) TransactionStatus(
	ctx context.Context, transactionHash *felt.Felt,
) (
	*rpc.TxnStatusResult, error,
) {
	return s.Provider.TransactionStatus(ctx, transactionHash)
}

func (s *ExternalSigner) BlockWithTxHashes(
	ctx context.Context, blockID rpc.BlockID,
) (any, error) {
	return s.Provider.BlockWithTxHashes(ctx, blockID)
}

func (s *ExternalSigner) Call(
	ctx context.Context, call rpc.FunctionCall, blockID rpc.BlockID,
) ([]*felt.Felt, error) {
	return s.Provider.Call(ctx, call, blockID)
}

func (s *ExternalSigner) Address() *types.Address {
	return &s.operationalAddress
}

func (s *ExternalSigner) ValidationContracts() *types.ValidationContracts {
	return &s.validationContracts
}

func (s *ExternalSigner) Nonce(ctx context.Context) (*felt.Felt, error) {
	return s.Provider.Nonce(ctx, rpc.WithBlockTag(rpc.BlockTagPreConfirmed), s.Address().Felt())
}

func SignInvokeTx(
	ctx context.Context,
	invokeTxnV3 *rpc.BroadcastInvokeTxnV3,
	chainID *felt.Felt,
	externalSignerURL string,
) error {
	signResp, err := HashAndSignTx(ctx, invokeTxnV3, chainID, externalSignerURL)
	if err != nil {
		return err
	}

	invokeTxnV3.Signature = []*felt.Felt{
		signResp.Signature[0],
		signResp.Signature[1],
	}

	return nil
}

func HashAndSignTx(
	ctx context.Context,
	bTx *rpc.BroadcastInvokeTxnV3,
	chainID *felt.Felt,
	externalSignerURL string,
) (signer.Response, error) {
	// Create request body
	reqBody := signer.Request{BroadcastInvokeTxnV3: bTx, ChainID: chainID}
	jsonData, err := json.Marshal(&reqBody)
	if err != nil {
		return signer.Response{}, err
	}

	signEndpoint := externalSignerURL + signer.SignEndpoint
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, signEndpoint, bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return signer.Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := externalSignerClient.Do(req)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() && ctx.Err() == nil {
			return signer.Response{}, fmt.Errorf(
				"external signer did not answer within %s: %w", externalSignerClient.Timeout, err,
			)
		}

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

	return decodeSignResponse(body)
}

func decodeSignResponse(body []byte) (signer.Response, error) {
	var signResp struct {
		Signature []*felt.Felt `json:"signature"`
	}
	if err := json.Unmarshal(body, &signResp); err != nil {
		return signer.Response{}, fmt.Errorf(
			"invalid external signer response `%s`: %w", signResponseExcerpt(body), err,
		)
	}

	if len(signResp.Signature) != 2 || slices.Contains(signResp.Signature, nil) {
		return signer.Response{}, fmt.Errorf(
			"invalid external signer response `%s`: expected a signature of 2 felts",
			signResponseExcerpt(body),
		)
	}

	return signer.Response{
		Signature: [2]*felt.Felt{signResp.Signature[0], signResp.Signature[1]},
	}, nil
}

func signResponseExcerpt(body []byte) string {
	excerpt := strings.Join(strings.Fields(string(body)), " ")
	if len(excerpt) <= maxSignResponseExcerpt {
		return excerpt
	}

	cut := excerpt[:maxSignResponseExcerpt]
	for range utf8.UTFMax - 1 {
		if r, size := utf8.DecodeLastRuneInString(cut); r != utf8.RuneError || size != 1 {
			break
		}
		cut = cut[:len(cut)-1]
	}

	return cut + "..."
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
