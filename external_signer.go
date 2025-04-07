package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/NethermindEth/juno/core/felt"
)

type SignRequest struct {
	Hash string `json:"hash"`
}

type SignResponse struct {
	Signature []*felt.Felt `json:"signature"`
}

func signTxHash(hash *felt.Felt, externalSignerUrl string) (*SignResponse, error) {
	// Create request body
	reqBody := SignRequest{Hash: hash.String()}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	// Make POST request
	resp, err := http.Post(externalSignerUrl+"/sign_hash", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read and decode response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var signResp SignResponse
	err = json.Unmarshal(body, &signResp)
	if err != nil {
		return nil, err
	}

	return &signResp, nil
}
