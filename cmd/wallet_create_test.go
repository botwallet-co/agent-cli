package cmd

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/solana/frost"
)

const testAPIKey = "bw_bot_test_only_key"

// dkgServer plays the server side of wallet creation. onComplete, if set,
// runs inside dkg_complete and returns the response; by default the wallet
// is created.
type dkgServer struct {
	t          *testing.T
	home       string
	onComplete func(req map[string]interface{}) (int, interface{})
}

func (d *dkgServer) handle(action string, req map[string]interface{}) (int, interface{}) {
	switch action {
	case "dkg_init":
		share, err := frost.GenerateKeyShare()
		if err != nil {
			d.t.Errorf("GenerateKeyShare: %v", err)
		}
		return apiOK(map[string]interface{}{
			"session_id":          "session-1",
			"server_public_share": base64.StdEncoding.EncodeToString(frost.EncodePoint(share.Public)),
		})
	case "dkg_complete":
		if d.onComplete != nil {
			return d.onComplete(req)
		}
		return apiOK(map[string]interface{}{
			"api_key":    testAPIKey,
			"username":   "my-bot-1234",
			"wallet_id":  "wallet-1",
			"claim_url":  "https://example.invalid/claim",
			"claim_code": "CLAIM1",
		})
	}
	return apiFail(http.StatusBadRequest, "VALIDATION_ERROR", "unexpected action "+action)
}

func seedFiles(t *testing.T, home string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(home, "seeds"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func loadConfigAt(t *testing.T, home string) *config.Config {
	t.Helper()
	t.Setenv(config.ConfigDirEnv, home)
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestWalletCreateSavesKey1BeforeServerCreatesWallet(t *testing.T) {
	home := t.TempDir()
	d := &dkgServer{t: t, home: home}
	d.onComplete = func(req map[string]interface{}) (int, interface{}) {
		// Key 1 must already be on disk when the server creates the wallet.
		files := seedFiles(t, home)
		if len(files) != 1 || files[0] != "my-bot.seed" {
			t.Errorf("seed files at dkg_complete = %v, want [my-bot.seed]", files)
		}
		d.onComplete = nil
		return d.handle("dkg_complete", req)
	}
	api := newFakeAPI(t, d.handle)

	run := runCLI(t, home, api.URL, nil, "wallet", "create", "--name", "My Bot")
	if run.exitCode != 0 {
		t.Fatalf("exit %d\n%s\n%s", run.exitCode, run.stdout, run.stderr)
	}
	if out := run.json(t); out["success"] != true {
		t.Fatalf("unexpected output: %v", out)
	}

	cfg := loadConfigAt(t, home)
	entry, ok := cfg.Wallets["my-bot"]
	if !ok || entry.APIKey != testAPIKey || cfg.DefaultWallet != "my-bot" {
		t.Fatalf("config after create = %+v", cfg)
	}
	seed, err := os.ReadFile(filepath.Join(home, "seeds", "my-bot.seed"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(seed), "# Deposit address: "+entry.PublicKey+"\n") {
		t.Errorf("seed file does not record deposit address %s", entry.PublicKey)
	}
	if strings.Contains(run.stdout, testAPIKey) {
		t.Error("API key printed to stdout")
	}
}

func TestWalletCreateRefusedRemovesUnusedSeed(t *testing.T) {
	home := t.TempDir()
	d := &dkgServer{t: t, home: home}
	d.onComplete = func(map[string]interface{}) (int, interface{}) {
		return apiFail(http.StatusBadRequest, "SESSION_EXPIRED", "DKG session expired")
	}
	api := newFakeAPI(t, d.handle)

	run := runCLI(t, home, api.URL, nil, "wallet", "create", "--name", "My Bot")
	if run.exitCode == 0 {
		t.Fatal("create succeeded, want failure")
	}
	if code := run.json(t)["error"]; code != "SESSION_EXPIRED" {
		t.Errorf("error = %v, want SESSION_EXPIRED", code)
	}
	if files := seedFiles(t, home); len(files) != 0 {
		t.Errorf("seed files left after a refused create: %v", files)
	}
}

func TestWalletCreateUncertainOutcomeKeepsSeed(t *testing.T) {
	home := t.TempDir()
	d := &dkgServer{t: t, home: home}
	d.onComplete = func(map[string]interface{}) (int, interface{}) {
		// A gateway error: the wallet may or may not have been created.
		return http.StatusBadGateway, "<html>502 Bad Gateway</html>"
	}
	api := newFakeAPI(t, d.handle)

	run := runCLI(t, home, api.URL, nil, "wallet", "create", "--name", "My Bot")
	if run.exitCode == 0 {
		t.Fatal("create succeeded, want failure")
	}
	out := run.json(t)
	if out["error"] != "REGISTRATION_UNCERTAIN" {
		t.Fatalf("error = %v, want REGISTRATION_UNCERTAIN", out["error"])
	}
	details, _ := out["details"].(map[string]interface{})
	address, _ := details["deposit_address"].(string)
	want := "unregistered-" + address + ".seed"
	if files := seedFiles(t, home); address == "" || len(files) != 1 || files[0] != want {
		t.Fatalf("seed files = %v, want [%s]", files, want)
	}
	kept, _ := details["key1_file"].(string)
	if words, err := config.LoadSeedFromPath(kept); err != nil || len(strings.Fields(words)) != 12 {
		t.Errorf("kept seed file %q: %v", kept, err)
	}

	// The local name is free again for the next attempt.
	d.onComplete = nil
	run = runCLI(t, home, api.URL, nil, "wallet", "create", "--name", "My Bot")
	if run.exitCode != 0 {
		t.Fatalf("second create: exit %d\n%s", run.exitCode, run.stdout)
	}
	if _, ok := loadConfigAt(t, home).Wallets["my-bot"]; !ok {
		t.Error("second create did not use the name my-bot")
	}
}

func TestWalletCreateStopsBeforeServerWhenConfigIsBroken(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	d := &dkgServer{t: t, home: home}
	api := newFakeAPI(t, d.handle)

	run := runCLI(t, home, api.URL, nil, "wallet", "create", "--name", "My Bot")
	if run.exitCode == 0 {
		t.Fatal("create succeeded with a broken config.json")
	}
	if code := run.json(t)["error"]; code != "CONFIG_ERROR" {
		t.Errorf("error = %v, want CONFIG_ERROR", code)
	}
	if calls := api.called(); len(calls) != 0 {
		t.Errorf("server was called before the local check failed: %v", calls)
	}
}

func TestWalletCreateKeepsAPIKeyWhenConfigCannotBeSaved(t *testing.T) {
	home := t.TempDir()
	d := &dkgServer{t: t, home: home}
	d.onComplete = func(req map[string]interface{}) (int, interface{}) {
		// config.json breaks while the server is creating the wallet.
		if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("{not json"), 0600); err != nil {
			t.Error(err)
		}
		d.onComplete = nil
		return d.handle("dkg_complete", req)
	}
	api := newFakeAPI(t, d.handle)

	run := runCLI(t, home, api.URL, nil, "wallet", "create", "--name", "My Bot")
	if run.exitCode == 0 {
		t.Fatal("create succeeded, want a config error")
	}
	out := run.json(t)
	if out["error"] != "CONFIG_ERROR" {
		t.Fatalf("error = %v, want CONFIG_ERROR", out["error"])
	}
	if strings.Contains(run.stdout, testAPIKey) || strings.Contains(run.stderr, testAPIKey) {
		t.Error("API key printed although the pending file was written")
	}

	details, _ := out["details"].(map[string]interface{})
	pendingPath, _ := details["pending_file"].(string)
	data, err := os.ReadFile(pendingPath)
	if err != nil {
		t.Fatalf("pending file: %v", err)
	}
	var pending config.PendingWallet
	if err := json.Unmarshal(data, &pending); err != nil {
		t.Fatal(err)
	}
	if pending.ConfigEntry.APIKey != testAPIKey || pending.ConfigEntry.SeedFile != "seeds/my-bot.seed" || pending.ClaimCode != "CLAIM1" {
		t.Errorf("pending record = %+v", pending)
	}
	if files := seedFiles(t, home); len(files) != 1 || files[0] != "my-bot.seed" {
		t.Errorf("seed files = %v, want [my-bot.seed]", files)
	}
}

func TestDKGCompleteRefused(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"expired session", apiError("SESSION_EXPIRED", "DKG session expired"), true},
		{"group key mismatch", apiError("DKG_VERIFICATION_FAILED", "Group key mismatch"), true},
		{"bad input", apiError("VALIDATION_ERROR", "agent_public_share must be 32 bytes"), true},
		{"key already registered", apiError("VALIDATION_ERROR", "This group key is already registered"), false},
		{"server error", apiError("INTERNAL_ERROR", "Failed to create wallet"), false},
		{"network error", errString("request failed: timeout"), false},
	}
	for _, tt := range tests {
		if got := dkgCompleteRefused(tt.err); got != tt.want {
			t.Errorf("%s: dkgCompleteRefused = %v, want %v", tt.name, got, tt.want)
		}
	}
}
