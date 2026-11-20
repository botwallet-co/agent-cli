package cmd

import (
	"strings"
	"testing"
)

func TestBadAddressesStopBeforeTheServer(t *testing.T) {
	home := t.TempDir()
	addTestWallet(t, home, "main")
	bad := strings.Repeat("z", 44)

	for name, args := range map[string][]string{
		"withdraw": {"withdraw", "5.00", bad, "--reason", "test"},
		"pay":      {"pay", bad, "5.00"},
	} {
		t.Run(name, func(t *testing.T) {
			api := newFakeAPI(t, func(string, map[string]interface{}) (int, interface{}) {
				return apiOK(map[string]interface{}{})
			})
			run := runCLI(t, home, api.URL, nil, args...)
			if code := run.json(t)["error"]; run.exitCode == 0 || code != "VALIDATION_ERROR" {
				t.Fatalf("exit %d, error = %v, want VALIDATION_ERROR\n%s", run.exitCode, code, run.stdout)
			}
			if calls := api.called(); len(calls) != 0 {
				t.Errorf("server was called: %v", calls)
			}
		})
	}

	// Usernames still go to the server.
	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		return apiOK(map[string]interface{}{"transaction_id": "tx-1", "status": "pre_approved"})
	})
	runCLI(t, home, api.URL, nil, "pay", "@some-agent", "5.00")
	if !api.received("pay") {
		t.Errorf("pay to a username did not reach the server: %v", api.called())
	}
}
