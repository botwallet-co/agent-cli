package cmd

import (
	"strings"
	"testing"

	solanago "github.com/gagliardetto/solana-go"

	"github.com/botwallet-co/agent-cli/solana"
	"github.com/botwallet-co/agent-cli/solana/txcheck/txchecktest"
)

func TestIsValidSolanaAddress(t *testing.T) {
	wallet := testAddress("wallet")
	usdcAccount := txchecktest.ATA(wallet, solanago.MustPublicKeyFromBase58(solana.USDCMintMainnet))
	if solanago.IsOnCurve(usdcAccount[:]) {
		t.Fatal("test setup: the associated token account should be off the curve")
	}

	cases := []struct {
		addr string
		want bool
	}{
		{wallet.String(), true},
		{usdcAccount.String(), true}, // off-curve addresses (vaults, escrows) stay allowed
		{"11111111111111111111111111111111", true},
		{strings.Repeat("z", 44), false}, // base58, but 33 bytes
		{strings.Repeat("2", 32), false}, // base58, but fewer than 32 bytes
		{wallet.String()[:31], false},
		{wallet.String()[:30] + "0OIl", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isValidSolanaAddress(c.addr); got != c.want {
			t.Errorf("isValidSolanaAddress(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}

func TestBadAddressesStopBeforeTheServer(t *testing.T) {
	home := t.TempDir()
	addTestWallet(t, home, "main")
	bad := strings.Repeat("z", 44)

	for name, args := range map[string][]string{
		"withdraw": {"withdraw", "5.00", bad, "--reason", "test"},
		"pay":      {"pay", bad, "5.00"},
	} {
		t.Run(name, func(t *testing.T) {
			api := newFakeAPI(t, func(string, map[string]interface{}) (int, interface{}) {
				return apiOK(map[string]interface{}{})
			})
			run := runCLI(t, home, api.URL, nil, args...)
			if code := run.json(t)["error"]; run.exitCode == 0 || code != "VALIDATION_ERROR" {
				t.Fatalf("exit %d, error = %v, want VALIDATION_ERROR\n%s", run.exitCode, code, run.stdout)
			}
			if calls := api.called(); len(calls) != 0 {
				t.Errorf("server was called: %v", calls)
			}
		})
	}

	// Usernames still go to the server.
	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		return apiOK(map[string]interface{}{"transaction_id": "tx-1", "status": "pre_approved"})
	})
	runCLI(t, home, api.URL, nil, "pay", "@some-agent", "5.00")
	if !api.received("pay") {
		t.Errorf("pay to a username did not reach the server: %v", api.called())
	}
}
