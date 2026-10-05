package x402

import (
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/botwallet-co/agent-cli/solana"
)

const (
	wrappedSOL = "So11111111111111111111111111111111111111112"
	baseUSDC   = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	payTo      = "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
)

func TestIsPayableOption(t *testing.T) {
	cases := []struct {
		name string
		opt  PaymentOption
		want bool
	}{
		{"USDC on solana", PaymentOption{Scheme: "exact", Network: "solana", Asset: solana.USDCMintMainnet}, true},
		{"USDC on solana-mainnet", PaymentOption{Scheme: "exact", Network: "solana-mainnet", Asset: solana.USDCMintMainnet}, true},
		{"USDC on CAIP-2 mainnet", PaymentOption{Scheme: "exact", Network: "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp", Asset: solana.USDCMintMainnet}, true},
		{"scheme in capitals", PaymentOption{Scheme: "EXACT", Network: "solana", Asset: solana.USDCMintMainnet}, true},
		{"USDC-Dev on solana-devnet", PaymentOption{Scheme: "exact", Network: "solana-devnet", Asset: solana.USDCMintDevnet}, true},
		{"USDC-Dev on CAIP-2 devnet", PaymentOption{Scheme: "exact", Network: "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1", Asset: solana.USDCMintDevnet}, true},

		{"SOL on solana", PaymentOption{Scheme: "exact", Network: "solana", Asset: wrappedSOL}, false},
		{"no asset", PaymentOption{Scheme: "exact", Network: "solana"}, false},
		{"other scheme", PaymentOption{Scheme: "upto", Network: "solana", Asset: solana.USDCMintMainnet}, false},
		{"no scheme", PaymentOption{Network: "solana", Asset: solana.USDCMintMainnet}, false},
		{"mainnet USDC on devnet", PaymentOption{Scheme: "exact", Network: "solana-devnet", Asset: solana.USDCMintMainnet}, false},
		{"devnet USDC on mainnet", PaymentOption{Scheme: "exact", Network: "solana", Asset: solana.USDCMintDevnet}, false},
		{"CAIP-2 testnet", PaymentOption{Scheme: "exact", Network: "solana:4uhcVJyU9pJkvQyS88uRDiswHXSCkY3z", Asset: solana.USDCMintMainnet}, false},
		{"USDC on Base", PaymentOption{Scheme: "exact", Network: "base", Asset: baseUSDC}, false},
	}
	for _, c := range cases {
		if got := IsPayableOption(&c.opt); got != c.want {
			t.Errorf("%s: IsPayableOption = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFindSolanaOptionSkipsOtherAssets(t *testing.T) {
	pr := &PaymentRequired{Accepts: []PaymentOption{
		{Scheme: "exact", Network: "base", Asset: baseUSDC, MaxAmountRequired: "10000", PayTo: "0xabc"},
		{Scheme: "exact", Network: "solana", Asset: wrappedSOL, MaxAmountRequired: "1000000", PayTo: payTo},
		{Scheme: "exact", Network: "solana", Asset: solana.USDCMintMainnet, MaxAmountRequired: "10000", PayTo: payTo},
	}}
	opt := FindSolanaOption(pr)
	if opt == nil || opt.Asset != solana.USDCMintMainnet || opt.GetAmount() != "10000" {
		t.Fatalf("FindSolanaOption = %+v, want the USDC option", opt)
	}

	pr.Accepts = pr.Accepts[:2]
	if opt := FindSolanaOption(pr); opt != nil {
		t.Errorf("FindSolanaOption = %+v, want nil when only SOL is offered on Solana", opt)
	}
}

func TestNormalizeSolanaNetwork(t *testing.T) {
	for in, want := range map[string]string{
		"solana":         "solana",
		"solana-mainnet": "solana",
		"solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp": "solana",
		"solana-devnet": "solana-devnet",
		"Solana-Devnet": "solana-devnet",
		"solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1": "solana-devnet",
		"base": "base",
	} {
		if got := NormalizeSolanaNetwork(in); got != want {
			t.Errorf("NormalizeSolanaNetwork(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFilterSolanaCompatibleNeedsUSDC(t *testing.T) {
	items := []DiscoveredResource{
		{Resource: "https://a.example/usdc", Accepts: []PaymentOption{{Scheme: "exact", Network: "solana", Asset: solana.USDCMintMainnet}}},
		{Resource: "https://b.example/sol", Accepts: []PaymentOption{{Scheme: "exact", Network: "solana", Asset: wrappedSOL}}},
		{Resource: "https://c.example/base", Accepts: []PaymentOption{{Scheme: "exact", Network: "base", Asset: baseUSDC}}},
	}
	got := FilterSolanaCompatible(items)
	if len(got) != 1 || got[0].Resource != "https://a.example/usdc" {
		t.Errorf("FilterSolanaCompatible = %+v", got)
	}
}

// response402 is a 402 response with body and, if header is set, a
// payment-required header.
func response402(body, header string) *http.Response {
	resp := &http.Response{
		StatusCode: http.StatusPaymentRequired,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	if header != "" {
		resp.Header.Set("Payment-Required", header)
	}
	return resp
}

func TestParse402Response(t *testing.T) {
	v1 := `{"x402Version":1,"accepts":[` +
		`{"scheme":"exact","network":"base","maxAmountRequired":"10000","payTo":"0xabc","asset":"` + baseUSDC + `"},` +
		`{"scheme":"exact","network":"solana","maxAmountRequired":"2500","payTo":"` + payTo + `","asset":"` + solana.USDCMintMainnet + `"}]}`
	v2 := `{"x402Version":2,"accepts":[` +
		`{"scheme":"exact","network":"solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp","amount":"1000","payTo":"` + payTo + `","asset":"` + solana.USDCMintMainnet + `"}]}`
	std := base64.StdEncoding.EncodeToString([]byte(v2))
	raw := base64.RawStdEncoding.EncodeToString([]byte(v2))
	if std == raw {
		t.Fatal("test header needs padding to tell the encodings apart")
	}

	cases := []struct {
		name, body, header string
		wantAmount         string
		wantNetwork        string
	}{
		{"v1 body", v1, "", "2500", "solana"},
		{"v1 body wins over a header", v1, std, "2500", "solana"},
		{"v2 header, padded base64", "", std, "1000", "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"},
		{"v2 header, unpadded base64", "", raw, "1000", "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"},
		{"v2 header with an empty JSON body", "{}", std, "1000", "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"},
		{"v2 header with a body that has no options", `{"error":"payment required"}`, std, "1000", "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"},
	}
	for _, c := range cases {
		pr, err := Parse402Response(response402(c.body, c.header))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		opt := FindSolanaOption(pr)
		if opt == nil {
			t.Errorf("%s: no Solana USDC option in %+v", c.name, pr)
			continue
		}
		if opt.GetAmount() != c.wantAmount || opt.Network != c.wantNetwork || opt.PayTo != payTo {
			t.Errorf("%s: option = %+v", c.name, opt)
		}
	}
}

func TestParse402ResponseRejects(t *testing.T) {
	big := `{"accepts":[],"pad":"` + strings.Repeat("x", 1024*1024) + `"}`
	cases := []struct {
		name, body, header, want string
	}{
		{"empty", "", "", "no payment options"},
		{"body without options", `{"error":"nope"}`, "", "no payment options"},
		{"header not base64", "", "%%%", "not valid base64"},
		{"header not JSON", "", base64.StdEncoding.EncodeToString([]byte("not json")), "JSON"},
		{"header without options", "", base64.StdEncoding.EncodeToString([]byte(`{"accepts":[]}`)), "no payment options"},
		{"body over 1 MB", big, "", "exceeds 1 MB"},
	}
	for _, c := range cases {
		_, err := Parse402Response(response402(c.body, c.header))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error = %v, want one containing %q", c.name, err, c.want)
		}
	}

	ok := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}"))}
	if _, err := Parse402Response(ok); err == nil {
		t.Error("a 200 response parsed as a 402")
	}
}
