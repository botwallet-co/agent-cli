// =============================================================================
// Config Lock and Atomic Writes
// =============================================================================
// config.json is shared by every botwallet process on the machine and by the
// MCP server. Agents often run commands in parallel, so every
// load-modify-save of config.json runs under a lock file, and the file is
// replaced atomically so a crash or full disk never leaves it half-written.
//
// Lock protocol (the MCP server must follow the same one):
//   - The lock is <config dir>/config.lock, created with O_CREATE|O_EXCL
//     (Node: fs.openSync(path, 'wx')). Its content (PID and RFC 3339 time)
//     is informational only.
//   - If it already exists, retry every 50 ms for up to 5 seconds.
//   - A lock file last modified more than 30 seconds ago is stale (its
//     process died): delete it and retry.
//   - The holder deletes it when done. Never hold it across network calls.
//
// Atomic write: write config.json.<random>.tmp in the same folder (0600),
// fsync, close, then rename it over config.json.
// =============================================================================

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	lockFileName   = "config.lock"
	lockWait       = 5 * time.Second
	lockStaleAfter = 30 * time.Second
	lockRetryEvery = 50 * time.Millisecond
)

// ErrConfigBusy means another botwallet process held the config lock for
// longer than lockWait.
var ErrConfigBusy = errors.New("another botwallet command is updating config.json; try again in a moment")

// withConfigLock runs fn while holding the config lock. fn must use the
// unlocked primitives (LoadConfig, SaveConfig, seed writers) and must not
// call another function that takes the lock.
func withConfigLock(fn func() error) error {
	dir, err := ResolveConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	release, err := acquireLock(filepath.Join(dir, lockFileName))
	if err != nil {
		return err
	}
	defer release()

	return fn()
}

// acquireLock creates the lock file, waiting for other holders, and returns
// a function that removes it.
func acquireLock(path string) (func(), error) {
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
			_ = f.Close()
			return func() { removeWithRetry(path) }, nil
		}

		if errors.Is(err, fs.ErrExist) {
			if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > lockStaleAfter {
				if os.Remove(path) == nil {
					continue
				}
			}
		}
		// Anything else is retried too: on Windows, creating a file that
		// another process is deleting fails with "access denied" for a moment.

		if time.Now().After(deadline) {
			if errors.Is(err, fs.ErrExist) {
				return nil, ErrConfigBusy
			}
			return nil, fmt.Errorf("failed to lock config: %w", err)
		}
		time.Sleep(lockRetryEvery)
	}
}

// writeFileAtomic replaces path with data. Readers see either the old file or
// the new one, never a partial write.
func writeFileAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp") // created 0600
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return renameWithRetry(tmpPath, path)
}

// renameWithRetry renames oldPath over newPath. On Windows the rename fails
// while another process (or an antivirus scanner) has newPath open, so it
// is retried for about a second.
func renameWithRetry(oldPath, newPath string) error {
	var err error
	delay := 10 * time.Millisecond
	for attempt := 0; attempt < 8; attempt++ {
		if err = os.Rename(oldPath, newPath); err == nil {
			return nil
		}
		time.Sleep(delay)
		delay *= 2
		if delay > 200*time.Millisecond {
			delay = 200 * time.Millisecond
		}
	}
	return err
}

// removeWithRetry deletes path, retrying briefly for the same Windows reason.
// If it still fails, the lock goes stale after lockStaleAfter.
func removeWithRetry(path string) {
	for attempt := 0; attempt < 10; attempt++ {
		if err := os.Remove(path); err == nil || errors.Is(err, fs.ErrNotExist) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}
