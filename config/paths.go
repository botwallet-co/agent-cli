package config

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// =============================================================================
// Path Helpers
// =============================================================================

// ConfigDirEnv names the environment variable that overrides the folder
// holding config.json and seeds/ (default: ~/.botwallet).
const ConfigDirEnv = "BOTWALLET_HOME"

// ErrNoHomeDir means there is no safe place to keep wallet keys.
var ErrNoHomeDir = errors.New("cannot find a home folder for Botwallet keys: set HOME, or set " +
	ConfigDirEnv + " to the absolute path of a private folder")

// Dir is the folder that holds config.json and seeds/, as ResolveConfigDir
// found it: always an absolute path. Every file this package reads or
// writes is named through a Dir, so no path is built from an unresolved "".
type Dir string

// ConfigFile is the path of config.json.
func (d Dir) ConfigFile() string {
	return filepath.Join(string(d), "config.json")
}

// SeedsDir is the folder of the Key 1 (seed) files.
func (d Dir) SeedsDir() string {
	return filepath.Join(string(d), "seeds")
}

// SeedFile is the path of the seed file of the wallet named localName,
// seeds/<name>.seed with the name made safe for a file name.
func (d Dir) SeedFile(localName string) string {
	return filepath.Join(d.SeedsDir(), sanitizeFilename(localName)+".seed")
}

// BackupNonceFile is the path of the pending 'wallet backup' code.
func (d Dir) BackupNonceFile() string {
	return filepath.Join(string(d), ".backup-nonce")
}

// lockFile is the path of the config lock (lock.go).
func (d Dir) lockFile() string {
	return filepath.Join(string(d), lockFileName)
}

// ResolveConfigDir returns the absolute path of the folder that holds
// config.json and seeds/:
//  1. $BOTWALLET_HOME, which must be an absolute path
//  2. <home>/.botwallet, where home is $HOME ($USERPROFILE on Windows) or,
//     if that is unset, the home folder in the OS user database
//
// It never falls back to a relative path. Keys written relative to the
// current directory could end up committed inside an agent's project.
func ResolveConfigDir() (Dir, error) {
	if dir := os.Getenv(ConfigDirEnv); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("%s must be an absolute path, got %q", ConfigDirEnv, dir)
		}
		return Dir(filepath.Clean(dir)), nil
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.IsAbs(home) {
		return Dir(filepath.Join(home, ".botwallet")), nil
	}
	if home := userDBHome(); filepath.IsAbs(home) {
		return Dir(filepath.Join(home, ".botwallet")), nil
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

// ConfigDir returns the configuration directory path for messages, or ""
// if it cannot be resolved. Files are only read and written through the
// Dir from ResolveConfigDir.
func ConfigDir() string {
	dir, err := ResolveConfigDir()
	if err != nil {
		return ""
	}
	return string(dir)
}

// ConfigPath returns the configuration file path, for messages.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

// SeedsDir returns the seeds directory path, for messages.
func SeedsDir() string {
	return filepath.Join(ConfigDir(), "seeds")
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
