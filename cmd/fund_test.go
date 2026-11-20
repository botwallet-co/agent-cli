package cmd

import "testing"

func TestFundStatusFilter(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"", "", true},
		{"all", "all", true},
		{"pending", "pending", true},
		{"funded", "funded", true},
		{"dismissed", "dismissed", true},
		{"Funded", "funded", true},
		// Statuses the older help listed.
		{"approved", "funded", true},
		{"denied", "dismissed", true},
		{"rejected", "dismissed", true},
		{"paid", "", false},
	}
	for _, c := range cases {
		got, ok := fundStatusFilter(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("fundStatusFilter(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestFundListStatus(t *testing.T) {
	home := t.TempDir()
	addTestWallet(t, home, "main")

	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		return apiOK(map[string]interface{}{"requests": []interface{}{}, "total": 0})
	})

	if run := runCLI(t, home, api.URL, nil, "fund", "list", "--status", "approved"); run.exitCode != 0 {
		t.Fatalf("exit %d\n%s", run.exitCode, run.stdout)
	}
	if got := api.requests("list_fund_requests"); len(got) != 1 || got[0]["status"] != "funded" {
		t.Fatalf("requests = %v, want one with status funded", got)
	}

	run := runCLI(t, home, api.URL, nil, "fund", "list", "--status", "paid")
	if code := run.json(t)["error"]; code != "VALIDATION_ERROR" {
		t.Fatalf("error = %v, want VALIDATION_ERROR", code)
	}
	if len(api.called()) != 1 {
		t.Errorf("an unknown status was sent to the server: %v", api.called())
	}
}
