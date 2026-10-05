package cmd

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"testing"

	"filippo.io/edwards25519"
	solanago "github.com/gagliardetto/solana-go"

	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/solana"
	"github.com/botwallet-co/agent-cli/solana/frost"
	"github.com/botwallet-co/agent-cli/solana/txcheck/txchecktest"
)

// testWallet is a local wallet set up the way wallet create leaves it.
type testWallet struct {
	name      string
	apiKey    string
	address   string // base58 group key = deposit address
	groupKey  []byte // the same address, raw 32 bytes
	agentPart *edwards25519.Point
}

// addTestWallet creates a wallet in home: a real Key 1, a made-up server
// share, and the config entry with the resulting deposit address.
func addTestWallet(t *testing.T, home, name string) testWallet {
	t.Helper()
	return addTestWalletEntry(t, home, name, true)
}

// addTestWalletEntry is addTestWallet; with storeAddress false the config
// entry has no address, like entries written by older MCP versions.
func addTestWalletEntry(t *testing.T, home, name string, storeAddress bool) testWallet {
	t.Helper()
	mnemonic, err := frost.GenerateShareMnemonic()
	if err != nil {
		t.Fatal(err)
	}
	agent, err := frost.KeyShareFromMnemonic(mnemonic)
	if err != nil {
		t.Fatal(err)
	}
	server, err := frost.GenerateKeyShare()
	if err != nil {
		t.Fatal(err)
	}
	groupKey := frost.EncodePoint(frost.ComputeGroupKey(agent.Public, server.Public))
	w := testWallet{
		name:      name,
		apiKey:    "bw_bot_test_" + name,
		address:   solanaBase58Encode(groupKey),
		groupKey:  groupKey,
		agentPart: agent.Public,
	}

	stored := w.address
	if !storeAddress {
		stored = ""
	}
	t.Setenv(config.ConfigDirEnv, home)
	if _, err := config.AddWalletWithInfo(name, name+"-user", name, w.apiKey, stored, mnemonic); err != nil {
		t.Fatal(err)
	}
	return w
}

// testAddress is a made-up Solana address.
func testAddress(name string) solanago.PublicKey {
	h := sha256.Sum256([]byte(name))
	return solanago.PublicKeyFromBytes(h[:])
}

// payeeAddress is the recipient the fake confirm responses show.
var payeeAddress = testAddress("payee")

// usdcMint is the mainnet USDC mint.
var usdcMint = solanago.MustPublicKeyFromBase58(solana.USDCMintMainnet)

// serverMessage builds the message the server's confirm returns for a
// 10 USDC payment with a 0.25 USDC fee from w to payTo, and the confirm
// fields that describe it (always to payeeAddress).
func serverMessage(w testWallet, payTo solanago.PublicKey) ([]byte, map[string]interface{}) {
	return serverMessageTo(w, payTo, txchecktest.ATA(payTo, usdcMint))
}

// serverMessageTo is serverMessage with the payment going to token account
// payToAccount.
func serverMessageTo(w testWallet, payTo, payToAccount solanago.PublicKey) ([]byte, map[string]interface{}) {
	mint := usdcMint
	wallet := solanago.MustPublicKeyFromBase58(w.address)
	feeCollection := testAddress("fee collection")
	msg := txchecktest.BuildPayment(txchecktest.Payment{
		FeePayer:               testAddress("fee payer"),
		Wallet:                 wallet,
		Source:                 txchecktest.ATA(wallet, mint),
		Recipient:              payTo,
		RecipientAccount:       payToAccount,
		RecipientAccountExists: true,
		FeeCollection:          feeCollection,
		FeeAccount:             txchecktest.ATA(feeCollection, mint),
		FeeAccountExists:       true,
		Mint:                   mint,
		Amount:                 10_000_000,
		Fee:                    250_000,
	})
	return msg.Bytes(), map[string]interface{}{
		"to_address":  payeeAddress.String(),
		"pay_to":      payeeAddress.String(),
		"amount_usdc": 10.0,
		"fee_usdc":    0.25,
		"total_usdc":  10.25,
		"network":     solana.ClusterMainnet,
	}
}

// signingAPI plays the server side of a confirm + FROST signing flow. It
// answers frost_sign_init with groupKey and checks the agent's partial
// signature against agentPart (the public share of the expected Key 1).
type signingAPI struct {
	t         *testing.T
	groupKey  []byte
	agentPart *edwards25519.Point
	message   []byte
	confirm   map[string]interface{} // extra confirm response fields
	stored    []byte                 // message_to_sign from frost_sign_init; message when nil

	mu          sync.Mutex
	agentNonce  *edwards25519.Point
	serverNonce *frost.SigningNonce
	validSig    bool
}

func (s *signingAPI) handle(action string, req map[string]interface{}) (int, interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch action {
	case "confirm_payment", "confirm_withdrawal", "x402_confirm":
		resp := map[string]interface{}{
			"message":        base64.StdEncoding.EncodeToString(s.message),
			"transaction_id": "tx-1",
			"url":            "https://example.invalid/paid",
		}
		for k, v := range s.confirm {
			resp[k] = v
		}
		return apiOK(resp)
	case "frost_sign_init":
		raw, _ := base64.StdEncoding.DecodeString(req["nonce_commitment"].(string))
		agentNonce, err := frost.DecodePoint(raw)
		if err != nil {
			return apiFail(http.StatusBadRequest, "VALIDATION_ERROR", "bad nonce commitment")
		}
		serverNonce, _ := frost.GenerateNonce()
		s.agentNonce, s.serverNonce = agentNonce, serverNonce
		stored := s.stored
		if stored == nil {
			stored = s.message
		}
		return apiOK(map[string]interface{}{
			"session_id":              "session-1",
			"server_nonce_commitment": base64.StdEncoding.EncodeToString(frost.EncodePoint(serverNonce.Commitment)),
			"group_key":               base64.StdEncoding.EncodeToString(s.groupKey),
			"message_to_sign":         base64.StdEncoding.EncodeToString(stored),
		})
	case "frost_sign_complete", "x402_sign_complete":
		raw, _ := base64.StdEncoding.DecodeString(req["partial_sig"].(string))
		z1, err := frost.DecodeScalar(raw)
		if err != nil {
			return apiFail(http.StatusBadRequest, "VALIDATION_ERROR", "bad partial signature")
		}
		// k = SHA-512(R || A || M) mod l, with R = R1 + R2 (standard Ed25519 challenge)
		groupNonce := new(edwards25519.Point).Add(s.agentNonce, s.serverNonce.Commitment)
		h := sha512.New()
		h.Write(groupNonce.Bytes())
		h.Write(s.groupKey)
		h.Write(s.message)
		k, _ := new(edwards25519.Scalar).SetUniformBytes(h.Sum(nil))
		s.validSig = frost.VerifyPartialSig(z1, s.agentNonce, s.agentPart, k)
		if !s.validSig {
			return apiFail(http.StatusBadRequest, "INVALID_PARTIAL_SIG", "partial signature does not verify")
		}
		return apiOK(map[string]interface{}{"status": "completed", "signed_transaction": "dGVzdA=="})
	case "balance":
		return apiOK(map[string]interface{}{"balance": 0})
	}
	return apiFail(http.StatusBadRequest, "VALIDATION_ERROR", "unexpected action "+action)
}

func (s *signingAPI) signatureWasValid() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validSig
}

func TestPayConfirmSignsWithTheAPIKeysWallet(t *testing.T) {
	home := t.TempDir()
	research := addTestWallet(t, home, "research")
	addTestWallet(t, home, "main") // added last, so it is the default wallet

	message, confirm := serverMessage(research, payeeAddress)
	server := &signingAPI{t: t, groupKey: research.groupKey, agentPart: research.agentPart, message: message, confirm: confirm}
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

// signingCommands are the commands that co-sign with Key 1, and the call
// that would send the partial signature.
var signingCommands = []struct {
	name     string
	args     []string
	complete string
}{
	{"pay", []string{"pay", "confirm", "tx-1"}, "frost_sign_complete"},
	{"withdraw", []string{"withdraw", "confirm", "wd-1"}, "frost_sign_complete"},
	{"x402", []string{"x402", "fetch", "confirm", "fetch-1"}, "x402_sign_complete"},
}

func TestSigningRefusesAnotherWalletsGroupKey(t *testing.T) {
	for _, c := range signingCommands {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			other := addTestWallet(t, home, "other")
			mine := addTestWallet(t, home, "mine") // default wallet

			// The server's signing session is for a different wallet's address.
			message, confirm := serverMessage(mine, payeeAddress)
			server := &signingAPI{t: t, groupKey: other.groupKey, agentPart: mine.agentPart, message: message, confirm: confirm}
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
				server := &signingAPI{t: t, groupKey: mine.groupKey, agentPart: mine.agentPart, message: message, confirm: confirm}
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
			server := &signingAPI{t: t, groupKey: mine.groupKey, agentPart: mine.agentPart, message: message, confirm: confirm, stored: other}
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
	server := &signingAPI{t: t, groupKey: mine.groupKey, agentPart: mine.agentPart, message: message, confirm: confirm}
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
	server := &signingAPI{t: t, groupKey: w.groupKey, agentPart: w.agentPart, message: message, confirm: confirm}
	api := newFakeAPI(t, server.handle)
	run := runCLI(t, home, api.URL, nil, "pay", "confirm", "tx-1")
	if code := run.json(t)["error"]; run.exitCode == 0 || code != "TRANSACTION_MISMATCH" {
		t.Fatalf("exit %d, error = %v, want TRANSACTION_MISMATCH\n%s", run.exitCode, code, run.stdout)
	}
	if api.received("frost_sign_complete") {
		t.Error("frost_sign_complete was sent for a payment to someone else")
	}

	message, confirm = serverMessage(w, payeeAddress)
	server = &signingAPI{t: t, groupKey: w.groupKey, agentPart: w.agentPart, message: message, confirm: confirm}
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
	api := newFakeAPI(t, (&signingAPI{t: t}).handle)

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
	api := newFakeAPI(t, (&signingAPI{t: t}).handle)
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
