package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// useTempHome points the config folder at a fresh temp home for one test.
// os.UserHomeDir reads USERPROFILE on Windows and HOME elsewhere, so both
// are set; BOTWALLET_HOME is cleared so the default ~/.botwallet is used.
func useTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(ConfigDirEnv, "")
	return filepath.Join(home, ".botwallet")
}

func TestResolveConfigDir(t *testing.T) {
	t.Run("default is ~/.botwallet", func(t *testing.T) {
		want := useTempHome(t)
		got, err := ResolveConfigDir()
		if err != nil || got != want {
			t.Fatalf("ResolveConfigDir() = %q, %v; want %q", got, err, want)
		}
	})

	t.Run("BOTWALLET_HOME wins", func(t *testing.T) {
		useTempHome(t)
		override := t.TempDir()
		t.Setenv(ConfigDirEnv, override)
		got, err := ResolveConfigDir()
		if err != nil || got != filepath.Clean(override) {
			t.Fatalf("ResolveConfigDir() = %q, %v; want %q", got, err, override)
		}
	})

	t.Run("relative BOTWALLET_HOME is refused", func(t *testing.T) {
		useTempHome(t)
		t.Setenv(ConfigDirEnv, "relative/keys")
		if got, err := ResolveConfigDir(); err == nil {
			t.Fatalf("ResolveConfigDir() = %q, want an error", got)
		}
	})

	t.Run("no HOME falls back to the user database", func(t *testing.T) {
		dbHome := t.TempDir()
		t.Setenv("HOME", "")
		t.Setenv("USERPROFILE", "")
		t.Setenv(ConfigDirEnv, "")
		orig := userDBHome
		userDBHome = func() string { return dbHome }
		t.Cleanup(func() { userDBHome = orig })

		got, err := ResolveConfigDir()
		if err != nil || got != filepath.Join(dbHome, ".botwallet") {
			t.Fatalf("ResolveConfigDir() = %q, %v; want %q", got, err, filepath.Join(dbHome, ".botwallet"))
		}
	})

	t.Run("no home anywhere is an error, never a relative path", func(t *testing.T) {
		t.Setenv("HOME", "")
		t.Setenv("USERPROFILE", "")
		t.Setenv(ConfigDirEnv, "")
		orig := userDBHome
		userDBHome = func() string { return "" }
		t.Cleanup(func() { userDBHome = orig })

		if _, err := ResolveConfigDir(); !errors.Is(err, ErrNoHomeDir) {
			t.Fatalf("ResolveConfigDir() error = %v, want ErrNoHomeDir", err)
		}
		if dir := ConfigDir(); dir != "" {
			t.Fatalf("ConfigDir() = %q, want empty", dir)
		}

		// Nothing may be read from or written to the current directory.
		cwd := t.TempDir()
		prev, _ := os.Getwd()
		if err := os.Chdir(cwd); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(prev) })

		if _, err := LoadConfig(); !errors.Is(err, ErrNoHomeDir) {
			t.Errorf("LoadConfig() error = %v, want ErrNoHomeDir", err)
		}
		if err := SaveConfig(&Config{}); !errors.Is(err, ErrNoHomeDir) {
			t.Errorf("SaveConfig() error = %v, want ErrNoHomeDir", err)
		}
		if _, err := SaveSeed("my-bot", "words"); !errors.Is(err, ErrNoHomeDir) {
			t.Errorf("SaveSeed() error = %v, want ErrNoHomeDir", err)
		}
		if err := WriteBackupNonce("abcd", "my-bot"); !errors.Is(err, ErrNoHomeDir) {
			t.Errorf("WriteBackupNonce() error = %v, want ErrNoHomeDir", err)
		}
		entries, _ := os.ReadDir(cwd)
		if len(entries) != 0 {
			t.Errorf("files were written to the current directory: %v", entries)
		}
	})
}

// testMnemonic is a fixed BIP39 phrase; these tests never touch a network.
const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func TestConcurrentAddWalletKeepsEveryEntry(t *testing.T) {
	useTempHome(t)

	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			name := fmt.Sprintf("bot-%d", i)
			_, err := AddWalletWithInfo(name, name, name, "bw_bot_key_"+name, "addr-"+name, testMnemonic)
			errs <- err
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("AddWalletWithInfo: %v", err)
		}
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Wallets) != n {
		t.Fatalf("config has %d wallets, want %d", len(cfg.Wallets), n)
	}
}

// TestHelperAddWallet is not a real test: TestParallelProcessesKeepEveryEntry
// runs the test binary again with BW_TEST_HELPER_NAME set, so each child
// process adds one wallet the way a separate `botwallet` run would.
func TestHelperAddWallet(t *testing.T) {
	name := os.Getenv("BW_TEST_HELPER_NAME")
	if name == "" {
		t.Skip("helper process only")
	}
	if _, err := AddWalletWithInfo(name, name, name, "bw_bot_key_"+name, "addr-"+name, testMnemonic); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func TestParallelProcessesKeepEveryEntry(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	dir := t.TempDir()

	const n = 6
	cmds := make([]*exec.Cmd, n)
	for i := range cmds {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperAddWallet$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			ConfigDirEnv+"="+dir,
			fmt.Sprintf("BW_TEST_HELPER_NAME=proc-%d", i),
		)
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[i] = cmd
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("helper process failed: %v", err)
		}
	}

	t.Setenv(ConfigDirEnv, dir)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Wallets) != n {
		t.Fatalf("config has %d wallets, want %d", len(cfg.Wallets), n)
	}
	if _, err := os.Stat(filepath.Join(dir, lockFileName)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("lock file left behind: %v", err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "config.json.*.tmp"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestStaleLockIsTakenOver(t *testing.T) {
	dir := useTempHome(t)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, lockFileName)
	if err := os.WriteFile(lock, []byte("12345 crashed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * lockStaleAfter)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := AddWalletWithInfo("my-bot", "u", "My Bot", "k", "a", testMnemonic); err != nil {
		t.Fatalf("AddWalletWithInfo with a stale lock: %v", err)
	}
	if _, err := os.Stat(lock); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("lock file left behind: %v", err)
	}
}

func TestSaveConfigReplacesFileWholly(t *testing.T) {
	dir := useTempHome(t)
	if err := SaveConfig(&Config{Wallets: map[string]WalletEntry{"a": {Username: "a"}}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(&Config{Wallets: map[string]WalletEntry{"b": {Username: "b"}}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Wallets["b"]; !ok || len(cfg.Wallets) != 1 {
		t.Fatalf("unexpected wallets after save: %v", cfg.Wallets)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

// phrase returns a distinct 12-word phrase per wallet. Seed files are only
// checked for word count, so these do not need to be valid BIP39.
func phrase(word string) string {
	return strings.TrimSpace(strings.Repeat(word+" ", 12))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSecondWalletNeverOverwritesFirstSeed(t *testing.T) {
	dir := useTempHome(t)

	first, err := AddWalletWithInfo("My Bot", "first-user", "My Bot", "key-1", "addr-1", phrase("one"))
	if err != nil {
		t.Fatal(err)
	}
	if first.LocalName != "my-bot" {
		t.Fatalf("first wallet local name = %q, want my-bot", first.LocalName)
	}
	firstSeed := readFile(t, first.SeedPath)

	// config.json lost (reset, deleted, or written by a racing process):
	// the seed file is the only trace of the first wallet.
	if err := os.Remove(filepath.Join(dir, "config.json")); err != nil {
		t.Fatal(err)
	}

	if got := GenerateLocalName("My Bot"); got != "my-bot-2" {
		t.Errorf("GenerateLocalName with an orphaned seed file = %q, want my-bot-2", got)
	}

	second, err := AddWalletWithInfo("My Bot", "second-user", "My Bot", "key-2", "addr-2", phrase("two"))
	if err != nil {
		t.Fatal(err)
	}
	if second.LocalName != "my-bot-2" {
		t.Errorf("second wallet local name = %q, want my-bot-2", second.LocalName)
	}
	if got := readFile(t, first.SeedPath); got != firstSeed {
		t.Fatalf("first wallet's seed file changed:\n%s", got)
	}
	if words, err := LoadSeedFromPath(second.SeedPath); err != nil || words != phrase("two") {
		t.Errorf("second seed = %q, %v", words, err)
	}

	// Writing straight onto an existing seed file is refused.
	if _, err := SaveSeed("my-bot", phrase("three")); !errors.Is(err, fs.ErrExist) {
		t.Errorf("SaveSeed onto an existing file: error = %v, want fs.ErrExist", err)
	}
	if got := readFile(t, first.SeedPath); got != firstSeed {
		t.Fatalf("SaveSeed changed an existing seed file")
	}
}

func TestSameNameInParallelGetsDistinctSeeds(t *testing.T) {
	useTempHome(t)

	const n = 6
	type result struct {
		saved SavedWallet
		err   error
	}
	results := make(chan result, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			saved, err := AddWalletWithInfo("My Bot", "u", "My Bot", fmt.Sprintf("key-%d", i), "addr", phrase(fmt.Sprintf("w%d", i)))
			results <- result{saved, err}
		}(i)
	}

	names := map[string]bool{}
	phrases := map[string]bool{}
	for i := 0; i < n; i++ {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		names[r.saved.LocalName] = true
		words, err := LoadSeedFromPath(r.saved.SeedPath)
		if err != nil {
			t.Fatal(err)
		}
		phrases[words] = true
	}
	if len(names) != n || len(phrases) != n {
		t.Fatalf("got %d names and %d distinct seeds, want %d each", len(names), len(phrases), n)
	}
}

func TestSeedFileRecordsDepositAddress(t *testing.T) {
	useTempHome(t)
	saved, err := AddWalletWithInfo("My Bot", "u", "My Bot", "k", "DepositAddr111", testMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, saved.SeedPath), "# Deposit address: DepositAddr111\n") {
		t.Error("seed file header does not record the deposit address")
	}
	if words, err := LoadSeedFromPath(saved.SeedPath); err != nil || words != testMnemonic {
		t.Errorf("LoadSeedFromPath = %q, %v", words, err)
	}
}

func TestWalletSeedPathFindsBareMCPSeedFile(t *testing.T) {
	dir := useTempHome(t)
	if err := os.MkdirAll(filepath.Join(dir, "seeds"), 0700); err != nil {
		t.Fatal(err)
	}
	inSeeds := filepath.Join(dir, "seeds", "mcp-bot.seed")
	if err := os.WriteFile(inSeeds, []byte(testMnemonic+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := WalletSeedPath("mcp-bot", WalletEntry{SeedFile: "mcp-bot.seed"})
	if got != inSeeds {
		t.Errorf("WalletSeedPath = %q, want %q", got, inSeeds)
	}
	got = WalletSeedPath("cli-bot", WalletEntry{SeedFile: "seeds/cli-bot.seed"})
	if want := filepath.Join(dir, "seeds", "cli-bot.seed"); got != want {
		t.Errorf("WalletSeedPath = %q, want %q", got, want)
	}
}

func TestNewWalletSeedLifecycle(t *testing.T) {
	dir := useTempHome(t)
	if err := PrepareNewWallet(); err != nil {
		t.Fatalf("PrepareNewWallet on a fresh machine: %v", err)
	}

	localName, seedPath, err := SaveNewSeed("My Bot", phrase("one"), "Addr111")
	if err != nil || localName != "my-bot" {
		t.Fatalf("SaveNewSeed = %q, %v", localName, err)
	}

	// Outcome unknown: the seed is kept under a name that frees my-bot,
	// and never replaces a file that is already there.
	clash := filepath.Join(dir, "seeds", "unregistered-Addr222.seed")
	if err := os.WriteFile(clash, []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	_, otherPath, err := SaveNewSeed("Other", phrase("two"), "Addr222")
	if err != nil {
		t.Fatal(err)
	}
	if got := KeepUnregisteredSeed(otherPath, "Addr222"); got != otherPath {
		t.Errorf("KeepUnregisteredSeed replaced an existing file, returned %q", got)
	}
	if readFile(t, clash) != "other" {
		t.Error("existing unregistered seed file was changed")
	}

	kept := KeepUnregisteredSeed(seedPath, "Addr111")
	if kept != filepath.Join(dir, "seeds", "unregistered-Addr111.seed") {
		t.Fatalf("KeepUnregisteredSeed = %q", kept)
	}
	if words, err := LoadSeedFromPath(kept); err != nil || words != phrase("one") {
		t.Errorf("kept seed = %q, %v", words, err)
	}
	if got := GenerateLocalName("My Bot"); got != "my-bot" {
		t.Errorf("GenerateLocalName after keeping the seed = %q, want my-bot", got)
	}

	// Refused outright: the unused seed is removed.
	_, refusedPath, err := SaveNewSeed("My Bot", phrase("three"), "Addr333")
	if err != nil {
		t.Fatal(err)
	}
	if err := DiscardNewSeed(refusedPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(refusedPath); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("refused seed still exists: %v", err)
	}
}

func TestPrepareNewWalletRejectsBrokenConfig(t *testing.T) {
	dir := useTempHome(t)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareNewWallet(); err == nil {
		t.Fatal("PrepareNewWallet accepted an unreadable config.json")
	}
}

func TestSavePendingWallet(t *testing.T) {
	useTempHome(t)
	entry := WalletEntry{Username: "my-bot-1234", APIKey: "bw_bot_test", PublicKey: "Addr", SeedFile: "seeds/my-bot.seed"}

	first, err := SavePendingWallet(PendingWallet{LocalName: "my-bot", ConfigEntry: entry})
	if err != nil {
		t.Fatal(err)
	}
	second, err := SavePendingWallet(PendingWallet{LocalName: "my-bot", ConfigEntry: entry})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("second pending file replaced the first")
	}

	var got PendingWallet
	if err := json.Unmarshal([]byte(readFile(t, first)), &got); err != nil {
		t.Fatal(err)
	}
	if got.ConfigEntry.APIKey != entry.APIKey || got.LocalName != "my-bot" || got.Note == "" {
		t.Errorf("pending record = %+v", got)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(first); err != nil || info.Mode().Perm() != 0600 {
			t.Errorf("pending file mode = %v, %v; want 0600", info.Mode().Perm(), err)
		}
	}
}

func TestResolveWallet(t *testing.T) {
	setup := func(t *testing.T) {
		useTempHome(t)
		t.Setenv("BOTWALLET_API_KEY", "")
		t.Setenv("BW_API_KEY", "")
		// "alpha" is added last, so it is the default wallet.
		for _, w := range []string{"beta", "alpha"} {
			if _, err := AddWalletWithInfo(w, w+"-user", w, "key-"+w, "addr-"+w, phrase(w)); err != nil {
				t.Fatal(err)
			}
		}
	}

	tests := []struct {
		name       string
		apiKeyFlag string
		walletFlag string
		env        map[string]string
		wantKey    string
		wantSource string
		wantLocal  string // "" means no local wallet (RequireLocal fails)
		wantErr    interface{}
	}{
		{name: "default wallet", wantKey: "key-alpha", wantSource: "default wallet", wantLocal: "alpha"},
		{name: "--wallet", walletFlag: "beta", wantKey: "key-beta", wantSource: "--wallet", wantLocal: "beta"},
		{name: "env key selects its own wallet, not the default",
			env: map[string]string{"BOTWALLET_API_KEY": "key-beta"}, wantKey: "key-beta", wantSource: "BOTWALLET_API_KEY", wantLocal: "beta"},
		{name: "BW_API_KEY alias", env: map[string]string{"BW_API_KEY": "key-beta"}, wantKey: "key-beta", wantSource: "BW_API_KEY", wantLocal: "beta"},
		{name: "--api-key beats env", apiKeyFlag: "key-alpha", env: map[string]string{"BOTWALLET_API_KEY": "key-beta"},
			wantKey: "key-alpha", wantSource: "--api-key", wantLocal: "alpha"},
		{name: "env key matching --wallet", walletFlag: "beta", env: map[string]string{"BOTWALLET_API_KEY": "key-beta"},
			wantKey: "key-beta", wantSource: "--wallet", wantLocal: "beta"},
		{name: "env key for another wallet than --wallet", walletFlag: "alpha", env: map[string]string{"BOTWALLET_API_KEY": "key-beta"},
			wantErr: &WalletConflictError{}},
		{name: "unknown --api-key with --wallet", apiKeyFlag: "key-elsewhere", walletFlag: "alpha", wantErr: &WalletConflictError{}},
		{name: "unknown --wallet", walletFlag: "gamma", wantErr: &WalletNotFoundError{}},
		{name: "env key with no local wallet never uses the default's Key 1",
			env: map[string]string{"BOTWALLET_API_KEY": "key-elsewhere"}, wantKey: "key-elsewhere", wantSource: "BOTWALLET_API_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got, err := ResolveWallet(tt.apiKeyFlag, tt.walletFlag)
			if tt.wantErr != nil {
				switch tt.wantErr.(type) {
				case *WalletConflictError:
					var target *WalletConflictError
					if !errors.As(err, &target) {
						t.Fatalf("error = %v, want WalletConflictError", err)
					}
				case *WalletNotFoundError:
					var target *WalletNotFoundError
					if !errors.As(err, &target) {
						t.Fatalf("error = %v, want WalletNotFoundError", err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.APIKey != tt.wantKey || got.KeySource != tt.wantSource || got.LocalName != tt.wantLocal {
				t.Fatalf("ResolveWallet = key %q from %q, wallet %q; want key %q from %q, wallet %q",
					got.APIKey, got.KeySource, got.LocalName, tt.wantKey, tt.wantSource, tt.wantLocal)
			}

			localErr := got.RequireLocal()
			if tt.wantLocal == "" {
				var noLocal *NoLocalWalletError
				if !errors.As(localErr, &noLocal) {
					t.Fatalf("RequireLocal error = %v, want NoLocalWalletError", localErr)
				}
				return
			}
			if localErr != nil {
				t.Fatal(localErr)
			}
			if words, err := LoadSeedFromPath(got.SeedPath()); err != nil || words != phrase(tt.wantLocal) {
				t.Errorf("Key 1 for %s = %q, %v", tt.wantLocal, words, err)
			}
		})
	}
}

func TestResolveWalletWithoutConfig(t *testing.T) {
	useTempHome(t)
	t.Setenv("BOTWALLET_API_KEY", "")
	t.Setenv("BW_API_KEY", "")

	got, err := ResolveWallet("", "")
	if err != nil || got.APIKey != "" {
		t.Fatalf("ResolveWallet = %+v, %v", got, err)
	}
	if err := got.RequireLocal(); !errors.Is(err, ErrNoWallet) {
		t.Errorf("RequireLocal error = %v, want ErrNoWallet", err)
	}

	// A broken config.json does not block server calls made with an env key,
	// but anything that needs Key 1 reports the config problem.
	dir, _ := ResolveConfigDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOTWALLET_API_KEY", "key-env")
	got, err = ResolveWallet("", "")
	if err != nil || got.APIKey != "key-env" {
		t.Fatalf("ResolveWallet with broken config = %+v, %v", got, err)
	}
	if err := got.RequireLocal(); err == nil || err != got.ConfigErr {
		t.Errorf("RequireLocal error = %v, want the config error", err)
	}
}

// writeNonceAged writes a backup code for my-bot created age ago.
func writeNonceAged(t *testing.T, code string, age time.Duration) {
	t.Helper()
	if err := WriteBackupNonce(code, "my-bot"); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(BackupNonce{Code: code, Wallet: "my-bot",
		CreatedAt: time.Now().Add(-age).UTC().Format(time.RFC3339)})
	if err := os.WriteFile(BackupNoncePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestBackupNonceWindow(t *testing.T) {
	useTempHome(t)

	// An agent reads the warning and runs reveal-backup in a later step.
	writeNonceAged(t, "abcd", 2*time.Minute)
	if name, err := ValidateBackupNonce("wxyz"); err == nil {
		t.Fatalf("wrong code accepted (wallet %q)", name)
	}
	if name, err := ValidateBackupNonce("abcd"); err != nil || name != "my-bot" {
		t.Fatalf("code 2 minutes old: got %q, %v", name, err)
	}
	// Single use.
	if _, err := ValidateBackupNonce("abcd"); err == nil {
		t.Fatal("code accepted twice")
	}

	writeNonceAged(t, "abcd", BackupNonceTTL+time.Minute)
	_, err := ValidateBackupNonce("abcd")
	if err == nil || !strings.Contains(err.Error(), BackupNonceTTLText()) {
		t.Fatalf("expired code: err = %v, want the expiry naming %s", err, BackupNonceTTLText())
	}
	if _, statErr := os.Stat(BackupNoncePath()); !errors.Is(statErr, fs.ErrNotExist) {
		t.Error("expired code was not deleted")
	}
}
