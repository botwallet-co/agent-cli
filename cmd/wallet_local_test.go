package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/solana/frost"
)

// An export imported twice gives two local wallets for one wallet, with the
// same username. wallet use tells them apart by local name.
func TestWalletUseBetweenTwoCopiesOfAWallet(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.ConfigDirEnv, home)
	mnemonic, err := frost.GenerateShareMnemonic()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := config.AddWalletWithInfo("main", "orion-1234", "main", "bw_bot_test_orion", testAddress("orion").String(), mnemonic); err != nil {
			t.Fatal(err)
		}
	}

	// main-2, added last, is the default.
	run := runCLI(t, home, "http://127.0.0.1:0", nil, "wallet", "use", "main")
	if out := run.json(t); run.exitCode != 0 || out["default"] != "main" || out["already_default"] != false {
		t.Fatalf("exit %d, output %v; want main as the new default", run.exitCode, out)
	}
	if out := runCLI(t, home, "http://127.0.0.1:0", nil, "wallet", "use", "main").json(t); out["already_default"] != true {
		t.Errorf("wallet use main again: %v, want already_default", out)
	}
}

// A config.json that cannot be read is a config error, not an unknown wallet.
func TestWalletUseWithBrokenConfig(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}

	run := runCLI(t, home, "http://127.0.0.1:0", nil, "wallet", "use", "main")
	if code := run.json(t)["error"]; run.exitCode == 0 || code != "CONFIG_ERROR" {
		t.Errorf("exit %d, error = %v, want CONFIG_ERROR\n%s", run.exitCode, code, run.stdout)
	}
}
