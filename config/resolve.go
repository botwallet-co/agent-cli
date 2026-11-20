package config

import (
	"errors"
	"fmt"
	"os"
)

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

	dir Dir // the config folder Entry was read from
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
	return walletSeedPath(r.dir, r.LocalName, *r.Entry)
}

// ResolveWallet decides which wallet a command acts as; see the rules above.
// apiKeyFlag and walletFlag are the --api-key and --wallet values.
func ResolveWallet(apiKeyFlag, walletFlag string) (*ResolvedWallet, error) {
	key, source := explicitAPIKey(apiKeyFlag)

	if walletFlag != "" {
		cfg, dir, err := loadConfig()
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
		return &ResolvedWallet{APIKey: entry.APIKey, KeySource: "--wallet", LocalName: walletFlag, Entry: &entry, dir: dir}, nil
	}

	cfg, dir, cfgErr := loadConfig()

	if key != "" {
		// Server calls work with the key alone, so a config problem only
		// matters to commands that need Key 1 (see RequireLocal).
		r := &ResolvedWallet{APIKey: key, KeySource: source, ConfigErr: cfgErr}
		if cfgErr == nil {
			if name := walletForAPIKey(cfg, key); name != "" {
				entry := cfg.Wallets[name]
				r.LocalName, r.Entry, r.dir = name, &entry, dir
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
	return &ResolvedWallet{APIKey: entry.APIKey, KeySource: "default wallet", LocalName: cfg.DefaultWallet, Entry: &entry, dir: dir}, nil
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

// GetBaseURL returns the API base URL to use, or "" for the default
// (api.DefaultBaseURL). Sources, in order:
//  1. the --api-url flag (flagValue)
//  2. the BOTWALLET_API_URL environment variable
//  3. base_url in config.json
func GetBaseURL(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}

	if url := os.Getenv("BOTWALLET_API_URL"); url != "" {
		return url
	}

	cfg, err := LoadConfig()
	if err == nil && cfg.BaseURL != "" {
		return cfg.BaseURL
	}

	return ""
}
