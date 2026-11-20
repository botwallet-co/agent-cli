package cmd

import (
	"bytes"
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

const testExportID = "0b6f3f5e-3c1a-4a52-9a43-2f0d7c9e8b11"

// The server's export key works for 5 imports within 24 hours, so the
// export must not be described as a backup or something to keep.
func TestWalletExportIsATransferFileNotABackup(t *testing.T) {
	home := t.TempDir()
	addTestWallet(t, home, "main")
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		return apiOK(map[string]interface{}{"export_id": testExportID, "encryption_key": key})
	})

	file := filepath.Join(t.TempDir(), "main.bwlt")
	run := runCLI(t, home, api.URL, nil, "wallet", "export", "-o", file)
	if run.exitCode != 0 {
		t.Fatalf("exit %d\n%s", run.exitCode, run.stdout)
	}
	out := run.json(t)
	if s, _ := out["valid_for"].(string); !strings.Contains(s, "24 hours") || !strings.Contains(s, "5 imports") {
		t.Errorf("valid_for = %q, want the 24 hour / 5 import limit", s)
	}
	if s, _ := out["owner_action"].(string); !strings.Contains(s, "not a backup") {
		t.Errorf("owner_action = %q, want it to say the file is not a backup", s)
	}

	human := runCLI(t, home, api.URL, nil, "wallet", "export", "-o", filepath.Join(t.TempDir(), "again.bwlt"), "--human")
	help := runCLI(t, home, api.URL, nil, "wallet", "export", "--help")
	for _, text := range []string{run.stdout, human.stdout, help.stdout} {
		for _, wrong := range []string{"safekeeping", "does not expire", "encrypted backup"} {
			if strings.Contains(text, wrong) {
				t.Errorf("output says %q:\n%s", wrong, text)
			}
		}
	}
}

// writeTestExport writes a .bwlt file holding payload, encrypted with key,
// and returns a fake API that hands out key for it.
func writeTestExport(t *testing.T, path string, payload config.BWLTPayload) *fakeAPI {
	t.Helper()
	key := bytes.Repeat([]byte{7}, 32)
	plaintext, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	nonce, ciphertext, err := config.EncryptPayload(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.WriteBWLT(path, testExportID, nonce, ciphertext); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	return newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		return apiOK(map[string]interface{}{"encryption_key": encoded})
	})
}

// An import becomes the default wallet, so a file whose Key 1 or address
// could not be used is refused before anything is saved.
func TestWalletImportRefusesUnusableKeys(t *testing.T) {
	words, err := frost.GenerateShareMnemonic()
	if err != nil {
		t.Fatal(err)
	}
	valid := config.BWLTPayload{Version: config.BWLTPayloadVersion, WalletName: "Orion", Username: "orion-1234",
		APIKey: "bw_bot_test_orion", PublicKey: testAddress("orion").String(), Seed: words}

	for name, edit := range map[string]func(*config.BWLTPayload){
		"words that are not a phrase": func(p *config.BWLTPayload) { p.Seed = "one two three four five six seven eight nine ten eleven twelve" },
		"a bad checksum word":         func(p *config.BWLTPayload) { p.Seed = strings.TrimSpace(strings.Repeat("abandon ", 12)) },
		"an address that is not one":  func(p *config.BWLTPayload) { p.PublicKey = "not-an-address" },
		"an address of 31 bytes":      func(p *config.BWLTPayload) { p.PublicKey = strings.Repeat("2", 32) },
	} {
		t.Run(name, func(t *testing.T) {
			payload := valid
			edit(&payload)
			file := filepath.Join(t.TempDir(), "w.bwlt")
			api := writeTestExport(t, file, payload)
			home := t.TempDir()

			out := checkError(t, home, api, []string{"wallet", "import", file}, "IMPORT_ERROR", "Export the wallet again")
			if msg, _ := out["message"].(string); !strings.Contains(msg, "not usable") {
				t.Errorf("message = %q", msg)
			}
			if entries, _ := os.ReadDir(home); len(entries) != 0 {
				t.Errorf("the refused import wrote %v", entries)
			}
		})
	}

	// The same file with valid keys imports.
	file := filepath.Join(t.TempDir(), "w.bwlt")
	api := writeTestExport(t, file, valid)
	if run := runCLI(t, t.TempDir(), api.URL, nil, "wallet", "import", file); run.exitCode != 0 {
		t.Fatalf("valid file: exit %d\n%s", run.exitCode, run.stdout)
	}
}

// Key 1 is saved with its words joined by single spaces, whatever the
// spacing in the file.
func TestWalletImportSavesKey1Words(t *testing.T) {
	words, err := frost.GenerateShareMnemonic()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "w.bwlt")
	api := writeTestExport(t, file, config.BWLTPayload{Version: config.BWLTPayloadVersion, WalletName: "Orion",
		Username: "orion-1234", APIKey: "bw_bot_test_orion", PublicKey: testAddress("orion").String(),
		Seed: "  " + strings.ReplaceAll(words, " ", " \n\t ") + "\n"})

	home := t.TempDir()
	run := runCLI(t, home, api.URL, nil, "wallet", "import", file)
	if run.exitCode != 0 {
		t.Fatalf("exit %d\n%s", run.exitCode, run.stdout)
	}
	name, _ := run.json(t)["wallet_name"].(string)
	t.Setenv(config.ConfigDirEnv, home)
	if saved, err := config.LoadWalletSeed(name); err != nil || saved != words {
		t.Errorf("saved Key 1 = %q, %v; want %q", saved, err, words)
	}
}

// After 5 imports or 24 hours the server answers EXPORT_EXPIRED once and
// NOT_FOUND after that. Both must say to export again on the wallet's machine.
func TestWalletImportOfExpiredExport(t *testing.T) {
	file := filepath.Join(t.TempDir(), "old.bwlt")
	if err := config.WriteBWLT(file, testExportID, make([]byte, 12), make([]byte, 64)); err != nil {
		t.Fatal(err)
	}

	for _, answer := range []struct {
		status int
		code   string
	}{
		{http.StatusForbidden, "EXPORT_EXPIRED"},
		{http.StatusNotFound, "NOT_FOUND"},
	} {
		home := t.TempDir()
		api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
			return apiFail(answer.status, answer.code, "This export has expired")
		})

		run := runCLI(t, home, api.URL, nil, "wallet", "import", file)
		if run.exitCode == 0 {
			t.Fatalf("%s: exit 0, want an error", answer.code)
		}
		out := run.json(t)
		if out["error"] != "EXPORT_EXPIRED" {
			t.Errorf("%s: error = %v, want EXPORT_EXPIRED", answer.code, out["error"])
		}
		if fix, _ := out["how_to_fix"].(string); !strings.Contains(fix, "botwallet wallet export") {
			t.Errorf("%s: how_to_fix = %q, want it to say to export again", answer.code, fix)
		}
	}
}
