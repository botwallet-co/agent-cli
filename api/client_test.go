package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// captureServer answers every call with success and records the last
// request body and headers.
type captureServer struct {
	*httptest.Server
	body    map[string]interface{}
	headers http.Header
	path    string
}

func newCaptureServer(t *testing.T, reply string) *captureServer {
	t.Helper()
	c := &captureServer{}
	c.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.body = nil
		c.headers = r.Header.Clone()
		c.path = r.URL.RequestURI()
		_ = json.NewDecoder(r.Body).Decode(&c.body)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(c.Close)
	return c
}

func TestX402PrepareSendsTheOptionsAsset(t *testing.T) {
	srv := newCaptureServer(t, `{"success":true,"data":{"fetch_id":"f-1"}}`)
	c := New("bw_bot_test", srv.URL)

	if _, err := c.X402Prepare(X402PrepareRequest{URL: "https://api.example/data", PayTo: "payTo", Amount: "10000",
		Network: "solana", Method: "GET", Asset: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", Scheme: "exact"}); err != nil {
		t.Fatal(err)
	}
	got := srv.body
	if got["action"] != "x402_prepare" || got["asset"] != "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" || got["scheme"] != "exact" ||
		got["amount"] != "10000" || got["pay_to"] != "payTo" {
		t.Errorf("request = %v", got)
	}

	// Without an asset or scheme the request is what older CLIs sent.
	if _, err := c.X402Prepare(X402PrepareRequest{URL: "https://api.example/data", PayTo: "payTo", Amount: "10000",
		Network: "solana", Method: "GET"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.body["asset"]; ok {
		t.Errorf("asset sent although empty: %v", srv.body)
	}
	if _, ok := srv.body["scheme"]; ok {
		t.Errorf("scheme sent although empty: %v", srv.body)
	}
}

// Every method sends exactly these JSON bodies: optional values are left
// out when empty or zero, which the server reads as "not given".
func TestRequestBodies(t *testing.T) {
	srv := newCaptureServer(t, `{"success":true,"data":{"export_id":"e","encryption_key":"k"}}`)
	c := New("bw_bot_test", srv.URL)
	item := LineItem{Description: "API calls", Quantity: 2, UnitPriceCents: 500, TotalCents: 1000}

	cases := []struct {
		name string
		call func() error
		want string
	}{
		{"ping", func() error { _, err := c.Ping(); return err }, `{"action":"ping"}`},
		{"dkg_init minimal", func() error { _, err := c.DKGInit("Bot", "", "", nil); return err },
			`{"action":"dkg_init","name":"Bot"}`},
		{"dkg_init full", func() error {
			_, err := c.DKGInit("Bot", "gpt", "o@example.com", map[string]interface{}{"k": "v"})
			return err
		}, `{"action":"dkg_init","name":"Bot","agent_model":"gpt","owner_email":"o@example.com","metadata":{"k":"v"}}`},
		{"dkg_complete", func() error { _, err := c.DKGComplete("s", "a1", "g"); return err },
			`{"action":"dkg_complete","session_id":"s","agent_public_share":"a1","group_public_key":"g"}`},
		{"update_name", func() error { _, err := c.UpdateName("N"); return err }, `{"action":"update_name","name":"N"}`},
		{"update_owner", func() error { _, err := c.UpdateOwner("o@x"); return err }, `{"action":"update_owner","owner_email":"o@x"}`},
		{"info", func() error { _, err := c.Info(); return err }, `{"action":"info"}`},
		{"balance", func() error { _, err := c.Balance(); return err }, `{"action":"balance"}`},
		{"lookup", func() error { _, err := c.Lookup("u"); return err }, `{"action":"lookup","username":"u"}`},
		{"can_i_afford", func() error { _, err := c.CanIAfford("u", 1.5); return err },
			`{"action":"can_i_afford","to":"u","amount":1.5}`},
		{"pay minimal", func() error { _, err := c.Pay("u", 2, "", "", ""); return err }, `{"action":"pay","to":"u","amount":2}`},
		{"pay full", func() error { _, err := c.Pay("u", 2, "n", "r", "idem"); return err },
			`{"action":"pay","to":"u","amount":2,"note":"n","reference":"r"}`},
		{"pay paylink", func() error { _, err := c.PayRequest("pr", ""); return err },
			`{"action":"pay","payment_request_id":"pr"}`},
		{"create_payment_request minimal", func() error {
			_, err := c.CreatePaymentRequest(PaymentRequest{Amount: 5, Description: "d"})
			return err
		}, `{"action":"create_payment_request","amount":5,"description":"d","reveal_owner":false}`},
		{"create_payment_request full", func() error {
			_, err := c.CreatePaymentRequest(PaymentRequest{Amount: 10, Description: "d", Reference: "r", ExpiresIn: "7d",
				RevealOwner: true, LineItems: []LineItem{item}})
			return err
		}, `{"action":"create_payment_request","amount":10,"description":"d","reveal_owner":true,"reference":"r","expires_in":"7d",` +
			`"line_items":[{"description":"API calls","quantity":2,"unit_price_cents":500,"total_cents":1000}]}`},
		{"create_payment_request empty items", func() error {
			_, err := c.CreatePaymentRequest(PaymentRequest{Amount: 5, Description: "d", LineItems: []LineItem{}})
			return err
		}, `{"action":"create_payment_request","amount":5,"description":"d","reveal_owner":false}`},
		{"send_paylink_invitation email", func() error { _, err := c.SendPaylinkInvitation("pr", "a@b", "", ""); return err },
			`{"action":"send_paylink_invitation","request_id":"pr","to_email":"a@b"}`},
		{"send_paylink_invitation wallet", func() error { _, err := c.SendPaylinkInvitation("pr", "", "@bot", "hi"); return err },
			`{"action":"send_paylink_invitation","request_id":"pr","to_wallet":"@bot","message":"hi"}`},
		{"get_payment_request id", func() error { _, err := c.GetPaymentRequest("pr", ""); return err },
			`{"action":"get_payment_request","request_id":"pr"}`},
		{"get_payment_request reference", func() error { _, err := c.GetPaymentRequest("", "ref"); return err },
			`{"action":"get_payment_request","reference":"ref"}`},
		{"list_payment_requests none", func() error { _, err := c.ListPaymentRequests("", 0, 0); return err },
			`{"action":"list_payment_requests"}`},
		{"list_payment_requests all", func() error { _, err := c.ListPaymentRequests("pending", 20, 40); return err },
			`{"action":"list_payment_requests","status":"pending","limit":20,"offset":40}`},
		{"list_payment_requests negative", func() error { _, err := c.ListPaymentRequests("", -1, -5); return err },
			`{"action":"list_payment_requests"}`},
		{"cancel_payment_request", func() error { _, err := c.CancelPaymentRequest("pr"); return err },
			`{"action":"cancel_payment_request","request_id":"pr"}`},
		{"get_deposit_address", func() error { _, err := c.GetDepositAddress(); return err }, `{"action":"get_deposit_address"}`},
		{"request_funds", func() error { _, err := c.RequestFunds(50, "why"); return err },
			`{"action":"request_funds","amount":50,"reason":"why"}`},
		{"list_fund_requests", func() error { _, err := c.ListFundRequests("funded", 10, 0); return err },
			`{"action":"list_fund_requests","status":"funded","limit":10}`},
		{"withdraw", func() error { _, err := c.Withdraw(5, "addr", "why", ""); return err },
			`{"action":"withdraw","amount":5,"to_address":"addr","reason":"why"}`},
		{"confirm_withdrawal", func() error { _, err := c.ConfirmWithdrawal("wd"); return err },
			`{"action":"confirm_withdrawal","withdrawal_id":"wd"}`},
		{"get_withdrawal", func() error { _, err := c.GetWithdrawal("wd"); return err },
			`{"action":"get_withdrawal","withdrawal_id":"wd"}`},
		{"transactions none", func() error { _, err := c.Transactions("", 0, 0); return err }, `{"action":"transactions"}`},
		{"transactions out", func() error { _, err := c.Transactions("out", 20, 5); return err },
			`{"action":"transactions","type":"out","limit":20,"offset":5}`},
		{"my_limits", func() error { _, err := c.MyLimits(); return err }, `{"action":"my_limits"}`},
		{"pending_approvals", func() error { _, err := c.PendingApprovals(); return err }, `{"action":"pending_approvals"}`},
		{"approval_status", func() error { _, err := c.ApprovalStatus("ap"); return err },
			`{"action":"approval_status","approval_id":"ap"}`},
		{"frost_sign_init", func() error { _, err := c.FrostSignInit("tx", "R1"); return err },
			`{"action":"frost_sign_init","transaction_id":"tx","nonce_commitment":"R1"}`},
		{"frost_sign_complete", func() error { _, err := c.FrostSignComplete("s", "z1"); return err },
			`{"action":"frost_sign_complete","session_id":"s","partial_sig":"z1"}`},
		{"events minimal", func() error { _, err := c.Events(nil, 0, false, ""); return err },
			`{"action":"events","unread_only":false}`},
		{"events full", func() error { _, err := c.Events([]string{"a", "b"}, 10, true, "2026-01-01T00:00:00Z"); return err },
			`{"action":"events","unread_only":true,"types":["a","b"],"limit":10,"since":"2026-01-01T00:00:00Z"}`},
		{"mark_read ids", func() error { _, err := c.MarkRead([]string{"e1"}, false); return err },
			`{"action":"mark_read","event_ids":["e1"]}`},
		{"mark_read all", func() error { _, err := c.MarkRead(nil, true); return err }, `{"action":"mark_read","all":true}`},
		{"confirm_payment", func() error { _, err := c.ConfirmPayment("tx"); return err },
			`{"action":"confirm_payment","transaction_id":"tx"}`},
		{"cancel_payment", func() error { _, err := c.CancelPayment("tx"); return err },
			`{"action":"cancel_payment","transaction_id":"tx"}`},
		{"list_payments none", func() error { _, err := c.ListPayments("", "", 0, 0); return err }, `{"action":"list_payments"}`},
		{"list_payments full", func() error { _, err := c.ListPayments("tx", "all", 20, 20); return err },
			`{"action":"list_payments","transaction_id":"tx","status":"all","limit":20,"offset":20}`},
		{"wallet_export", func() error { _, _, err := c.ExportWallet(); return err }, `{"action":"wallet_export"}`},
		{"wallet_import_key", func() error { _, err := c.ImportWalletKey("e"); return err },
			`{"action":"wallet_import_key","export_id":"e"}`},
		{"x402_prepare", func() error {
			_, err := c.X402Prepare(X402PrepareRequest{URL: "https://a.example", PayTo: "p", Amount: "100", Network: "solana", Method: "POST"})
			return err
		}, `{"action":"x402_prepare","url":"https://a.example","pay_to":"p","amount":"100","network":"solana","method":"POST"}`},
		{"x402_prepare empty", func() error { _, err := c.X402Prepare(X402PrepareRequest{}); return err },
			`{"action":"x402_prepare","url":"","pay_to":"","amount":"","network":"","method":""}`},
		{"x402_confirm", func() error { _, err := c.X402Confirm("f"); return err }, `{"action":"x402_confirm","fetch_id":"f"}`},
		{"x402_sign_complete", func() error { _, err := c.X402SignComplete("s", "z1"); return err },
			`{"action":"x402_sign_complete","session_id":"s","partial_sig":"z1"}`},
		{"x402_settle success", func() error { _, err := c.X402Settle("f", true, 200, ""); return err },
			`{"action":"x402_settle","fetch_id":"f","success":true,"response_status":200}`},
		{"x402_settle failure", func() error { _, err := c.X402Settle("f", false, 0, "boom"); return err },
			`{"action":"x402_settle","fetch_id":"f","success":false,"response_status":0,"error_message":"boom"}`},
	}
	for _, tc := range cases {
		if err := tc.call(); err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		var want map[string]interface{}
		if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
			t.Fatalf("%s: bad want: %v", tc.name, err)
		}
		if !reflect.DeepEqual(srv.body, want) {
			got, _ := json.Marshal(srv.body)
			t.Errorf("%s: body = %s\nwant %s", tc.name, got, tc.want)
		}
		if srv.path != "/bot" {
			t.Errorf("%s: path = %s, want /bot", tc.name, srv.path)
		}
	}
}

func TestRequestHeaders(t *testing.T) {
	srv := newCaptureServer(t, `{"success":true,"data":{}}`)
	SetVersion("9.9.9")
	t.Cleanup(func() { SetVersion("dev") })

	if _, err := New("bw_bot_test", srv.URL).Pay("u", 1, "", "", "order-7"); err != nil {
		t.Fatal(err)
	}
	h := srv.headers
	if h.Get("Authorization") != "Bearer bw_bot_test" || h.Get("X-Idempotency-Key") != "order-7" ||
		h.Get("User-Agent") != "Botwallet-CLI/9.9.9" || h.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", h)
	}

	if _, err := New("", srv.URL).Ping(); err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.headers["Authorization"]; ok {
		t.Errorf("Authorization sent without an API key: %v", srv.headers)
	}
	if _, ok := srv.headers["X-Idempotency-Key"]; ok {
		t.Errorf("X-Idempotency-Key sent without a key: %v", srv.headers)
	}
}

func TestNewDefaultsTheBaseURL(t *testing.T) {
	if got := New("", "").BaseURL; got != DefaultBaseURL {
		t.Errorf("New with no URL: BaseURL = %q, want %q", got, DefaultBaseURL)
	}
	if got := New("", "http://localhost:9999/v1").BaseURL; got != "http://localhost:9999/v1" {
		t.Errorf("New with a URL: BaseURL = %q", got)
	}
}

func TestX402Catalog(t *testing.T) {
	srv := newCaptureServer(t, `{"success":true,"data":{"entries":[{"slug":"s","name":"N","url":"https://a.example","price_usdc":0.01}]}}`)
	entries, err := New("", srv.URL).X402Catalog("url content")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Slug != "s" || entries[0].PriceUSDC != 0.01 {
		t.Errorf("entries = %+v", entries)
	}
	if srv.path != "/public?action=x402_catalog&query=url+content" {
		t.Errorf("path = %s", srv.path)
	}
	if srv.headers.Get("User-Agent") != "Botwallet-CLI/x402" || srv.headers.Get("Accept") != "application/json" {
		t.Errorf("headers = %v", srv.headers)
	}

	fail := newCaptureServer(t, `{"success":false,"error":{"code":"X","message":"catalog is down"}}`)
	if _, err := New("", fail.URL).X402Catalog(""); err == nil || err.Error() != "catalog error: catalog is down" {
		t.Errorf("error = %v", err)
	}
	if fail.path != "/public?action=x402_catalog" {
		t.Errorf("path = %s", fail.path)
	}
}
