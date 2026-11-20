package cmd

import (
	"strings"
	"testing"

	solanago "github.com/gagliardetto/solana-go"
)

func TestPayConfirmSignsWithTheAPIKeysWallet(t *testing.T) {
	home := t.TempDir()
	research := addTestWallet(t, home, "research")
	addTestWallet(t, home, "main") // added last, so it is the default wallet

	message, confirm := serverMessage(research, payeeAddress)
	server := &signingAPI{groupKey: research.groupKey, agentPart: research.agentPart, message: message, confirm: confirm}
	api := newFakeAPI(t, server.handle)

	// The key in the environment selects "research"; its Key 1 must sign,
	// not the default wallet's.
	run := runCLI(t, home, api.URL, []string{"BOTWALLET_API_KEY=" + research.apiKey}, "pay", "confirm", "tx-1")
	if run.exitCode != 0 {
		t.Fatalf("exit %d\n%s\n%s", run.exitCode, run.stdout, run.stderr)
	}
	if !server.signatureWasValid() {
		t.Fatal("partial signature was not made with the research wallet's Key 1")
	}
	if got := api.authOf("confirm_payment"); got != "Bearer "+research.apiKey {
		t.Errorf("confirm_payment Authorization = %q", got)
	}
}

func TestSigningRefusesAnotherWalletsGroupKey(t *testing.T) {
	for _, c := range signingCommands {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			other := addTestWallet(t, home, "other")
			mine := addTestWallet(t, home, "mine") // default wallet

			// The server's signing session is for a different wallet's address.
			message, confirm := serverMessage(mine, payeeAddress)
			server := &signingAPI{groupKey: other.groupKey, agentPart: mine.agentPart, message: message, confirm: confirm}
			api := newFakeAPI(t, server.handle)

			run := runCLI(t, home, api.URL, nil, c.args...)
			if run.exitCode == 0 {
				t.Fatal("signed for another wallet's address")
			}
			if code := run.json(t)["error"]; code != "WALLET_MISMATCH" {
				t.Fatalf("error = %v, want WALLET_MISMATCH\n%s", code, run.stdout)
			}
			if api.received(c.complete) {
				t.Errorf("%s was sent although the group key did not match", c.complete)
			}
		})
	}
}

func TestSigningRefusesATransactionOtherThanTheOneShown(t *testing.T) {
	cases := []struct {
		name  string
		payTo solanago.PublicKey
		edit  func(confirm map[string]interface{})
	}{
		{name: "pays someone else", payTo: testAddress("someone else")},
		{name: "pays more", payTo: payeeAddress, edit: func(confirm map[string]interface{}) { confirm["amount_usdc"] = 1.0 }},
		{name: "charges a larger fee", payTo: payeeAddress, edit: func(confirm map[string]interface{}) { confirm["fee_usdc"] = 0.0 }},
		{name: "names no recipient", payTo: payeeAddress, edit: func(confirm map[string]interface{}) { delete(confirm, "to_address") }},
		{name: "on another network", payTo: payeeAddress, edit: func(confirm map[string]interface{}) { confirm["network"] = "testnet" }},
	}
	for _, c := range cases {
		for _, cmd := range signingCommands {
			t.Run(c.name+"/"+cmd.name, func(t *testing.T) {
				home := t.TempDir()
				mine := addTestWallet(t, home, "mine")
				message, confirm := serverMessage(mine, c.payTo)
				if c.edit != nil {
					c.edit(confirm)
				}
				server := &signingAPI{groupKey: mine.groupKey, agentPart: mine.agentPart, message: message, confirm: confirm}
				api := newFakeAPI(t, server.handle)

				run := runCLI(t, home, api.URL, nil, cmd.args...)
				if run.exitCode == 0 {
					t.Fatal("signed a transaction that does not match the confirm response")
				}
				out := run.json(t)
				if out["error"] != "TRANSACTION_MISMATCH" {
					t.Fatalf("error = %v, want TRANSACTION_MISMATCH\n%s", out["error"], run.stdout)
				}
				if msg, _ := out["message"].(string); !strings.Contains(msg, "Nothing was signed") {
					t.Errorf("message = %q", msg)
				}
				// The address is on file, so the check runs before a signing
				// session starts.
				if api.received("frost_sign_init") || api.received(cmd.complete) {
					t.Errorf("signing started anyway: %v", api.called())
				}
			})
		}
	}
}

func TestSigningRefusesWhenTheServerWouldSignAnotherMessage(t *testing.T) {
	for _, cmd := range signingCommands {
		t.Run(cmd.name, func(t *testing.T) {
			home := t.TempDir()
			mine := addTestWallet(t, home, "mine")
			message, confirm := serverMessage(mine, payeeAddress)
			other, _ := serverMessage(mine, testAddress("someone else"))
			server := &signingAPI{groupKey: mine.groupKey, agentPart: mine.agentPart, message: message, confirm: confirm, stored: other}
			api := newFakeAPI(t, server.handle)

			run := runCLI(t, home, api.URL, nil, cmd.args...)
			if code := run.json(t)["error"]; run.exitCode == 0 || code != "TRANSACTION_MISMATCH" {
				t.Fatalf("exit %d, error = %v, want TRANSACTION_MISMATCH\n%s", run.exitCode, code, run.stdout)
			}
			if api.received(cmd.complete) {
				t.Errorf("%s was sent for a message the CLI did not check", cmd.complete)
			}
		})
	}
}

// Servers before Oct 2026 paid a recipient's first USDC account, which is not
// always the associated one. Key 1 signs only a payment into the associated
// account, as the MCP server and the signing page do.
func TestSigningRefusesTheRecipientsOtherUSDCAccount(t *testing.T) {
	home := t.TempDir()
	mine := addTestWallet(t, home, "mine")
	message, confirm := serverMessageTo(mine, payeeAddress, testAddress("payee's older USDC account"))
	server := &signingAPI{groupKey: mine.groupKey, agentPart: mine.agentPart, message: message, confirm: confirm}
	api := newFakeAPI(t, server.handle)

	run := runCLI(t, home, api.URL, nil, "pay", "confirm", "tx-1")
	if code := run.json(t)["error"]; run.exitCode == 0 || code != "TRANSACTION_MISMATCH" {
		t.Fatalf("exit %d, error = %v, want TRANSACTION_MISMATCH\n%s", run.exitCode, code, run.stdout)
	}
	if api.received("frost_sign_init") {
		t.Errorf("signing started anyway: %v", api.called())
	}
}

// Wallets written by older MCP versions have no address in config.json; the
// CLI then checks the transaction against the server's group key.
func TestSigningChecksWalletsWithoutAnAddress(t *testing.T) {
	home := t.TempDir()
	w := addTestWalletEntry(t, home, "mcp", false)

	message, confirm := serverMessage(w, testAddress("someone else"))
	server := &signingAPI{groupKey: w.groupKey, agentPart: w.agentPart, message: message, confirm: confirm}
	api := newFakeAPI(t, server.handle)
	run := runCLI(t, home, api.URL, nil, "pay", "confirm", "tx-1")
	if code := run.json(t)["error"]; run.exitCode == 0 || code != "TRANSACTION_MISMATCH" {
		t.Fatalf("exit %d, error = %v, want TRANSACTION_MISMATCH\n%s", run.exitCode, code, run.stdout)
	}
	if api.received("frost_sign_complete") {
		t.Error("frost_sign_complete was sent for a payment to someone else")
	}

	message, confirm = serverMessage(w, payeeAddress)
	server = &signingAPI{groupKey: w.groupKey, agentPart: w.agentPart, message: message, confirm: confirm}
	api = newFakeAPI(t, server.handle)
	run = runCLI(t, home, api.URL, nil, "pay", "confirm", "tx-1")
	if run.exitCode != 0 {
		t.Fatalf("exit %d\n%s\n%s", run.exitCode, run.stdout, run.stderr)
	}
	if !server.signatureWasValid() {
		t.Fatal("partial signature did not verify")
	}
}

func TestWalletFlagConflictingWithEnvKeyStops(t *testing.T) {
	home := t.TempDir()
	research := addTestWallet(t, home, "research")
	addTestWallet(t, home, "main")
	api := newFakeAPI(t, (&signingAPI{}).handle)

	run := runCLI(t, home, api.URL, []string{"BOTWALLET_API_KEY=" + research.apiKey}, "--wallet", "main", "wallet", "balance")
	if run.exitCode == 0 {
		t.Fatal("balance ran with a conflicting --wallet and API key")
	}
	if code := run.json(t)["error"]; code != "WALLET_CONFLICT" {
		t.Errorf("error = %v, want WALLET_CONFLICT", code)
	}
	if calls := api.called(); len(calls) != 0 {
		t.Errorf("server was called: %v", calls)
	}

	// The same pair is fine when they agree.
	run = runCLI(t, home, api.URL, []string{"BOTWALLET_API_KEY=" + research.apiKey}, "--wallet", "research", "wallet", "balance")
	if run.exitCode != 0 {
		t.Fatalf("exit %d\n%s", run.exitCode, run.stdout)
	}
}

func TestEnvKeyWithoutLocalWalletCannotSign(t *testing.T) {
	home := t.TempDir()
	addTestWallet(t, home, "main")
	api := newFakeAPI(t, (&signingAPI{}).handle)
	env := []string{"BOTWALLET_API_KEY=bw_bot_test_not_on_this_machine"}

	run := runCLI(t, home, api.URL, env, "pay", "confirm", "tx-1")
	if run.exitCode == 0 {
		t.Fatal("pay confirm ran without Key 1 for the API key's wallet")
	}
	if code := run.json(t)["error"]; code != "NO_LOCAL_WALLET" {
		t.Errorf("error = %v, want NO_LOCAL_WALLET", code)
	}
	if calls := api.called(); len(calls) != 0 {
		t.Errorf("server was called before the local check failed: %v", calls)
	}

	// Server-only commands still work with that key.
	run = runCLI(t, home, api.URL, env, "wallet", "balance")
	if run.exitCode != 0 {
		t.Fatalf("balance: exit %d\n%s", run.exitCode, run.stdout)
	}
	if got := api.authOf("balance"); got != "Bearer bw_bot_test_not_on_this_machine" {
		t.Errorf("balance Authorization = %q", got)
	}
}
