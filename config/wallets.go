package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"time"
)

// =============================================================================
// Wallet Management
// =============================================================================

// LocalWallet is a wallet in config.json.
type LocalWallet struct {
	LocalName string
	Entry     WalletEntry
	IsDefault bool
}

// ListWallets returns all wallets sorted by name
func ListWallets() ([]LocalWallet, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	names := walletNames(cfg)
	result := make([]LocalWallet, len(names))
	for i, name := range names {
		result[i] = LocalWallet{
			LocalName: name,
			Entry:     cfg.Wallets[name],
			IsDefault: name == cfg.DefaultWallet,
		}
	}

	return result, nil
}

// WalletNames returns the local names of the wallets in config.json, sorted.
func WalletNames() ([]string, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	return walletNames(cfg), nil
}

// GetWallet retrieves a wallet entry by local name. A name that is not in
// config.json gives a *WalletNotFoundError.
func GetWallet(localName string) (*WalletEntry, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	wallet, exists := cfg.Wallets[localName]
	if !exists {
		return nil, &WalletNotFoundError{Name: localName, Available: walletNames(cfg)}
	}

	return &wallet, nil
}

// GetDefaultWallet returns the default wallet entry
func GetDefaultWallet() (*WalletEntry, string, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, "", err
	}

	if cfg.DefaultWallet == "" {
		return nil, "", fmt.Errorf("no default wallet set")
	}

	wallet, exists := cfg.Wallets[cfg.DefaultWallet]
	if !exists {
		return nil, "", fmt.Errorf("default wallet '%s' not found", cfg.DefaultWallet)
	}

	return &wallet, cfg.DefaultWallet, nil
}

// SetDefaultWallet sets the default wallet
func SetDefaultWallet(localName string) error {
	return withConfigLock(func(dir Dir) error {
		cfg, err := loadConfigFrom(dir)
		if err != nil {
			return err
		}

		// Check wallet exists
		if _, exists := cfg.Wallets[localName]; !exists {
			return fmt.Errorf("wallet '%s' not found", localName)
		}

		cfg.DefaultWallet = localName
		return saveConfig(dir, cfg)
	})
}

// UpdateWalletDisplayName updates only the display name of a wallet in the local config.
// The map key (localName) and seed file path are never changed.
func UpdateWalletDisplayName(localName, newDisplayName string) error {
	return withConfigLock(func(dir Dir) error {
		cfg, err := loadConfigFrom(dir)
		if err != nil {
			return err
		}

		wallet, exists := cfg.Wallets[localName]
		if !exists {
			return fmt.Errorf("wallet '%s' not found", localName)
		}

		wallet.DisplayName = newDisplayName
		cfg.Wallets[localName] = wallet

		return saveConfig(dir, cfg)
	})
}

// freeLocalName returns sanitize(name), or the same with a -2, -3, ...
// suffix, choosing the first that is neither a config entry nor an existing
// seed file in dir. A seed file without a config entry still holds some
// wallet's Key 1, so its name is never reused.
func freeLocalName(dir Dir, cfg *Config, name string) string {
	baseName := sanitizeFilename(name)
	if !localNameTaken(dir, cfg, baseName) {
		return baseName
	}

	// Add numeric suffix
	for i := 2; i <= 100; i++ {
		candidate := fmt.Sprintf("%s-%d", baseName, i)
		if !localNameTaken(dir, cfg, candidate) {
			return candidate
		}
	}

	// Fallback to timestamp
	return fmt.Sprintf("%s-%d", baseName, time.Now().UnixNano())
}

// localNameTaken reports whether localName is a config entry or has a seed
// file on disk. Anything but a clear "does not exist" counts as taken.
func localNameTaken(dir Dir, cfg *Config, localName string) bool {
	if _, exists := cfg.Wallets[localName]; exists {
		return true
	}
	_, err := os.Lstat(dir.SeedFile(localName))
	return !errors.Is(err, fs.ErrNotExist)
}

// walletNames returns the local wallet names, sorted.
func walletNames(cfg *Config) []string {
	names := make([]string, 0, len(cfg.Wallets))
	for name := range cfg.Wallets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
