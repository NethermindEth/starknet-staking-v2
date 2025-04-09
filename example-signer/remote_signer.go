package main

import (
	"context"
	"encoding/json"
	"log"
	"math/big"
	"net/http"
	"os"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/account"
	"github.com/NethermindEth/starknet.go/curve"
	"github.com/cockroachdb/errors"
	"github.com/joho/godotenv"
)

type SignRequest struct {
	Hash felt.Felt `json:"transaction_hash"`
}

type SignResponse struct {
	Signature []*felt.Felt `json:"signature"`
}

type Signer struct {
	publicKey *big.Int
	keyStore  *account.MemKeystore
}

func loadEnv() string {
	err := godotenv.Load(".env")
	if err != nil {
		log.Printf("No '.env' file found %s, will try looking for PRIVATE_KEY as a cli environment variable", err)
	}

	signerKey := os.Getenv("PRIVATE_KEY")
	if signerKey == "" {
		panic("Failed to load PRIVATE_KEY, empty string")
	}

	return signerKey
}

func newSigner(privateKey string) (Signer, error) {
	privKey, ok := new(big.Int).SetString(privateKey, 0)
	if !ok {
		return Signer{}, errors.Errorf("Cannot turn private key %s into a big int", privateKey)
	}

	publicKey, _, err := curve.Curve.PrivateToPoint(privKey)
	if err != nil {
		return Signer{}, errors.New("Cannot derive public key from private key")
	}

	ks := account.SetNewMemKeystore(publicKey.String(), privKey)

	return Signer{keyStore: ks, publicKey: publicKey}, nil
}

func (s *Signer) sign(msg *felt.Felt) ([]*felt.Felt, error) {
	msgBig := msg.BigInt(new(big.Int))

	s1, s2, err := s.keyStore.Sign(context.Background(), s.publicKey.String(), msgBig)
	if err != nil {
		return nil, err
	}

	s1Felt := new(felt.Felt).SetBigInt(s1)
	s2Felt := new(felt.Felt).SetBigInt(s2)

	return []*felt.Felt{s1Felt, s2Felt}, nil
}

func signHandler(w http.ResponseWriter, r *http.Request, signer *Signer) {
	var req SignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	signature, err := signer.sign(&req.Hash)
	if err != nil {
		http.Error(w, "Failed to sign hash: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := SignResponse{Signature: signature}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func main() {
	signerKey := loadEnv()
	signer, err := newSigner(signerKey)
	if err != nil {
		panic(err)
	}

	http.HandleFunc("/sign_hash", func(w http.ResponseWriter, r *http.Request) {
		signHandler(w, r, &signer)
	})

	log.Println("🚀 Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
