package signer

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func SetExternalSignerTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()

	client := externalSignerClient
	externalSignerClient = newExternalSignerClient(timeout)
	t.Cleanup(func() { externalSignerClient = client })
}

func NewHungSignerServer(t *testing.T) string {
	t.Helper()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	return server.URL
}
