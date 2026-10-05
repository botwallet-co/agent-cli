package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestX402PrepareSendsTheOptionsAsset(t *testing.T) {
	var got map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"success":true,"data":{"fetch_id":"f-1"}}`))
	}))
	defer srv.Close()
	c := NewClientWithURL("bw_bot_test", srv.URL)

	if _, err := c.X402Prepare("https://api.example/data", "payTo", "10000", "solana", "GET",
		"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", "exact"); err != nil {
		t.Fatal(err)
	}
	if got["action"] != "x402_prepare" || got["asset"] != "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" || got["scheme"] != "exact" ||
		got["amount"] != "10000" || got["pay_to"] != "payTo" {
		t.Errorf("request = %v", got)
	}

	// Without an asset or scheme the request is what older CLIs sent.
	if _, err := c.X402Prepare("https://api.example/data", "payTo", "10000", "solana", "GET", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["asset"]; ok {
		t.Errorf("asset sent although empty: %v", got)
	}
	if _, ok := got["scheme"]; ok {
		t.Errorf("scheme sent although empty: %v", got)
	}
}
