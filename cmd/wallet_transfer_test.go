package cmd

import (
	"encoding/base64"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/botwallet-co/agent-cli/config"
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
