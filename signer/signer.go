package signer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/curve"
	"github.com/NethermindEth/starknet.go/hash"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
)

const SIGN_ENDPOINT = "/sign"

type Request struct {
	*rpc.InvokeTxnV3 `json:"transaction"`
	ChainId          *felt.Felt `json:"chain_id"`
}

type Response struct {
	Signature [2]*felt.Felt `json:"signature"`
}

func (r *Response) String() string {
	return fmt.Sprintf(
		`{r: %s, s: %s}`,
		r.Signature[0],
		r.Signature[1],
	)
}

type Signer struct {
	logger    *utils.ZapLogger
	keyStore  *account.MemKeystore
	publicKey string
}

func New(privateKey string, logger *utils.ZapLogger) (Signer, error) {
	privKey, ok := new(big.Int).SetString(privateKey, 0)
	if !ok {
		return Signer{}, errors.Errorf("Cannot turn private key %s into a big int", privateKey)
	}

	publicKey, _, err := curve.Curve.PrivateToPoint(privKey)
	if err != nil {
		return Signer{}, errors.New("Cannot derive public key from private key")
	}

	publicKeyStr := publicKey.String()
	ks := account.SetNewMemKeystore(publicKeyStr, privKey)

	return Signer{
		logger:    logger,
		keyStore:  ks,
		publicKey: publicKeyStr,
	}, nil
}

// Listen for requests of the type `POST` at `<address>/sign`. The request
// should include the hash of the transaction being signed.
func (s *Signer) Listen(address string) error {
	http.HandleFunc(SIGN_ENDPOINT, s.handler)

	s.logger.Infof("Server running at %s", address)

	return http.ListenAndServe(address, nil)
}

// Decodes the request and returns ECDSA `r` and `s` signature values via http
func (s *Signer) handler(w http.ResponseWriter, r *http.Request) {
	s.logger.Debug("Receiving http request")

	defer func() { _ = r.Body.Close() }()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Failed to decode request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	signature, err := s.hashAndSign(req.InvokeTxnV3, req.ChainId)
	if err != nil {
		http.Error(w, "Failed to sign tx: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := Response{Signature: signature}
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(resp)
	if err != nil {
		s.logger.Errorf("Error encoding response %s: %s", resp, err)
		return
	}

	s.logger.Debugw("Answered http request", "response", resp)
}

// Given a transaction hash returns the ECDSA `r` and `s` signature values
func (s *Signer) hashAndSign(invokeTxnV3 *rpc.InvokeTxnV3, chainId *felt.Felt) ([2]*felt.Felt, error) {
	s.logger.Infow("Signing transaction", "transaction", invokeTxnV3, "chainId", chainId)

	hash, err := hash.TransactionHashInvokeV3(invokeTxnV3, chainId)
	if err != nil {
		return [2]*felt.Felt{}, err
	}

	hashBig := hash.BigInt(new(big.Int))

	s1, s2, err := s.keyStore.Sign(context.Background(), s.publicKey, hashBig)
	if err != nil {
		return [2]*felt.Felt{}, err
	}

	s.logger.Debugw("Signature", "r", s1, "s", s2)

	return [2]*felt.Felt{
		new(felt.Felt).SetBigInt(s1),
		new(felt.Felt).SetBigInt(s2),
	}, nil
}
