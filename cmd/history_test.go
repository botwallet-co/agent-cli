package cmd

import "testing"

func TestHistoryTypeFilter(t *testing.T) {
	home := t.TempDir()
	addTestWallet(t, home, "main")

	types := make(chan interface{}, 10)
	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		types <- req["type"]
		return apiOK(map[string]interface{}{"transactions": []interface{}{}, "total": 0})
	})

	// The server filters by direction only.
	for _, v := range []string{"in", "out", "OUT"} {
		if run := runCLI(t, home, api.URL, nil, "history", "--type", v); run.exitCode != 0 {
			t.Fatalf("--type %s: exit %d\n%s", v, run.exitCode, run.stdout)
		}
	}
	for _, want := range []string{"in", "out", "out"} {
		if got := <-types; got != want {
			t.Errorf("type sent = %v, want %s", got, want)
		}
	}

	// A kind the server cannot filter by used to return every transaction.
	run := runCLI(t, home, api.URL, nil, "history", "--type", "payment")
	if code := run.json(t)["error"]; code != "VALIDATION_ERROR" {
		t.Fatalf("error = %v, want VALIDATION_ERROR\n%s", code, run.stdout)
	}
	if len(types) != 0 {
		t.Error("an unsupported --type was sent to the server")
	}
}
