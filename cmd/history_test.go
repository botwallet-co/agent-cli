package cmd

import "testing"

func TestHistoryTypeFilter(t *testing.T) {
	home := t.TempDir()
	addTestWallet(t, home, "main")

	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		return apiOK(map[string]interface{}{"transactions": []interface{}{}, "total": 0})
	})

	// The server filters by direction only.
	for _, v := range []string{"in", "out", "OUT"} {
		if run := runCLI(t, home, api.URL, nil, "history", "--type", v); run.exitCode != 0 {
			t.Fatalf("--type %s: exit %d\n%s", v, run.exitCode, run.stdout)
		}
	}
	sent := api.requests("transactions")
	if len(sent) != 3 {
		t.Fatalf("transactions requests = %v, want 3", sent)
	}
	for i, want := range []string{"in", "out", "out"} {
		if got := sent[i]["type"]; got != want {
			t.Errorf("type sent = %v, want %s", got, want)
		}
	}

	// A kind the server cannot filter by used to return every transaction.
	run := runCLI(t, home, api.URL, nil, "history", "--type", "payment")
	if code := run.json(t)["error"]; code != "VALIDATION_ERROR" {
		t.Fatalf("error = %v, want VALIDATION_ERROR\n%s", code, run.stdout)
	}
	if len(api.called()) != 3 {
		t.Errorf("an unsupported --type was sent to the server: %v", api.called())
	}
}
