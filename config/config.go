// =============================================================================
// Botwallet CLI Configuration - Multi-Wallet Support
// =============================================================================
// Handles secure storage of multiple wallet credentials:
// - API keys stored in config.json (0600 permissions)
// - Key shares (S1) stored separately in seeds/ folder (0600 permissions)
// - Key shares are NEVER printed to stdout during normal operation
//
// Directory structure:
//   ~/.botwallet/
//   ├── config.json          # Wallet registry + API keys (0600)
//   ├── .backup-nonce        # Temporary backup nonce (auto-deleted)
//   └── seeds/               # Key share folder (0700)
//       ├── my-bot.seed      # Individual key share files (0600)
//       └── test-bot.seed
//
// API Key Priority:
// 1. --api-key flag (highest priority)
// 2. BOTWALLET_API_KEY environment variable
// 3. --wallet flag (selects wallet from config)
// 4. Default wallet from config file
// =============================================================================

package config

import (
	cryptoRand "crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
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

// ConfigDir returns the configuration directory path
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".botwallet"
	}
	return filepath.Join(home, ".botwallet")
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

// SaveConfig saves configuration to the config file with secure permissions
func SaveConfig(config *Config) error {
	dir := ConfigDir()

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

	path := ConfigPath()
	// Write with 0600 permissions (owner read/write only)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// =============================================================================
// Seed File Management
// =============================================================================

// SaveSeed saves a key share (S1) to a secure file.
// IMPORTANT: Never prints key share to stdout.
func SaveSeed(localName string, seedPhrase string) (string, error) {
	// Ensure seeds directory exists (0700 = owner only)
	if err := os.MkdirAll(SeedsDir(), 0700); err != nil {
		return "", fmt.Errorf("failed to create seeds directory: %w", err)
	}

	seedPath := SeedPath(localName)

	// Format seed file with warnings
	content := fmt.Sprintf(`# Botwallet Key Share (S1)
# Wallet: %s
#
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
`, localName, seedPhrase)

	// Write with 0600 permissions (owner read/write only)
	if err := os.WriteFile(seedPath, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("failed to write seed file: %w", err)
	}

	return seedPath, nil
}

// LoadSeed loads a key share from file.
// IMPORTANT: Caller must handle securely, never print to stdout.
func LoadSeed(localName string) (string, error) {
	seedPath := SeedPath(localName)
	return LoadSeedFromPath(seedPath)
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

// AddWallet adds a new wallet to the config
func AddWallet(localName, username, displayName, apiKey, publicKey, seedPhrase string) error {
	_, _, err := AddWalletWithInfo(localName, username, displayName, apiKey, publicKey, seedPhrase)
	return err
}

// AddWalletWithInfo adds a new wallet, sets it as the default, and returns status info:
// - previousDefault: name of the previous default wallet ("" if this is the first)
// - totalWallets: total number of wallets after adding
func AddWalletWithInfo(localName, username, displayName, apiKey, publicKey, seedPhrase string) (previousDefault string, totalWallets int, err error) {
	config, err := LoadConfig()
	if err != nil {
		return "", 0, err
	}

	// Sanitize local name
	localName = sanitizeFilename(localName)

	if _, exists := config.Wallets[localName]; exists {
		return "", 0, fmt.Errorf("wallet '%s' already exists locally", localName)
	}

	seedPath, err := SaveSeed(localName, seedPhrase)
	if err != nil {
		return "", 0, err
	}

	config.Wallets[localName] = WalletEntry{
		Username:    username,
		DisplayName: displayName,
		APIKey:      apiKey,
		PublicKey:   publicKey,
		SeedFile:    "seeds/" + localName + ".seed",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	// Always set the newly registered wallet as default
	previousDefault = config.DefaultWallet
	if previousDefault == localName {
		previousDefault = ""
	}
	config.DefaultWallet = localName

	if err := SaveConfig(config); err != nil {
		// Try to clean up seed file on failure
		_ = os.Remove(seedPath)
		return "", 0, err
	}

	return previousDefault, len(config.Wallets), nil
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
}

// GenerateLocalName creates a unique local name for a wallet
func GenerateLocalName(displayName string) string {
	config, _ := LoadConfig()

	baseName := sanitizeFilename(displayName)
	if baseName == "" {
		baseName = "wallet"
	}

	// Try base name first
	if config == nil || config.Wallets == nil {
		return baseName
	}

	if _, exists := config.Wallets[baseName]; !exists {
		return baseName
	}

	// Add numeric suffix
	for i := 2; i <= 100; i++ {
		name := fmt.Sprintf("%s-%d", baseName, i)
		if _, exists := config.Wallets[name]; !exists {
			return name
		}
	}

	// Fallback to timestamp
	return fmt.Sprintf("%s-%d", baseName, time.Now().Unix())
}

// =============================================================================
// API Key Resolution (with multi-wallet support)
// =============================================================================

// GetAPIKey retrieves the API key from available sources (priority order)
// 1. Explicit flag value (passed as parameter)
// 2. BOTWALLET_API_KEY environment variable
// 3. BW_API_KEY environment variable
// 4. Specified wallet (via walletFlag)
// 5. Default wallet from config
func GetAPIKey(flagValue string) string {
	key, _ := GetAPIKeyWithWallet(flagValue, "")
	return key
}

// GetAPIKeyWithWallet retrieves API key with wallet selection support.
// Returns an error if --wallet was explicitly set but not found locally.
func GetAPIKeyWithWallet(flagValue, walletFlag string) (string, error) {
	// 1. Flag value (highest priority)
	if flagValue != "" {
		return flagValue, nil
	}

	// 2. BOTWALLET_API_KEY environment variable
	if key := os.Getenv("BOTWALLET_API_KEY"); key != "" {
		return key, nil
	}

	// 3. BW_API_KEY environment variable (alias)
	if key := os.Getenv("BW_API_KEY"); key != "" {
		return key, nil
	}

	// 4. Specified wallet — fail hard if it doesn't exist
	if walletFlag != "" {
		wallet, err := GetWallet(walletFlag)
		if err != nil {
			available, _ := ListWallets()
			var names []string
			for _, w := range available {
				names = append(names, w.LocalName)
			}
			return "", fmt.Errorf("wallet '%s' not found. Available wallets: %v", walletFlag, names)
		}
		return wallet.APIKey, nil
	}

	// 5. Default wallet from config
	if wallet, _, err := GetDefaultWallet(); err == nil {
		return wallet.APIKey, nil
	}

	return "", nil
}

// GetCurrentWallet returns the wallet entry based on wallet flag or default
// Returns the wallet entry, local name, and an error if not found
func GetCurrentWallet(walletFlag string) (*WalletEntry, string, error) {
	// If wallet flag specified, use that wallet
	if walletFlag != "" {
		wallet, err := GetWallet(walletFlag)
		if err != nil {
			return nil, "", fmt.Errorf("wallet '%s' not found", walletFlag)
		}
		return wallet, walletFlag, nil
	}

	// Otherwise use default wallet
	wallet, name, err := GetDefaultWallet()
	if err != nil {
		return nil, "", fmt.Errorf("no wallet configured")
	}
	return wallet, name, nil
}

// GetCurrentWalletSeedPath returns the seed file path for the current wallet
func GetCurrentWalletSeedPath(walletFlag string) (string, error) {
	wallet, localName, err := GetCurrentWallet(walletFlag)
	if err != nil {
		return "", err
	}

	// If wallet has a seed file path, use it
	if wallet.SeedFile != "" {
		// SeedFile is relative to config dir (e.g., "seeds/my-bot.seed")
		return filepath.Join(ConfigDir(), wallet.SeedFile), nil
	}

	// Fallback: try the standard seed path
	return SeedPath(localName), nil
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

	if err := os.MkdirAll(ConfigDir(), 0700); err != nil {
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

	// Check expiry first (30 seconds) — expired nonces are always deleted
	createdAt, err := time.Parse(time.RFC3339, nonce.CreatedAt)
	if err != nil {
		os.Remove(path)
		return "", fmt.Errorf("invalid nonce timestamp. Run 'botwallet wallet backup' again")
	}

	if time.Since(createdAt) > 30*time.Second {
		os.Remove(path) // Expired — force re-generation
		return "", fmt.Errorf("confirmation code expired (30 second limit). Run 'botwallet wallet backup' again")
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
