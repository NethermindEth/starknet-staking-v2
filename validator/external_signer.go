package validator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/NethermindEth/juno/core/felt"
)

type SignRequest struct {
	Hash string `json:"transaction_hash"`
}

type SignResponse struct {
	Signature []*felt.Felt `json:"signature"`
}

func SignTxHash(hash *felt.Felt, externalSignerUrl string) (*SignResponse, error) {
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

	// Check if status code indicates an error (non-2xx)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Server error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var signResp SignResponse
	err = json.Unmarshal(body, &signResp)
	if err != nil {
		return nil, err
	}

	return &signResp, nil
}
