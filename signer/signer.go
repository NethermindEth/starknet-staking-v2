package signer

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/curve"
	"github.com/cockroachdb/errors"
)

type SignRequest struct {
	Hash felt.Felt `json:"transaction_hash"`
}

type SignResponse struct {
	Signature []*felt.Felt `json:"signature"`
}

type Signer struct {
	logger    *utils.ZapLogger
	publicKey *big.Int
	keyStore  *account.MemKeystore
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

	ks := account.SetNewMemKeystore(publicKey.String(), privKey)

	return Signer{
		logger:    logger,
		keyStore:  ks,
		publicKey: publicKey,
	}, nil
}

// Listen for requests of the type `POST` at `<address>/sign`. The request
// should include the hash of the transaction being signed.
func (s *Signer) Listen(address string) error {
	const sign_endpoint = "/sign"
	http.HandleFunc(sign_endpoint, s.handler)

	s.logger.Infof("Server running at %s", address)

	return http.ListenAndServe(address, nil)
}

// Decodes the request and signs it by returning the `r` and `v` values
func (s *Signer) handler(w http.ResponseWriter, r *http.Request) {
	s.logger.Debug("Recieving http request")

	var req SignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	signature, err := s.sign(&req.Hash)
	if err != nil {
		http.Error(w, "Failed to sign hash: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := SignResponse{Signature: signature}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	s.logger.Debugw("Answered http request", "response", resp)
}

func (s *Signer) sign(msg *felt.Felt) ([]*felt.Felt, error) {
	s.logger.Infof("Signing message with hash: %s", msg)

	msgBig := msg.BigInt(new(big.Int))

	r, v, err := s.keyStore.Sign(context.Background(), s.publicKey.String(), msgBig)
	if err != nil {
		return nil, err
	}

	s1Felt := new(felt.Felt).SetBigInt(r)
	s2Felt := new(felt.Felt).SetBigInt(v)

	s.logger.Debugf("r", r, "v", v)

	return []*felt.Felt{s1Felt, s2Felt}, nil
}
