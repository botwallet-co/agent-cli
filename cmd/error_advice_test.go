package cmd

import (
	"net/http"
	"strings"
	"testing"
)

// Codes newer servers send get a next step even in a reply without its own
// how_to_fix; a reply with one keeps it.

func TestNewServerCodesHaveAdvice(t *testing.T) {
	cases := map[string][]string{
		"WALLET_SUSPENDED":         {"unfreeze it in the Botwallet dashboard"},
		"IDEMPOTENCY_KEY_CONFLICT": {"same arguments and --idempotency-key", "new, unique key"},
		"DEPRECATED":               {"Update the CLI", "@botwallet/agent-cli@latest"},
		"SIGNING_IN_PROGRESS":      {"do not pay again", "'botwallet pay list --id <id>'", "'botwallet withdraw get <id>'"},
		"SUBMISSION_UNCONFIRMED":   {"may still go through", "do not pay again"},
	}
	for code, want := range cases {
		fix := getDefaultHowToFix(code, nil)
		for _, w := range want {
			if !strings.Contains(fix, w) {
				t.Errorf("%s: how_to_fix = %q, want it to contain %q", code, fix, w)
			}
		}
	}

	fix := getDefaultHowToFix("SIGNING_IN_PROGRESS", map[string]interface{}{"check_command": "botwallet withdraw get wd-1"})
	if !strings.Contains(fix, "'botwallet withdraw get wd-1'") {
		t.Errorf("how_to_fix = %q, want the server's check command", fix)
	}
}

func TestIdempotencyKeyConflictAdvice(t *testing.T) {
	const serverFix = "Use a new, unique idempotency key (for example a UUID) for each payment, withdrawal or paid API call."
	for _, c := range []struct {
		name, fix, want string
	}{
		{"server advice kept", serverFix, serverFix},
		{"default without one", "", "same arguments and --idempotency-key"},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			addTestWallet(t, home, "main")
			api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
				return apiFailWithFix(http.StatusConflict, "IDEMPOTENCY_KEY_CONFLICT",
					"This idempotency key was already used for a different kind of request.", c.fix)
			})

			checkError(t, home, api, []string{"pay", "shop", "5", "--idempotency-key", "order-7"},
				"IDEMPOTENCY_KEY_CONFLICT", c.want)
		})
	}
}
