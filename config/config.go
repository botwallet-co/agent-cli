// =============================================================================
// Botwallet CLI Configuration - Multi-Wallet Support
// =============================================================================
// Handles secure storage of multiple wallet credentials:
// - API keys stored in config.json (0600 permissions)
// - Key shares (S1) stored separately in seeds/ folder (0600 permissions)
// - Key shares are NEVER printed to stdout during normal operation
//
// Directory structure (set BOTWALLET_HOME to use another folder):
//   ~/.botwallet/
//   ├── config.json          # Wallet registry + API keys (0600)
//   ├── config.lock          # Exists only while a command updates config.json (lock.go)
//   ├── pending-*.json       # Only if a new wallet could not be added to config.json
//   ├── .backup-nonce        # Temporary backup nonce (auto-deleted)
//   └── seeds/               # Key share folder (0700)
//       ├── my-bot.seed      # Individual key share files (0600, never overwritten)
//       ├── test-bot.seed
//       └── unregistered-<address>.seed  # Key 1 of a create whose outcome was unknown
//
// API Key Priority (see ResolveWallet):
// 1. --api-key flag (highest priority)
// 2. BOTWALLET_API_KEY (or BW_API_KEY) environment variable
// 3. --wallet flag (selects wallet from config)
// 4. Default wallet from config file
// --wallet together with a key from 1 or 2 must name the same wallet.
// =============================================================================

package config

import (
	cryptoRand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// Config Structures
// =============================================================================

// WalletEntry represents a single wallet in the config
type WalletEntry struct {
	Username    string `json:"username"`               // Server-assigned username (e.g., "clever-byte-1234")
	DisplayName string `json:"display_name,omitempty"` // User-provided name (e.g., "Research Wallet")
	APIKey      string `json:"api_key"`                // API key for authentication
	PublicKey   string `json:"public_key"`             // Solana public key (deposit address)
	SeedFile    string `json:"seed_file"`              // Relative path to seed file (e.g., "seeds/my-bot.seed")
	CreatedAt   string `json:"created_at"`             // ISO timestamp of creation
}

// Config represents the CLI configuration (V2 with multi-wallet support)
type Config struct {
	Version       int                    `json:"version"`                  // Config version (2 for multi-wallet)
	DefaultWallet string                 `json:"default_wallet,omitempty"` // Local name of default wallet
	Wallets       map[string]WalletEntry `json:"wallets"`                  // Wallets keyed by local name
	BaseURL       string                 `json:"base_url,omitempty"`       // Custom API URL (for development)
}

// =============================================================================
// Path Helpers
// =============================================================================

// ConfigDirEnv names the environment variable that overrides the folder
// holding config.json and seeds/ (default: ~/.botwallet).
const ConfigDirEnv = "BOTWALLET_HOME"

// ErrNoHomeDir means there is no safe place to keep wallet keys.
var ErrNoHomeDir = errors.New("cannot find a home folder for Botwallet keys: set HOME, or set " +
	ConfigDirEnv + " to the absolute path of a private folder")

// ResolveConfigDir returns the absolute path of the folder that holds
// config.json and seeds/:
//  1. $BOTWALLET_HOME, which must be an absolute path
//  2. <home>/.botwallet, where home is $HOME ($USERPROFILE on Windows) or,
//     if that is unset, the home folder in the OS user database
//
// It never falls back to a relative path. Keys written relative to the
// current directory could end up committed inside an agent's project.
func ResolveConfigDir() (string, error) {
	if dir := os.Getenv(ConfigDirEnv); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("%s must be an absolute path, got %q", ConfigDirEnv, dir)
		}
		return filepath.Clean(dir), nil
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.IsAbs(home) {
		return filepath.Join(home, ".botwallet"), nil
	}
	if home := userDBHome(); filepath.IsAbs(home) {
		return filepath.Join(home, ".botwallet"), nil
	}
	return "", ErrNoHomeDir
}

// userDBHome returns the current user's home folder from the OS user
// database (/etc/passwd on Unix, also without cgo), or "". A variable so
// tests can simulate a machine without one.
var userDBHome = func() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return u.HomeDir
}

// ConfigDir returns the configuration directory path, or "" if it cannot be
// resolved. Every function here that reads or writes files checks
// ResolveConfigDir first, so "" never becomes a relative path on disk.
func ConfigDir() string {
	dir, err := ResolveConfigDir()
	if err != nil {
		return ""
	}
	return dir
}

// ConfigPath returns the configuration file path
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

// SeedsDir returns the seeds directory path
func SeedsDir() string {
	return filepath.Join(ConfigDir(), "seeds")
}

// SeedPath returns the path for a wallet's seed file
func SeedPath(localName string) string {
	// Sanitize the local name for filesystem
	safeName := sanitizeFilename(localName)
	return filepath.Join(SeedsDir(), safeName+".seed")
}

// sanitizeFilename removes or replaces characters unsafe for filenames
func sanitizeFilename(name string) string {
	// Convert to lowercase and replace spaces with hyphens
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "-")

	// Keep only alphanumeric and hyphens
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}

	// Collapse multiple hyphens
	cleaned := result.String()
	for strings.Contains(cleaned, "--") {
		cleaned = strings.ReplaceAll(cleaned, "--", "-")
	}

	// Trim leading/trailing hyphens
	cleaned = strings.Trim(cleaned, "-")

	// Ensure non-empty
	if cleaned == "" {
		cleaned = "wallet"
	}

	return cleaned
}

// =============================================================================
// Config Loading & Saving
// =============================================================================

// LoadConfig loads configuration from the config file
func LoadConfig() (*Config, error) {
	if _, err := ResolveConfigDir(); err != nil {
		return nil, err
	}
	path := ConfigPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{
				Version: 2,
				Wallets: make(map[string]WalletEntry),
			}, nil
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	if config.Wallets == nil {
		config.Wallets = make(map[string]WalletEntry)
	}
	return &config, nil
}

// SaveConfig saves configuration to the config file with secure permissions.
// The file is replaced atomically (see lock.go). Callers that load, modify
// and save must hold the config lock (withConfigLock) so parallel commands
// don't drop each other's changes.
func SaveConfig(config *Config) error {
	dir, err := ResolveConfigDir()
	if err != nil {
		return err
	}

	// Create directory if it doesn't exist (0700 = owner only)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Ensure version is set
	if config.Version == 0 {
		config.Version = 2
	}
	if config.Wallets == nil {
		config.Wallets = make(map[string]WalletEntry)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Temp file + rename, 0600 (owner read/write only)
	if err := writeFileAtomic(ConfigPath(), data); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// =============================================================================
// Seed File Management
// =============================================================================

// SaveSeed writes a key share (S1) to seeds/<localName>.seed.
// It never replaces an existing file: Key 1 exists only in its seed file, so
// a file that is already there belongs to some wallet even if config.json no
// longer lists it. In that case the returned error wraps fs.ErrExist.
// IMPORTANT: Never prints key share to stdout.
func SaveSeed(localName string, seedPhrase string) (string, error) {
	return saveSeed(localName, seedPhrase, "")
}

// saveSeed is SaveSeed with the wallet's deposit address (if known) written
// into the file header, so a seed file can be matched to its wallet.
func saveSeed(localName, seedPhrase, depositAddress string) (string, error) {
	if _, err := ResolveConfigDir(); err != nil {
		return "", err
	}

	// Ensure seeds directory exists (0700 = owner only)
	if err := os.MkdirAll(SeedsDir(), 0700); err != nil {
		return "", fmt.Errorf("failed to create seeds directory: %w", err)
	}

	seedPath := SeedPath(localName)

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

// writeNewFile creates path with 0600 permissions and writes data to disk
// (fsync). It fails if path already exists. If anything fails after the file
// was created, the file is removed again: this call created it, so it holds
// nothing else.
func writeNewFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// LoadSeed loads a key share from file.
// IMPORTANT: Caller must handle securely, never print to stdout.
func LoadSeed(localName string) (string, error) {
	if _, err := ResolveConfigDir(); err != nil {
		return "", err
	}
	seedPath := SeedPath(localName)
	return LoadSeedFromPath(seedPath)
}

// WalletSeedPath returns the absolute path of a local wallet's Key 1 file.
// seed_file in config.json is relative to the config folder
// ("seeds/<name>.seed"). Older MCP versions wrote just "<name>.seed" while
// saving the file in seeds/, so a bare file name that does not exist next to
// config.json is looked up in seeds/.
func WalletSeedPath(localName string, entry WalletEntry) string {
	if entry.SeedFile == "" {
		return SeedPath(localName)
	}
	path := filepath.Join(ConfigDir(), filepath.FromSlash(entry.SeedFile))
	if filepath.Base(entry.SeedFile) == entry.SeedFile {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			inSeeds := filepath.Join(SeedsDir(), entry.SeedFile)
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
	cfg, err := LoadConfig()
	if err != nil {
		return "", err
	}
	if entry, ok := cfg.Wallets[localName]; ok {
		return LoadSeedFromPath(WalletSeedPath(localName, entry))
	}
	return LoadSeed(localName)
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

// =============================================================================
// Wallet Management
// =============================================================================

// SavedWallet describes a wallet just added to the local config.
type SavedWallet struct {
	LocalName       string // key in config.json; carries a -N suffix if the requested name was taken
	SeedPath        string // absolute path of the Key 1 file
	PreviousDefault string // previous default wallet ("" if this is the first)
	TotalWallets    int    // number of wallets after adding
}

// AddWalletWithInfo saves Key 1 and adds the wallet to config.json as the
// default, under the first free local name derived from name (name, name-2,
// name-3, ...). It never replaces another wallet's seed file. If config.json
// cannot be updated, the seed file is kept and the error names it: it may be
// the only copy of Key 1.
func AddWalletWithInfo(name, username, displayName, apiKey, publicKey, seedPhrase string) (SavedWallet, error) {
	var saved SavedWallet
	err := withConfigLock(func() error {
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}
		localName, seedPath, err := saveNewSeedLocked(cfg, name, seedPhrase, publicKey)
		if err != nil {
			return err
		}
		saved, err = registerWalletLocked(cfg, localName, username, displayName, apiKey, publicKey)
		if err != nil {
			saved = SavedWallet{LocalName: localName, SeedPath: seedPath}
			return fmt.Errorf("%w (Key 1 was saved to %s and has been kept)", err, seedPath)
		}
		return nil
	})
	return saved, err
}

// saveNewSeedLocked writes Key 1 under the first free local name derived
// from name. The caller holds the config lock and passes the loaded config.
func saveNewSeedLocked(cfg *Config, name, seedPhrase, depositAddress string) (localName, seedPath string, err error) {
	// Writers that don't take the lock (older CLI or MCP versions) can claim
	// a name between the check and the write; O_EXCL catches that, and the
	// next free name is tried.
	for attempt := 0; attempt < 5; attempt++ {
		localName = freeLocalName(cfg, name)
		seedPath, err = saveSeed(localName, seedPhrase, depositAddress)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		return localName, seedPath, nil
	}
	return "", "", err
}

// registerWalletLocked adds a wallet whose seed file is seeds/<localName>.seed
// to cfg, makes it the default and saves cfg. The caller holds the config
// lock. If localName is already a config entry, the next free name is used
// for the entry; seed_file still points at the wallet's own seed file.
func registerWalletLocked(cfg *Config, localName, username, displayName, apiKey, publicKey string) (SavedWallet, error) {
	seedFile := "seeds/" + sanitizeFilename(localName) + ".seed"
	key := localName
	if _, exists := cfg.Wallets[key]; exists {
		key = freeLocalName(cfg, localName)
	}

	cfg.Wallets[key] = WalletEntry{
		Username:    username,
		DisplayName: displayName,
		APIKey:      apiKey,
		PublicKey:   publicKey,
		SeedFile:    seedFile,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	// Always set the newly registered wallet as default
	previousDefault := cfg.DefaultWallet
	if previousDefault == key {
		previousDefault = ""
	}
	cfg.DefaultWallet = key

	if err := SaveConfig(cfg); err != nil {
		// Never delete the seed file here: it may be the only copy of Key 1.
		return SavedWallet{}, err
	}

	return SavedWallet{
		LocalName:       key,
		SeedPath:        filepath.Join(ConfigDir(), filepath.FromSlash(seedFile)),
		PreviousDefault: previousDefault,
		TotalWallets:    len(cfg.Wallets),
	}, nil
}

// =============================================================================
// New Wallet Registration (wallet create)
// =============================================================================
// The server returns a new wallet's API key only once, and Key 1 is generated
// here and never leaves this machine. So wallet create stores things in this
// order, and never deletes a seed file whose wallet may exist on the server:
//   1. PrepareNewWallet  - before dkg_init: config readable, folders writable
//   2. SaveNewSeed       - before dkg_complete: Key 1 on disk (O_EXCL, fsync)
//   3. dkg_complete      - the server creates the wallet
//   4. RegisterWallet    - config.json entry with the API key
// If 3 is refused outright, DiscardNewSeed removes the unused seed. If its
// outcome is unknown, KeepUnregisteredSeed keeps it under a clear name. If 4
// fails, SavePendingWallet keeps the API key in a separate file.

// PrepareNewWallet checks, before a wallet is created on the server, that its
// keys can be stored here: config.json must be readable (or absent) and the
// config and seeds folders writable. Failing now costs nothing; failing after
// the server created the wallet leaves a wallet without its Key 1.
func PrepareNewWallet() error {
	if _, err := LoadConfig(); err != nil {
		return err
	}
	for _, dir := range []string{ConfigDir(), SeedsDir()} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("cannot create %s: %w", dir, err)
		}
		f, err := os.CreateTemp(dir, ".write-test-*")
		if err != nil {
			return fmt.Errorf("cannot write to %s: %w", dir, err)
		}
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
	}
	return nil
}

// SaveNewSeed writes Key 1 for a wallet that is about to be created, under
// the first free local name derived from name, and returns that name and the
// file's path. depositAddress is recorded in the file header.
func SaveNewSeed(name, seedPhrase, depositAddress string) (localName, seedPath string, err error) {
	err = withConfigLock(func() error {
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}
		localName, seedPath, err = saveNewSeedLocked(cfg, name, seedPhrase, depositAddress)
		return err
	})
	return localName, seedPath, err
}

// RegisterWallet adds a wallet whose Key 1 was saved by SaveNewSeed to
// config.json and makes it the default. If localName became a config entry
// in the meantime, the entry gets the next free name (see SavedWallet).
func RegisterWallet(localName, username, displayName, apiKey, publicKey string) (SavedWallet, error) {
	var saved SavedWallet
	err := withConfigLock(func() error {
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}
		saved, err = registerWalletLocked(cfg, localName, username, displayName, apiKey, publicKey)
		return err
	})
	return saved, err
}

// DiscardNewSeed removes a seed file written by SaveNewSeed. Only call it
// when the server clearly refused to create the wallet, so the Key 1 in it
// belongs to no wallet.
func DiscardNewSeed(seedPath string) error {
	return os.Remove(seedPath)
}

// KeepUnregisteredSeed is for a new wallet whose creation may or may not have
// happened on the server (timeout, network or server error). It renames the
// seed file to seeds/unregistered-<depositAddress>.seed, so the local name is
// free for the next attempt and the file says which wallet it belongs to.
// The file is never deleted: if the wallet exists, it holds the only copy of
// its Key 1. Returns the path the file ends up at.
func KeepUnregisteredSeed(seedPath, depositAddress string) string {
	target := filepath.Join(filepath.Dir(seedPath), "unregistered-"+depositAddress+".seed")
	if _, err := os.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
		return seedPath
	}
	if err := os.Rename(seedPath, target); err != nil {
		return seedPath
	}
	return target
}

// PendingWallet is a wallet the server created that could not be added to
// config.json. Its API key is shown only once, so it is saved to a separate
// file instead of being lost.
type PendingWallet struct {
	Note        string      `json:"note"`
	LocalName   string      `json:"local_name"`
	ClaimURL    string      `json:"claim_url,omitempty"`
	ClaimCode   string      `json:"claim_code,omitempty"`
	ConfigEntry WalletEntry `json:"config_entry"`
}

// SavePendingWallet writes p to <config dir>/pending-<username>-<random>.json
// (0600, never replacing a file) and returns the file's path.
func SavePendingWallet(p PendingWallet) (string, error) {
	dir, err := ResolveConfigDir()
	if err != nil {
		return "", err
	}
	if p.Note == "" {
		p.Note = "This wallet was created but could not be added to config.json. " +
			"Copy config_entry into the \"wallets\" object of config.json under the key local_name, then delete this file."
	}
	if p.ConfigEntry.CreatedAt == "" {
		p.ConfigEntry.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "pending-"+sanitizeFilename(p.ConfigEntry.Username)+"-*.json") // created 0600
	if err != nil {
		return "", err
	}
	path := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// UpdateWalletDisplayName updates only the display name of a wallet in the local config.
// The map key (localName) and seed file path are never changed.
func UpdateWalletDisplayName(localName, newDisplayName string) error {
	return withConfigLock(func() error {
		cfg, err := LoadConfig()
		if err != nil {
			return err
		}

		wallet, exists := cfg.Wallets[localName]
		if !exists {
			return fmt.Errorf("wallet '%s' not found", localName)
		}

		wallet.DisplayName = newDisplayName
		cfg.Wallets[localName] = wallet

		return SaveConfig(cfg)
	})
}

// GetWallet retrieves a wallet entry by local name
func GetWallet(localName string) (*WalletEntry, error) {
	config, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	wallet, exists := config.Wallets[localName]
	if !exists {
		return nil, fmt.Errorf("wallet '%s' not found", localName)
	}

	return &wallet, nil
}

// GetDefaultWallet returns the default wallet entry
func GetDefaultWallet() (*WalletEntry, string, error) {
	config, err := LoadConfig()
	if err != nil {
		return nil, "", err
	}

	if config.DefaultWallet == "" {
		return nil, "", fmt.Errorf("no default wallet set")
	}

	wallet, exists := config.Wallets[config.DefaultWallet]
	if !exists {
		return nil, "", fmt.Errorf("default wallet '%s' not found", config.DefaultWallet)
	}

	return &wallet, config.DefaultWallet, nil
}

// SetDefaultWallet sets the default wallet
func SetDefaultWallet(localName string) error {
	return withConfigLock(func() error {
		config, err := LoadConfig()
		if err != nil {
			return err
		}

		// Check wallet exists
		if _, exists := config.Wallets[localName]; !exists {
			return fmt.Errorf("wallet '%s' not found", localName)
		}

		config.DefaultWallet = localName
		return SaveConfig(config)
	})
}

// ListWallets returns all wallets sorted by name
func ListWallets() ([]struct {
	LocalName string
	Entry     WalletEntry
	IsDefault bool
}, error) {
	config, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	// Sort by local name
	names := make([]string, 0, len(config.Wallets))
	for name := range config.Wallets {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]struct {
		LocalName string
		Entry     WalletEntry
		IsDefault bool
	}, len(names))

	for i, name := range names {
		result[i].LocalName = name
		result[i].Entry = config.Wallets[name]
		result[i].IsDefault = name == config.DefaultWallet
	}

	return result, nil
}

// RemoveWallet removes a wallet from config (does NOT delete seed file for safety)
func RemoveWallet(localName string) error {
	return withConfigLock(func() error {
		config, err := LoadConfig()
		if err != nil {
			return err
		}

		if _, exists := config.Wallets[localName]; !exists {
			return fmt.Errorf("wallet '%s' not found", localName)
		}

		delete(config.Wallets, localName)

		// Clear default if it was this wallet
		if config.DefaultWallet == localName {
			config.DefaultWallet = ""
			// Set first remaining wallet as default
			for name := range config.Wallets {
				config.DefaultWallet = name
				break
			}
		}

		return SaveConfig(config)
	})
}

// GenerateLocalName returns a local name for a wallet that is not in use:
// the sanitized display name, or the same with a -2, -3, ... suffix.
func GenerateLocalName(displayName string) string {
	cfg, err := LoadConfig()
	if err != nil {
		cfg = &Config{Wallets: make(map[string]WalletEntry)}
	}
	return freeLocalName(cfg, displayName)
}

// freeLocalName returns sanitize(name), or the same with a -2, -3, ...
// suffix, choosing the first that is neither a config entry nor an existing
// seed file. A seed file without a config entry still holds some wallet's
// Key 1, so its name is never reused.
func freeLocalName(cfg *Config, name string) string {
	baseName := sanitizeFilename(name)
	if !localNameTaken(cfg, baseName) {
		return baseName
	}

	// Add numeric suffix
	for i := 2; i <= 100; i++ {
		candidate := fmt.Sprintf("%s-%d", baseName, i)
		if !localNameTaken(cfg, candidate) {
			return candidate
		}
	}

	// Fallback to timestamp
	return fmt.Sprintf("%s-%d", baseName, time.Now().UnixNano())
}

// localNameTaken reports whether localName is a config entry or has a seed
// file on disk. Anything but a clear "does not exist" counts as taken.
func localNameTaken(cfg *Config, localName string) bool {
	if _, exists := cfg.Wallets[localName]; exists {
		return true
	}
	_, err := os.Lstat(SeedPath(localName))
	return !errors.Is(err, fs.ErrNotExist)
}

// =============================================================================
// Wallet Resolution: which wallet a command acts as
// =============================================================================
// A command acts as exactly one wallet. Its API key (the wallet the server
// sees) and its Key 1 file (the wallet that signs) must belong to the same
// wallet, or the CLI could ask the server about one wallet and sign with
// another wallet's key. Rules (the MCP server should follow the same):
//
//  1. The API key comes from, in order: --api-key, BOTWALLET_API_KEY,
//     BW_API_KEY, --wallet, the default wallet.
//  2. If --wallet is set and a key also comes from --api-key or an env var,
//     the key must be that wallet's api_key. Otherwise the command stops
//     with WalletConflictError instead of quietly picking one.
//  3. A key from --api-key or an env var selects the local wallet whose
//     api_key equals it. If none does, server calls still work, but anything
//     that needs Key 1 (signing, export, backup) stops. It never falls back
//     to the default wallet's Key 1.

// ErrNoWallet means no wallet is configured on this machine.
var ErrNoWallet = errors.New("no wallet configured")

// WalletNotFoundError means --wallet names a wallet that is not in config.json.
type WalletNotFoundError struct {
	Name      string
	Available []string
}

func (e *WalletNotFoundError) Error() string {
	return fmt.Sprintf("wallet '%s' not found. Available wallets: %v", e.Name, e.Available)
}

// WalletConflictError means --wallet and an API key from --api-key or an
// environment variable point at different wallets.
type WalletConflictError struct {
	Wallet    string // the --wallet value
	KeySource string // "--api-key", "BOTWALLET_API_KEY" or "BW_API_KEY"
	KeyWallet string // local wallet that owns the key, "" if none
}

func (e *WalletConflictError) Error() string {
	owner := "a wallet that is not in config.json"
	if e.KeyWallet != "" {
		owner = fmt.Sprintf("wallet '%s'", e.KeyWallet)
	}
	return fmt.Sprintf("--wallet %s does not match the API key from %s, which belongs to %s", e.Wallet, e.KeySource, owner)
}

// NoLocalWalletError means the API key in use (from --api-key or an
// environment variable) belongs to no wallet in config.json, so this
// machine has no Key 1 for it.
type NoLocalWalletError struct {
	KeySource string
}

func (e *NoLocalWalletError) Error() string {
	return fmt.Sprintf("the API key from %s does not belong to any wallet in %s, so Key 1 for it is not on this machine", e.KeySource, ConfigPath())
}

// ResolvedWallet is the wallet a command acts as.
type ResolvedWallet struct {
	APIKey    string       // "" if no key is configured anywhere
	KeySource string       // "--api-key", "BOTWALLET_API_KEY", "BW_API_KEY", "--wallet" or "default wallet"
	LocalName string       // local wallet name; "" if no local wallet has APIKey
	Entry     *WalletEntry // nil if no local wallet has APIKey
	ConfigErr error        // set if config.json could not be read
}

// RequireLocal returns an error unless the wallet's Key 1 is configured on
// this machine.
func (r *ResolvedWallet) RequireLocal() error {
	switch {
	case r.Entry != nil:
		return nil
	case r.ConfigErr != nil:
		return r.ConfigErr
	case r.KeySource != "" && r.KeySource != "default wallet" && r.KeySource != "--wallet":
		return &NoLocalWalletError{KeySource: r.KeySource}
	default:
		return ErrNoWallet
	}
}

// SeedPath returns the path of the wallet's Key 1 file. Call RequireLocal first.
func (r *ResolvedWallet) SeedPath() string {
	return WalletSeedPath(r.LocalName, *r.Entry)
}

// ResolveWallet decides which wallet a command acts as; see the rules above.
// apiKeyFlag and walletFlag are the --api-key and --wallet values.
func ResolveWallet(apiKeyFlag, walletFlag string) (*ResolvedWallet, error) {
	key, source := explicitAPIKey(apiKeyFlag)

	if walletFlag != "" {
		cfg, err := LoadConfig()
		if err != nil {
			return nil, err
		}
		entry, ok := cfg.Wallets[walletFlag]
		if !ok {
			return nil, &WalletNotFoundError{Name: walletFlag, Available: walletNames(cfg)}
		}
		if key != "" && key != entry.APIKey {
			return nil, &WalletConflictError{Wallet: walletFlag, KeySource: source, KeyWallet: walletForAPIKey(cfg, key)}
		}
		return &ResolvedWallet{APIKey: entry.APIKey, KeySource: "--wallet", LocalName: walletFlag, Entry: &entry}, nil
	}

	cfg, cfgErr := LoadConfig()

	if key != "" {
		// Server calls work with the key alone, so a config problem only
		// matters to commands that need Key 1 (see RequireLocal).
		r := &ResolvedWallet{APIKey: key, KeySource: source, ConfigErr: cfgErr}
		if cfgErr == nil {
			if name := walletForAPIKey(cfg, key); name != "" {
				entry := cfg.Wallets[name]
				r.LocalName, r.Entry = name, &entry
			}
		}
		return r, nil
	}

	if cfgErr != nil {
		return &ResolvedWallet{ConfigErr: cfgErr}, nil
	}
	entry, ok := cfg.Wallets[cfg.DefaultWallet]
	if cfg.DefaultWallet == "" || !ok {
		return &ResolvedWallet{}, nil
	}
	return &ResolvedWallet{APIKey: entry.APIKey, KeySource: "default wallet", LocalName: cfg.DefaultWallet, Entry: &entry}, nil
}

// explicitAPIKey returns a key given by flag or environment, and its source.
func explicitAPIKey(apiKeyFlag string) (key, source string) {
	if apiKeyFlag != "" {
		return apiKeyFlag, "--api-key"
	}
	for _, name := range []string{"BOTWALLET_API_KEY", "BW_API_KEY"} {
		if key := os.Getenv(name); key != "" {
			return key, name
		}
	}
	return "", ""
}

// walletForAPIKey returns the local wallet whose api_key is key, preferring
// the default wallet, or "" if there is none.
func walletForAPIKey(cfg *Config, key string) string {
	if entry, ok := cfg.Wallets[cfg.DefaultWallet]; ok && entry.APIKey == key {
		return cfg.DefaultWallet
	}
	for _, name := range walletNames(cfg) {
		if cfg.Wallets[name].APIKey == key {
			return name
		}
	}
	return ""
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

// GetBaseURL retrieves the base URL from available sources
func GetBaseURL(flagValue string) string {
	// 1. Flag value
	if flagValue != "" {
		return flagValue
	}

	// 2. Environment variable
	if url := os.Getenv("BOTWALLET_API_URL"); url != "" {
		return url
	}

	// 3. Config file
	config, err := LoadConfig()
	if err == nil && config.BaseURL != "" {
		return config.BaseURL
	}

	return ""
}

// RedactAPIKey redacts an API key for safe display
func RedactAPIKey(apiKey string) string {
	if len(apiKey) <= 12 {
		return "***"
	}
	return apiKey[:12] + "****" + apiKey[len(apiKey)-4:]
}

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

// BackupNoncePath returns the path for the backup nonce file
func BackupNoncePath() string {
	return filepath.Join(ConfigDir(), ".backup-nonce")
}

// WriteBackupNonce creates a new backup nonce and saves it
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
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	return os.WriteFile(BackupNoncePath(), data, 0600)
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
	if _, err := ResolveConfigDir(); err != nil {
		return "", err
	}
	path := BackupNoncePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
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
			now := time.Now().UnixNano()
			b[i] = chars[(now>>uint(i*8))%int64(len(chars))]
			continue
		}
		b[i] = chars[n.Int64()]
	}
	return string(b)
}
