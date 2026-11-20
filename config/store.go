package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// =============================================================================
// Config Structures
// =============================================================================

// configVersion is the config.json format version written: 2 is the
// multi-wallet format.
const configVersion = 2

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
	Version       int                    `json:"version"`                  // Config version (configVersion)
	DefaultWallet string                 `json:"default_wallet,omitempty"` // Local name of default wallet
	Wallets       map[string]WalletEntry `json:"wallets"`                  // Wallets keyed by local name
	BaseURL       string                 `json:"base_url,omitempty"`       // Custom API URL (for development)
}

// =============================================================================
// Config Loading & Saving
// =============================================================================

// LoadConfig loads configuration from the config file
func LoadConfig() (*Config, error) {
	cfg, _, err := loadConfig()
	return cfg, err
}

// loadConfig is LoadConfig that also returns the folder it read from.
func loadConfig() (*Config, Dir, error) {
	dir, err := ResolveConfigDir()
	if err != nil {
		return nil, "", err
	}
	cfg, err := loadConfigFrom(dir)
	return cfg, dir, err
}

// loadConfigFrom reads config.json in dir. No file is an empty config.
func loadConfigFrom(dir Dir) (*Config, error) {
	data, err := os.ReadFile(dir.ConfigFile())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Config{
				Version: configVersion,
				Wallets: make(map[string]WalletEntry),
			}, nil
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	if cfg.Wallets == nil {
		cfg.Wallets = make(map[string]WalletEntry)
	}
	return &cfg, nil
}

// saveConfig saves cfg to config.json in dir with secure permissions.
// The file is replaced atomically (see lock.go). Callers that load, modify
// and save must hold the config lock (withConfigLock) so parallel commands
// don't drop each other's changes.
func saveConfig(dir Dir, cfg *Config) error {
	// Create directory if it doesn't exist (0700 = owner only)
	if err := os.MkdirAll(string(dir), 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Ensure version is set
	if cfg.Version == 0 {
		cfg.Version = configVersion
	}
	if cfg.Wallets == nil {
		cfg.Wallets = make(map[string]WalletEntry)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Temp file + rename, 0600 (owner read/write only)
	if err := writeFileAtomic(dir.ConfigFile(), data); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}
