package cmd

import (
	"strings"
	"testing"
)

func TestPayStatusFilter(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"actionable", "actionable", true},
		{"pending", "pending", true},
		{"all", "all", true},
		{"awaiting_approval", "awaiting_approval", true},
		{"Completed", "completed", true},
		{"rejected", "rejected", true},
		{"cancelled", "expired", true},
		{"done", "", false},
	}
	for _, c := range cases {
		got, ok := payStatusFilter(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("payStatusFilter(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestPayListHumanShowsFullIDs(t *testing.T) {
	home := t.TempDir()
	addTestWallet(t, home, "main")

	const id = "3f6c1a2e-9b4d-4e8f-a1c2-7d5e9f0b1a23"
	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		return apiOK(map[string]interface{}{"payments": []interface{}{
			map[string]interface{}{"transaction_id": id, "status": "approved", "to": "@merchant", "amount_usdc": 5.0, "created_at": "2026-10-01T10:00:00Z"},
		}})
	})

	run := runCLI(t, home, api.URL, nil, "pay", "list", "--human")
	if run.exitCode != 0 {
		t.Fatalf("exit %d\n%s", run.exitCode, run.stdout)
	}
	// The server finds a payment by its full ID only.
	if !strings.Contains(run.stdout, "pay confirm "+id) {
		t.Errorf("tip does not show the full ID:\n%s", run.stdout)
	}
	if strings.Count(run.stdout, id) < 2 {
		t.Errorf("table does not show the full ID:\n%s", run.stdout)
	}
}
