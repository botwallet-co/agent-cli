package cmd

import (
	"strings"
	"testing"
)

// paylink get, send and cancel find a paylink by its full ID, not by the
// short code in its URL, so --human output must show the ID.
func TestPaylinkHumanOutputShowsFullIDs(t *testing.T) {
	const id = "3f6c1a2e-9b4d-4e8f-a1c2-7d5e9f0b1a23"
	home := t.TempDir()
	addTestWallet(t, home, "main")
	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		switch action {
		case "list_payment_requests":
			return apiOK(map[string]interface{}{"payment_requests": []interface{}{
				map[string]interface{}{"request_id": id, "short_code": "AB12CD", "status": "pending", "amount_usdc": 5.0},
			}})
		case "send_paylink_invitation":
			return apiOK(map[string]interface{}{"sent_to": "wallet", "to": "@data-bot", "short_code": "AB12CD"})
		}
		return apiFail(400, "VALIDATION_ERROR", "unexpected action "+action)
	})

	list := runCLI(t, home, api.URL, nil, "paylink", "list", "--human")
	if list.exitCode != 0 || !strings.Contains(list.stdout, id) {
		t.Errorf("paylink list does not show the full ID (exit %d):\n%s", list.exitCode, list.stdout)
	}

	send := runCLI(t, home, api.URL, nil, "paylink", "send", id, "--to", "@data-bot", "--human")
	if send.exitCode != 0 || !strings.Contains(send.stdout, "botwallet paylink get "+id) {
		t.Errorf("paylink send tip does not use the full ID (exit %d):\n%s", send.exitCode, send.stdout)
	}
	if strings.Contains(send.stdout, "paylink get AB12CD") {
		t.Errorf("paylink send tells the agent to look the paylink up by its short code:\n%s", send.stdout)
	}
}

// --reveal-owner is the flag's kebab-case name; --revealOwner, its first
// spelling, still works the same.
func TestPaylinkCreateRevealOwnerSpellings(t *testing.T) {
	home := t.TempDir()
	addTestWallet(t, home, "main")
	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		return apiOK(map[string]interface{}{"request_id": "pl-1", "amount_usdc": 5.0})
	})

	for _, c := range []struct {
		flags []string
		want  bool
	}{
		{nil, true},
		{[]string{"--reveal-owner=false"}, false},
		{[]string{"--revealOwner=false"}, false},
		{[]string{"--revealOwner=false", "--reveal-owner"}, true},
	} {
		args := append([]string{"paylink", "create", "5", "--desc", "Report"}, c.flags...)
		if run := runCLI(t, home, api.URL, nil, args...); run.exitCode != 0 {
			t.Fatalf("%v: exit %d\n%s", c.flags, run.exitCode, run.stdout)
		}
		sent := api.requests("create_payment_request")
		if got := sent[len(sent)-1]["reveal_owner"]; got != c.want {
			t.Errorf("%v: reveal_owner sent = %v, want %v", c.flags, got, c.want)
		}
	}

	help := runCLI(t, home, api.URL, nil, "paylink", "create", "--help")
	if !strings.Contains(help.stdout, "--reveal-owner") || strings.Contains(help.stdout, "--revealOwner") {
		t.Errorf("paylink create --help should list --reveal-owner only:\n%s", help.stdout)
	}
}
