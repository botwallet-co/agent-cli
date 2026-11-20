package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// =============================================================================
// Seed File Management
// =============================================================================

// saveSeed writes a key share (S1) to seeds/<localName>.seed in dir, with
// the wallet's deposit address (if known) in the file header, so a seed file
// can be matched to its wallet.
// It never replaces an existing file: Key 1 exists only in its seed file, so
// a file that is already there belongs to some wallet even if config.json no
// longer lists it. In that case the returned error wraps fs.ErrExist.
// IMPORTANT: Never prints key share to stdout.
func saveSeed(dir Dir, localName, seedPhrase, depositAddress string) (string, error) {
	// Ensure seeds directory exists (0700 = owner only)
	if err := os.MkdirAll(dir.SeedsDir(), 0700); err != nil {
		return "", fmt.Errorf("failed to create seeds directory: %w", err)
	}

	seedPath := dir.SeedFile(localName)

	addressLine := ""
	if depositAddress != "" {
		addressLine = "# Deposit address: " + depositAddress + "\n"
	}

	// Format seed file with warnings
	content := fmt.Sprintf(`# Botwallet Key Share (S1)
# Wallet: %s
%s#
# This is the bot's key share for threshold signing.
# It is ONE HALF of your wallet's signing capability.
# The server holds the other half (S2).
#
# ⚠️  Neither share alone can access your funds.
# ⚠️  For full recovery, you need BOTH S1 and S2.
# ⚠️  Get S2 from the Botwallet dashboard.
#
# To view this share: botwallet wallet backup
#

%s
`, localName, addressLine, seedPhrase)

	// Create with 0600 permissions (owner read/write only), never overwrite
	if err := writeNewFile(seedPath, []byte(content)); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("refusing to overwrite existing key share at %s: %w", seedPath, err)
		}
		return "", fmt.Errorf("failed to write seed file: %w", err)
	}

	return seedPath, nil
}

// walletSeedPath returns the absolute path of a local wallet's Key 1 file.
// seed_file in config.json is relative to the config folder
// ("seeds/<name>.seed"). Older MCP versions wrote just "<name>.seed" while
// saving the file in seeds/, so a bare file name that does not exist next to
// config.json is looked up in seeds/.
func walletSeedPath(dir Dir, localName string, entry WalletEntry) string {
	if entry.SeedFile == "" {
		return dir.SeedFile(localName)
	}
	path := filepath.Join(string(dir), filepath.FromSlash(entry.SeedFile))
	if filepath.Base(entry.SeedFile) == entry.SeedFile {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			inSeeds := filepath.Join(dir.SeedsDir(), entry.SeedFile)
			if _, err := os.Stat(inSeeds); err == nil {
				return inSeeds
			}
		}
	}
	return path
}

// LoadWalletSeed loads Key 1 for a local wallet, using its seed_file entry
// when the wallet is in config.json.
// IMPORTANT: Caller must handle securely, never print to stdout.
func LoadWalletSeed(localName string) (string, error) {
	cfg, dir, err := loadConfig()
	if err != nil {
		return "", err
	}
	if entry, ok := cfg.Wallets[localName]; ok {
		return LoadSeedFromPath(walletSeedPath(dir, localName, entry))
	}
	return LoadSeedFromPath(dir.SeedFile(localName))
}

// LoadSeedFromPath loads a key share from a specific file path.
// Supports both 12-word (FROST key share) and 24-word (legacy) mnemonics.
func LoadSeedFromPath(seedPath string) (string, error) {
	data, err := os.ReadFile(seedPath)
	if err != nil {
		return "", fmt.Errorf("failed to read seed file: %w", err)
	}

	// Parse seed from file (skip comment lines)
	var seedWords []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// This line should be the mnemonic phrase
		seedWords = strings.Fields(line)
		break
	}

	if len(seedWords) != 12 && len(seedWords) != 24 {
		return "", fmt.Errorf("invalid seed file: expected 12 or 24 words, found %d", len(seedWords))
	}

	return strings.Join(seedWords, " "), nil
}
