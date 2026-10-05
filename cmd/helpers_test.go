package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/config"
)

// These tests run the real CLI in a child process (the test binary started
// again with BW_TEST_CLI_ARGS set), because error paths end in os.Exit.
// The child talks to a fake API server started by the parent test and keeps
// its files in a temp BOTWALLET_HOME. Nothing reaches a real server.

// TestHelperCLI is not a real test: runCLI starts the test binary with
// BW_TEST_CLI_ARGS set, and this runs the CLI with those arguments.
func TestHelperCLI(t *testing.T) {
	raw := os.Getenv("BW_TEST_CLI_ARGS")
	if raw == "" {
		t.Skip("helper process only")
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		os.Exit(3)
	}
	// main sets the version; BW_TEST_VERSION stands in for it.
	if v := os.Getenv("BW_TEST_VERSION"); v != "" {
		SetVersionInfo(v, "test-commit", "test-date")
	}
	rootCmd.SetArgs(args)
	if err := Execute(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// cliRun is the outcome of one CLI run.
type cliRun struct {
	stdout   string
	stderr   string
	exitCode int
}

// json decodes the run's stdout as one JSON object.
func (r cliRun) json(t *testing.T) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("stdout is not JSON (%v):\n%s\nstderr:\n%s", err, r.stdout, r.stderr)
	}
	return out
}

// runCLI runs `botwallet <args>` against apiURL with home as BOTWALLET_HOME.
// extraEnv entries ("NAME=value") are added last.
func runCLI(t *testing.T, home, apiURL string, extraEnv []string, args ...string) cliRun {
	t.Helper()
	encoded, _ := json.Marshal(args)

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperCLI$", "-test.count=1")
	for _, kv := range os.Environ() {
		name := strings.SplitN(kv, "=", 2)[0]
		switch strings.ToUpper(name) {
		case "BOTWALLET_API_KEY", "BW_API_KEY", "BOTWALLET_API_URL", config.ConfigDirEnv:
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env,
		"BW_TEST_CLI_ARGS="+string(encoded),
		config.ConfigDirEnv+"="+home,
		"BOTWALLET_API_URL="+apiURL,
	)
	cmd.Env = append(cmd.Env, extraEnv...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	run := cliRun{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		run.exitCode = exitErr.ExitCode()
	default:
		t.Fatalf("running CLI: %v", err)
	}
	return run
}

// fakeAPI is a stand-in for the Botwallet API. handle gets the action and
// request body and returns the HTTP status and response body.
type fakeAPI struct {
	*httptest.Server
	mu      sync.Mutex
	actions []string
	auth    map[string]string // action -> Authorization header of its last call
}

func newFakeAPI(t *testing.T, handle func(action string, req map[string]interface{}) (int, interface{})) *fakeAPI {
	t.Helper()
	f := &fakeAPI{auth: map[string]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		action, _ := req["action"].(string)

		f.mu.Lock()
		f.actions = append(f.actions, action)
		f.auth[action] = r.Header.Get("Authorization")
		f.mu.Unlock()

		status, body := handle(action, req)
		if raw, ok := body.(string); ok {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(raw))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(f.Close)
	return f
}

// called returns the actions the fake API received, in order.
func (f *fakeAPI) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.actions...)
}

// authOf returns the Authorization header the last call of action carried.
func (f *fakeAPI) authOf(action string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.auth[action]
}

func (f *fakeAPI) received(action string) bool {
	for _, a := range f.called() {
		if a == action {
			return true
		}
	}
	return false
}

func apiOK(data map[string]interface{}) (int, interface{}) {
	return http.StatusOK, map[string]interface{}{"success": true, "data": data}
}

func apiFail(status int, code, message string) (int, interface{}) {
	return status, map[string]interface{}{
		"success": false,
		"error":   map[string]interface{}{"code": code, "message": message},
	}
}

func apiError(code, message string) error {
	return &api.APIError{Code: code, Message: message}
}

// errString is a plain (non-API) error, like a network failure.
type errString string

func (e errString) Error() string { return string(e) }
