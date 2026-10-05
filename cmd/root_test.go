package cmd

import (
	"net/http"
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	home := t.TempDir()
	env := []string{"BW_TEST_VERSION=1.2.3"}

	for _, args := range [][]string{{"--version"}, {"version"}} {
		run := runCLI(t, home, "http://127.0.0.1:0", env, args...)
		if run.exitCode != 0 {
			t.Fatalf("%v: exit %d\n%s", args, run.exitCode, run.stdout)
		}
		if v := run.json(t)["version"]; v != "1.2.3" {
			t.Errorf("%v: version = %v, want 1.2.3", args, v)
		}
	}

	run := runCLI(t, home, "http://127.0.0.1:0", env, "--version", "--human")
	if !strings.Contains(run.stdout, "Botwallet CLI v1.2.3") {
		t.Errorf("--version --human printed:\n%s", run.stdout)
	}
}

func TestBuildVersion(t *testing.T) {
	info := func(v string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Path: "github.com/botwallet-co/agent-cli", Version: v}}, true
		}
	}
	noInfo := func() (*debug.BuildInfo, bool) { return nil, false }

	cases := []struct {
		name      string
		ldflags   string
		buildInfo func() (*debug.BuildInfo, bool)
		want      string
	}{
		{"release build", "0.1.0-beta.12", info("(devel)"), "0.1.0-beta.12"},
		{"ldflags win over build info", "0.1.0-beta.12", info("v0.1.0-beta.11"), "0.1.0-beta.12"},
		{"go install at a tag", "dev", info("v0.1.0-beta.12"), "0.1.0-beta.12"},
		{"no ldflags at all", "", info("v0.1.0-beta.12"), "0.1.0-beta.12"},
		{"local build", "dev", info("(devel)"), "dev"},
		{"no build info", "dev", noInfo, "dev"},
	}
	for _, c := range cases {
		if got := buildVersion(c.ldflags, c.buildInfo); got != c.want {
			t.Errorf("%s: buildVersion = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBalanceShortcut(t *testing.T) {
	home := t.TempDir()
	addTestWallet(t, home, "main")
	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		return apiOK(map[string]interface{}{"balance_usdc": 12.5})
	})

	run := runCLI(t, home, api.URL, nil, "balance")
	if run.exitCode != 0 {
		t.Fatalf("exit %d\n%s", run.exitCode, run.stdout)
	}
	if got := api.called(); len(got) != 1 || got[0] != "balance" {
		t.Errorf("API calls = %v, want [balance]", got)
	}
}

func TestWithdrawalShortfallShowsAmounts(t *testing.T) {
	home := t.TempDir()
	addTestWallet(t, home, "main")
	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		return http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error": map[string]interface{}{
				"code":          "INSUFFICIENT_FUNDS",
				"message":       "Not enough balance for withdrawal + network fee",
				"balance_usdc":  3.25,
				"required_usdc": 50.5,
			},
		}
	})

	run := runCLI(t, home, api.URL, nil, "withdraw", "50", testAddress("dest").String(), "--reason", "test", "--human")
	if run.exitCode == 0 {
		t.Fatal("exit 0, want an error")
	}
	for _, want := range []string{"3.25", "50.50"} {
		if !strings.Contains(run.stdout, want) {
			t.Errorf("output does not show %s:\n%s", want, run.stdout)
		}
	}
}
