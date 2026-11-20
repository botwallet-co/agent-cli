package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testExportID = "0b6f3f5e-3c1a-4a52-9a43-2f0d7c9e8b11"

func TestBWLTRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.bwlt")
	nonce := bytes.Repeat([]byte{7}, bwltNonceLen)
	ciphertext := bytes.Repeat([]byte{9}, 40)
	if err := WriteBWLT(path, testExportID, nonce, ciphertext); err != nil {
		t.Fatal(err)
	}
	id, gotNonce, gotCipher, err := ReadBWLT(path)
	if err != nil || id != testExportID || !bytes.Equal(gotNonce, nonce) || !bytes.Equal(gotCipher, ciphertext) {
		t.Fatalf("ReadBWLT = %q, %x, %x, %v", id, gotNonce, gotCipher, err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
			t.Errorf("file mode = %v, %v; want 0600", info.Mode().Perm(), err)
		}
	}

	// The same file with another first byte is not a .bwlt file.
	valid, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append([]byte("X"), valid[1:]...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ReadBWLT(path); err == nil || !strings.Contains(err.Error(), "invalid magic bytes") {
		t.Errorf("ReadBWLT of a file without the magic bytes: error = %v, want invalid magic bytes", err)
	}

	// Neither is a file one byte short of the smallest valid one.
	if err := os.WriteFile(path, valid[:bwltMinSize-1], 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ReadBWLT(path); err == nil || !strings.Contains(err.Error(), "too small") {
		t.Errorf("ReadBWLT of a file one byte too short: error = %v, want too small", err)
	}
}

// An export never replaces a file, even one created after the command
// checked that the path was free: it may be another wallet's export.
func TestWriteBWLTNeverReplacesAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "taken.bwlt")
	if err := os.WriteFile(path, []byte("someone else's file"), 0600); err != nil {
		t.Fatal(err)
	}
	err := WriteBWLT(path, testExportID, make([]byte, bwltNonceLen), make([]byte, 32))
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("WriteBWLT onto an existing file: error = %v, want fs.ErrExist", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "someone else's file" {
		t.Errorf("existing file was changed to %q", got)
	}
}

// The payload's JSON names are the .bwlt format, which the MCP server
// reads and writes too.
func TestBWLTPayloadJSON(t *testing.T) {
	got, err := json.Marshal(BWLTPayload{Version: BWLTPayloadVersion, WalletName: "Orion", Username: "orion-1234",
		APIKey: "key", PublicKey: "address", Seed: "twelve words", ExportedAt: "2026-10-01T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"wallet_name":"Orion","username":"orion-1234","api_key":"key","public_key":"address",` +
		`"seed":"twelve words","exported_at":"2026-10-01T12:00:00Z"}`
	if string(got) != want {
		t.Errorf("payload JSON = %s\nwant %s", got, want)
	}
}
