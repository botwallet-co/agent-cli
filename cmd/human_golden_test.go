package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/solana/frost"
)

// TestHumanOutputGolden runs commands with --human against canned API
// replies and compares what they print with testdata/human-golden.txt.
// After an intended change to the output, rewrite the file and review the
// diff:
//
//	go test ./cmd -run TestHumanOutputGolden -update
const humanGoldenFile = "testdata/human-golden.txt"

var update = flag.Bool("update", false, "rewrite "+humanGoldenFile)

// humanCase is one command run: its arguments and the reply to every API call.
type humanCase struct {
	name  string
	args  []string
	reply map[string]interface{} // data of a successful reply
}

func humanCases() []humanCase {
	const reqID = "3f6c1a2e-9b4d-4e8f-a1c2-7d5e9f0b1a23"
	return []humanCase{
		{"fund ask", []string{"fund", "ask", "50", "--reason", "API costs"}, map[string]interface{}{
			"request_id": reqID, "amount_usdc": 50.0, "reason": "API costs", "sent_to": "owner@example.com",
			"payment_url": "https://botwallet.co/pay/FR12"}},
		{"fund ask amount fallback", []string{"fund", "50", "--reason", "API costs"}, map[string]interface{}{
			"request_id": reqID, "amount": 50.0, "reason": "API costs"}},
		{"fund ask missing fields", []string{"fund", "ask", "5", "--reason", "x"}, map[string]interface{}{}},
		{"fund list", []string{"fund", "list"}, map[string]interface{}{"total": 3.0, "has_more": true, "requests": []interface{}{
			map[string]interface{}{"request_id": reqID, "status": "pending", "amount_usdc": 50.0,
				"reason": "Need to purchase database credits for the month"},
			map[string]interface{}{"request_id": "fr-short", "status": "funded", "amount": 12.5, "reason": "Short"},
			map[string]interface{}{"status": "dismissed", "amount_usdc": 0.0, "amount": 3.0},
			"not a request",
		}}},
		{"fund list empty", []string{"fund", "list"}, map[string]interface{}{"requests": []interface{}{}, "total": 0.0}},
		{"paylink get pending", []string{"paylink", "get", reqID}, map[string]interface{}{
			"request_id": reqID, "status": "pending", "amount_usdc": 25.0, "description": "Research report",
			"expires_at": "2026-10-02T12:00:00Z", "payment_url": "https://botwallet.co/pay/AB12CD"}},
		{"paylink get completed", []string{"paylink", "get", reqID}, map[string]interface{}{
			"request_id": reqID, "status": "completed", "amount": 25.0, "description": "Research report",
			"received_usdc": 24.5, "paid_by": "@data-bot", "paid_at": "2026-10-01T10:00:00Z"}},
		{"paylink get completed without receipt", []string{"paylink", "get", reqID}, map[string]interface{}{
			"request_id": reqID, "status": "completed", "amount_usdc": 25.0, "received": 24.5}},
		{"paylink get expired", []string{"paylink", "get", "--reference", "INV-1"}, map[string]interface{}{
			"status": "expired", "amount_usdc": 5.0}},
		{"paylink list", []string{"paylink", "list"}, map[string]interface{}{"total": 2.0, "has_more": true, "payment_requests": []interface{}{
			map[string]interface{}{"request_id": reqID, "short_code": "AB12CD", "status": "pending", "amount_usdc": 25.0,
				"description": "Research report for the third quarter"},
			map[string]interface{}{"status": "completed", "amount": 5.0},
			"not a paylink",
		}}},
		{"paylink list legacy key", []string{"paylink", "list"}, map[string]interface{}{"requests": []interface{}{
			map[string]interface{}{"short_code": "ZZ99", "status": "expired", "amount_usdc": 1.0, "description": "Old"},
		}}},
		{"paylink list empty", []string{"paylink", "list"}, map[string]interface{}{"payment_requests": []interface{}{}}},
		{"paylink cancel", []string{"paylink", "cancel", reqID}, map[string]interface{}{"request_id": reqID}},
		{"paylink create items", []string{"paylink", "create", "--desc", "Services", "--item", "API Calls, 5.00, 2", "--item", "Setup Fee, 10.00"},
			map[string]interface{}{"request_id": reqID, "short_code": "AB12CD", "payment_url": "https://botwallet.co/pay/AB12CD",
				"amount_usdc": 20.0, "description": "Services"}},
		{"paylink create one item", []string{"paylink", "create", "--desc", "Setup", "--item", "Setup Fee, 10.00"},
			map[string]interface{}{"request_id": reqID, "amount_usdc": 10.0, "description": "Setup"}},
		{"paylink send to a bot", []string{"paylink", "send", reqID, "--to", "@data-bot"}, map[string]interface{}{
			"sent_to": "wallet", "to": "@data-bot", "amount_usdc": 25.0, "short_code": "AB12CD"}},
		{"pay cancel", []string{"pay", "cancel", reqID}, map[string]interface{}{
			"transaction_id": reqID, "previous_status": "pre_approved", "amount_usdc": 10.0}},
		{"pay cancel fallback amount", []string{"pay", "cancel", reqID}, map[string]interface{}{"amount": 2.0}},
		{"wallet owner found", []string{"wallet", "owner", "boss@example.com"}, map[string]interface{}{
			"pledged_to": "boss@example.com", "owner_found": true, "claim_url": "https://app.botwallet.co/claim/abc",
			"claim_code": "ABC123"}},
		{"wallet owner new", []string{"wallet", "owner", "boss@example.com"}, map[string]interface{}{
			"pledged_to": "boss@example.com", "owner_found": false}},
		{"wallet rename", []string{"wallet", "rename", "Research Budget"}, map[string]interface{}{
			"name": "Research Budget", "username": "orion-1234"}},
		{"withdraw get completed", []string{"withdraw", "get", "wd-1"}, map[string]interface{}{
			"withdrawal_id": "wd-1", "status": "completed", "amount_usdc": 50.0, "network_fee_usdc": 0.5,
			"you_receive_usdc": 50.0, "to_address": "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
			"created_at": "2026-10-01T09:00:00Z", "completed_at": "2026-10-01T09:05:00Z", "solana_tx": "5VERv8NMvzbJ"}},
		{"withdraw get approved", []string{"withdraw", "get", "wd-1"}, map[string]interface{}{
			"withdrawal_id": "wd-1", "status": "approved", "amount_usdc": 50.0,
			"fee_breakdown": map[string]interface{}{"platform_fee_usdc": 0.5, "account_setup_fee_usdc": 0.25}}},
		{"withdraw get awaiting", []string{"withdraw", "get", "wd-1"}, map[string]interface{}{
			"withdrawal_id": "wd-1", "status": "awaiting_approval", "amount_usdc": 50.0,
			"approval_url": "https://app.botwallet.co/approve/ap-1"}},
		{"withdraw get pending", []string{"withdraw", "get", "wd-1"}, map[string]interface{}{"withdrawal_id": "wd-1", "status": "pending"}},
		{"withdraw get failed", []string{"withdraw", "get", "wd-1"}, map[string]interface{}{
			"withdrawal_id": "wd-1", "status": "failed", "failure_reason": "Blockhash expired"}},
		{"withdraw get denied", []string{"withdraw", "get", "wd-1"}, map[string]interface{}{"withdrawal_id": "wd-1", "status": "denied"}},
		{"withdraw get expired", []string{"withdraw", "get", "wd-1"}, map[string]interface{}{"withdrawal_id": "wd-1", "status": "expired"}},
		{"withdraw get other", []string{"withdraw", "get", "wd-1"}, map[string]interface{}{"withdrawal_id": "wd-1", "status": "reversed"}},
	}
}

// recordLocalWalletCommands records the commands that read and write the
// local wallet files (list, use, backup, reveal-backup, export, import) and
// ping, in both modes. Backup codes, the 12 words, addresses and temp paths
// differ from run to run, so they are replaced with placeholders.
func recordLocalWalletCommands(t *testing.T, record func(string, cliRun)) {
	t.Helper()
	home := t.TempDir()
	mainWallet := addTestWallet(t, home, "main")
	mnemonic, err := frost.GenerateShareMnemonic()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.ConfigDirEnv, home)
	research, err := config.AddWalletWithInfo("Research Budget", "research-1234", "Research Budget", "bw_bot_test_research", "", mnemonic)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.AddWalletWithInfo("legacy", "", "", "bw_bot_test_legacy", "", mnemonic); err != nil {
		t.Fatal(err)
	}
	if err := config.SetDefaultWallet("main"); err != nil {
		t.Fatal(err)
	}

	exportKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		switch action {
		case "wallet_export":
			return apiOK(map[string]interface{}{"export_id": testExportID, "encryption_key": exportKey})
		case "wallet_import_key":
			return apiOK(map[string]interface{}{"encryption_key": exportKey})
		case "ping":
			return apiOK(map[string]interface{}{"version": "1.2.3", "timestamp": "2026-10-01T12:00:00Z"})
		}
		return apiFail(http.StatusBadRequest, "VALIDATION_ERROR", "unexpected action "+action)
	})

	placeholders := []string{mainWallet.address, "<address of main>"}
	codePattern := regexp.MustCompile(`(--code |"code": ")[a-z0-9]{4}`)
	scrub := func(run cliRun) cliRun {
		r := strings.NewReplacer(placeholders...)
		run.stdout = codePattern.ReplaceAllString(r.Replace(run.stdout), "${1}<code>")
		run.stderr = r.Replace(run.stderr)
		return run
	}
	runIn := func(home string, args ...string) cliRun {
		return scrub(runCLI(t, home, api.URL, nil, args...))
	}

	record("wallet list (json)", runIn(home, "wallet", "list"))
	record("wallet list", runIn(home, "wallet", "list", "--human"))
	record("wallet list empty", runIn(t.TempDir(), "wallet", "list", "--human"))
	record("wallet use (json)", runIn(home, "wallet", "use", research.LocalName))
	record("wallet use already default", runIn(home, "wallet", "use", research.LocalName, "--human"))
	record("wallet use", runIn(home, "wallet", "use", "main", "--human"))
	record("wallet use already default (json)", runIn(home, "wallet", "use", "main"))
	record("wallet use unknown", runIn(home, "wallet", "use", "nope", "--human"))

	// Backup and reveal-backup act as the default wallet, main.
	mainWords, err := config.LoadWalletSeed("main")
	if err != nil {
		t.Fatal(err)
	}
	placeholders = append(placeholders, mainWords, "<12 words of main>")
	for _, human := range []bool{false, true} {
		mode, flags := " (json)", []string(nil)
		if human {
			mode, flags = "", []string{"--human"}
		}
		backup := runCLI(t, home, api.URL, nil, append([]string{"wallet", "backup"}, flags...)...)
		code := codePattern.FindString(backup.stdout)
		if code == "" {
			t.Fatalf("no code in wallet backup output:\n%s", backup.stdout)
		}
		code = code[len(code)-4:]
		record("wallet backup"+mode, scrub(backup))
		record("wallet reveal-backup"+mode, runIn(home, append([]string{"wallet", "reveal-backup", "--code", code}, flags...)...))
	}

	exportDir := t.TempDir()
	for _, human := range []bool{false, true} {
		mode, flags, file := " (json)", []string(nil), filepath.Join(exportDir, "main.bwlt")
		if human {
			mode, flags, file = "", []string{"--human"}, filepath.Join(exportDir, "main-human")
		}
		absFile, _ := filepath.Abs(file)
		if !strings.HasSuffix(absFile, ".bwlt") {
			absFile += ".bwlt"
		}
		escaped, _ := json.Marshal(absFile)
		placeholders = append(placeholders, string(escaped[1:len(escaped)-1]), "<export file>", absFile, "<export file>")
		record("wallet export"+mode, runIn(home, append([]string{"wallet", "export", "-o", file}, flags...)...))
		record("wallet import"+mode, runIn(t.TempDir(), append([]string{"wallet", "import", absFile}, flags...)...))
	}

	record("ping (json)", runIn(home, "ping"))
	record("ping", runIn(home, "ping", "--human"))
}

// fakeExternalAPI stands in for x402 APIs at http://api.example.test, reached
// through HTTP_PROXY (the probe refuses localhost addresses).
func fakeExternalAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/free":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"temp":20}`))
		case "/missing":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("no such thing"))
		case "/base-only":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write([]byte(`{"x402Version":1,"accepts":[{"scheme":"exact","network":"base",` +
				`"maxAmountRequired":"10000","payTo":"0xabc","asset":"0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"}]}`))
		case "/paid":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write([]byte(`{"x402Version":1,"accepts":[{"scheme":"exact","network":"solana",` +
				`"maxAmountRequired":"10000","payTo":"` + payeeAddress.String() + `","asset":"` + usdcMint.String() + `"}]}`))
		case "/moved":
			http.Redirect(w, r, "/paid", http.StatusFound)
		case "/elsewhere":
			http.Redirect(w, r, "http://pay.other.test/paid", http.StatusFound)
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHumanOutputGolden(t *testing.T) {
	var b strings.Builder
	record := func(name string, run cliRun) {
		fmt.Fprintf(&b, "=== %s (exit %d)\n--- stdout\n%s--- stderr\n%s", name, run.exitCode, run.stdout, run.stderr)
	}

	for _, c := range humanCases() {
		home := t.TempDir()
		addTestWallet(t, home, "main")
		reply := c.reply
		api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
			return apiOK(reply)
		})
		record(c.name, runCLI(t, home, api.URL, nil, append(c.args, "--human")...))
	}

	// wallet rename also adds a note to its JSON output
	home := t.TempDir()
	addTestWallet(t, home, "main")
	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		return apiOK(map[string]interface{}{"name": "Research Budget", "username": "orion-1234"})
	})
	record("wallet rename (json)", runCLI(t, home, api.URL, nil, "wallet", "rename", "Research Budget"))

	// The confirm summary shown before signing
	for _, c := range submittingCommands {
		home, server := newSigningServer(t)
		server.confirm["fee_breakdown"] = map[string]interface{}{"platform_fee_usdc": 0.1, "account_setup_fee_usdc": 0.15}
		api := newFakeAPI(t, server.handle)
		record(c.name+" confirm", runCLI(t, home, api.URL, nil, append(c.args, "--human")...))
	}

	// x402 probes of free and incompatible APIs
	external := fakeExternalAPI(t)
	proxy := []string{"HTTP_PROXY=" + external.URL, "http_proxy=" + external.URL, "NO_PROXY=", "no_proxy="}
	for _, path := range []string{"/free", "/missing", "/base-only"} {
		home := t.TempDir()
		addTestWallet(t, home, "main")
		api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
			return apiFail(http.StatusBadRequest, "VALIDATION_ERROR", "unexpected action "+action)
		})
		record("x402 fetch "+path, runCLI(t, home, api.URL, proxy, "x402", "fetch", "http://api.example.test"+path, "--human"))
		if calls := api.called(); len(calls) != 0 {
			t.Errorf("x402 fetch %s called the Botwallet API: %v", path, calls)
		}
	}

	recordLocalWalletCommands(t, record)

	got := b.String()
	if *update {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(humanGoldenFile, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(humanGoldenFile)
	if err != nil {
		t.Fatalf("%v (create it with: go test ./cmd -run TestHumanOutputGolden -update)", err)
	}
	want := strings.ReplaceAll(string(data), "\r\n", "\n")
	if got != want {
		gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
		for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
			var g, w string
			if i < len(gotLines) {
				g = gotLines[i]
			}
			if i < len(wantLines) {
				w = wantLines[i]
			}
			if g != w {
				t.Fatalf("output differs from %s at line %d:\n got: %q\nwant: %q\n(if the change is intended: go test ./cmd -run TestHumanOutputGolden -update, then review the diff)",
					humanGoldenFile, i+1, g, w)
			}
		}
	}
}
