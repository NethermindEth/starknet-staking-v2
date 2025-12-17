package signer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/curve"
	"github.com/NethermindEth/starknet.go/hash"
	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/cockroachdb/errors"
)

const SignEndpoint = "/sign"

type Request struct {
	*rpc.InvokeTxnV3 `json:"transaction"`
	ChainID          *felt.Felt `json:"chain_id"`
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
		return Signer{}, errors.Errorf("cannot turn private key %s into a big int", privateKey)
	}

	publicKey, _ := curve.PrivateKeyToPoint(privKey)
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
	mux := http.NewServeMux()
	mux.HandleFunc(SignEndpoint, s.handler)

	//nolint:exhaustruct // Only specifying used fields
	server := &http.Server{
		Addr:         address,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
		Handler:      mux,
	}

	s.logger.Infof("server running at %s", address)

	return server.ListenAndServe()
}

// Decodes the request and returns ECDSA `r` and `s` signature values via http
func (s *Signer) handler(w http.ResponseWriter, r *http.Request) {
	s.logger.Debugw("receiving http request", "request", r)

	defer func() { _ = r.Body.Close() }()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body: "+err.Error(), http.StatusBadRequest)

		return
	}

	var req Request
	err = json.Unmarshal(body, &req)
	if err != nil {
		http.Error(w, "failed to decode request body: "+err.Error(), http.StatusBadRequest)

		return
	}

	signature, err := s.hashAndSign(req.InvokeTxnV3, req.ChainID)
	if err != nil {
		http.Error(w, "failed to sign tx: "+err.Error(), http.StatusInternalServerError)

		return
	}

	resp := Response{Signature: signature}
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(resp)
	if err != nil {
		s.logger.Errorf("encoding response %s: %s", resp, err)

		return
	}

	s.logger.Debugw("answered http request", "response", resp)
}

// Given a transaction hash returns the ECDSA `r` and `s` signature values
func (s *Signer) hashAndSign(
	invokeTxnV3 *rpc.InvokeTxnV3,
	chainID *felt.Felt,
) ([2]*felt.Felt, error) {
	s.logger.Infow("Signing transaction", "transaction", invokeTxnV3, "chainId", chainID)

	txnHash, err := hash.TransactionHashInvokeV3(invokeTxnV3, chainID)
	if err != nil {
		return [2]*felt.Felt{}, err
	}

	hashBig := txnHash.BigInt(new(big.Int))

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
