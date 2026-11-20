package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// =============================================================================
// Adding a Wallet
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
	err := withConfigLock(func(dir Dir) error {
		cfg, err := loadConfigFrom(dir)
		if err != nil {
			return err
		}
		localName, seedPath, err := saveNewSeedLocked(dir, cfg, name, seedPhrase, publicKey)
		if err != nil {
			return err
		}
		saved, err = registerWalletLocked(dir, cfg, localName, username, displayName, apiKey, publicKey)
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
func saveNewSeedLocked(dir Dir, cfg *Config, name, seedPhrase, depositAddress string) (localName, seedPath string, err error) {
	// Writers that don't take the lock (older CLI or MCP versions) can claim
	// a name between the check and the write; O_EXCL catches that, and the
	// next free name is tried.
	for attempt := 0; attempt < 5; attempt++ {
		localName = freeLocalName(dir, cfg, name)
		seedPath, err = saveSeed(dir, localName, seedPhrase, depositAddress)
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
func registerWalletLocked(dir Dir, cfg *Config, localName, username, displayName, apiKey, publicKey string) (SavedWallet, error) {
	seedFile := "seeds/" + sanitizeFilename(localName) + ".seed"
	key := localName
	if _, exists := cfg.Wallets[key]; exists {
		key = freeLocalName(dir, cfg, localName)
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

	if err := saveConfig(dir, cfg); err != nil {
		// Never delete the seed file here: it may be the only copy of Key 1.
		return SavedWallet{}, err
	}

	return SavedWallet{
		LocalName:       key,
		SeedPath:        filepath.Join(string(dir), filepath.FromSlash(seedFile)),
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
	_, dir, err := loadConfig()
	if err != nil {
		return err
	}
	for _, d := range []string{string(dir), dir.SeedsDir()} {
		if err := os.MkdirAll(d, 0700); err != nil {
			return fmt.Errorf("cannot create %s: %w", d, err)
		}
		f, err := os.CreateTemp(d, ".write-test-*")
		if err != nil {
			return fmt.Errorf("cannot write to %s: %w", d, err)
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
	err = withConfigLock(func(dir Dir) error {
		cfg, err := loadConfigFrom(dir)
		if err != nil {
			return err
		}
		localName, seedPath, err = saveNewSeedLocked(dir, cfg, name, seedPhrase, depositAddress)
		return err
	})
	return localName, seedPath, err
}

// RegisterWallet adds a wallet whose Key 1 was saved by SaveNewSeed to
// config.json and makes it the default. If localName became a config entry
// in the meantime, the entry gets the next free name (see SavedWallet).
func RegisterWallet(localName, username, displayName, apiKey, publicKey string) (SavedWallet, error) {
	var saved SavedWallet
	err := withConfigLock(func(dir Dir) error {
		cfg, err := loadConfigFrom(dir)
		if err != nil {
			return err
		}
		saved, err = registerWalletLocked(dir, cfg, localName, username, displayName, apiKey, publicKey)
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

	if err := os.MkdirAll(string(dir), 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(string(dir), "pending-"+sanitizeFilename(p.ConfigEntry.Username)+"-*.json") // created 0600
	if err != nil {
		return "", err
	}
	path := f.Name()
	if err := writeSyncClose(f, data); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
