package config

import (
	cryptoRand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"time"
)

// =============================================================================
// Backup Nonce Management (Speed Bump for S1 reveal)
// =============================================================================

// BackupNonceTTL is how long a 'wallet backup' confirmation code stays
// valid. An agent runs reveal-backup as a separate tool call after reading
// the warning, which can take well over half a minute; the code stays
// single-use.
const BackupNonceTTL = 5 * time.Minute

// BackupNonceTTLText is BackupNonceTTL in words, e.g. "5 minutes".
func BackupNonceTTLText() string {
	if BackupNonceTTL%time.Minute != 0 {
		return fmt.Sprintf("%d seconds", int(BackupNonceTTL/time.Second))
	}
	if n := int(BackupNonceTTL / time.Minute); n != 1 {
		return fmt.Sprintf("%d minutes", n)
	}
	return "1 minute"
}

// BackupNonce represents a one-time code for the backup flow
type BackupNonce struct {
	Code      string `json:"code"`
	Wallet    string `json:"wallet"`
	CreatedAt string `json:"created_at"`
}

// WriteBackupNonce creates a new backup nonce and saves it, replacing any
// earlier one.
func WriteBackupNonce(code string, walletName string) error {
	nonce := BackupNonce{
		Code:      code,
		Wallet:    walletName,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(nonce)
	if err != nil {
		return fmt.Errorf("failed to marshal nonce: %w", err)
	}

	dir, err := ResolveConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(string(dir), 0700); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	return os.WriteFile(dir.BackupNonceFile(), data, 0600)
}

// ValidateBackupNonce checks if a code matches the stored nonce and is within the time limit.
// Returns the wallet name if valid.
//
// Deletion policy:
//   - Correct code → delete (single-use, success)
//   - Expired code → delete (force re-generation)
//   - Wrong code → keep (allow retry with correct code)
//   - Corrupt file → delete (unrecoverable)
func ValidateBackupNonce(code string) (walletName string, err error) {
	dir, err := ResolveConfigDir()
	if err != nil {
		return "", err
	}
	path := dir.BackupNonceFile()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("no pending backup request. Run 'botwallet wallet backup' first")
		}
		return "", fmt.Errorf("failed to read nonce: %w", err)
	}

	var nonce BackupNonce
	if err := json.Unmarshal(data, &nonce); err != nil {
		os.Remove(path) // Corrupt file — clean up
		return "", fmt.Errorf("invalid nonce file. Run 'botwallet wallet backup' again")
	}

	// Check expiry first (BackupNonceTTL) — expired nonces are always deleted
	createdAt, err := time.Parse(time.RFC3339, nonce.CreatedAt)
	if err != nil {
		os.Remove(path)
		return "", fmt.Errorf("invalid nonce timestamp. Run 'botwallet wallet backup' again")
	}

	if time.Since(createdAt) > BackupNonceTTL {
		os.Remove(path) // Expired — force re-generation
		return "", fmt.Errorf("confirmation code expired (%s limit). Run 'botwallet wallet backup' again, then 'wallet reveal-backup --code <code>' right after it",
			BackupNonceTTLText())
	}

	// Check code — wrong code does NOT delete the nonce (allows retry)
	if nonce.Code != code {
		return "", fmt.Errorf("invalid confirmation code. Check the code from 'botwallet wallet backup' output")
	}

	// Success — delete the nonce (single-use)
	os.Remove(path)
	return nonce.Wallet, nil
}

// GenerateBackupCode generates a random 4-character alphanumeric code
// using crypto/rand with uniform distribution (no modulo bias).
func GenerateBackupCode() string {
	const chars = "abcdefghjkmnpqrstuvwxyz23456789" // No ambiguous chars (l, 1, o, 0, i)
	max := big.NewInt(int64(len(chars)))
	b := make([]byte, 4)
	for i := range b {
		n, err := cryptoRand.Int(cryptoRand.Reader, max)
		if err != nil {
			// crypto/rand.Reader does not fail (Go 1.24+ crashes the program
			// first); a guessable code must never be the fallback.
			panic("botwallet: crypto/rand failed: " + err.Error())
		}
		b[i] = chars[n.Int64()]
	}
	return string(b)
}
