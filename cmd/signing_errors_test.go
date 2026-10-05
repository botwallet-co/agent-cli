package cmd

import (
	"net/http"
	"strings"
	"testing"
)

// replyTo answers action with the given reply and every other call like
// server does.
func replyTo(server *signingAPI, action string, status int, body interface{}) func(string, map[string]interface{}) (int, interface{}) {
	return func(a string, req map[string]interface{}) (int, interface{}) {
		if a == action {
			return status, body
		}
		return server.handle(a, req)
	}
}

// apiFailWithFix is apiFail with the server's how_to_fix.
func apiFailWithFix(status int, code, message, howToFix string) (int, interface{}) {
	return status, map[string]interface{}{
		"success": false,
		"error":   map[string]interface{}{"code": code, "message": message, "how_to_fix": howToFix},
	}
}

// newSigningServer sets up a wallet and a server that would sign its
// 10 USDC payment.
func newSigningServer(t *testing.T) (string, *signingAPI) {
	t.Helper()
	home := t.TempDir()
	w := addTestWallet(t, home, "main")
	message, confirm := serverMessage(w, payeeAddress)
	return home, &signingAPI{t: t, groupKey: w.groupKey, agentPart: w.agentPart, message: message, confirm: confirm}
}

// checkError runs args and checks the error code and that how_to_fix
// contains each of wantFix.
func checkError(t *testing.T, home string, api *fakeAPI, args []string, wantCode string, wantFix ...string) map[string]interface{} {
	t.Helper()
	run := runCLI(t, home, api.URL, nil, args...)
	if run.exitCode == 0 {
		t.Fatalf("exit 0, want an error\n%s", run.stdout)
	}
	out := run.json(t)
	if out["error"] != wantCode {
		t.Fatalf("error = %v, want %s\n%s", out["error"], wantCode, run.stdout)
	}
	fix, _ := out["how_to_fix"].(string)
	for _, w := range wantFix {
		if !strings.Contains(fix, w) {
			t.Errorf("how_to_fix = %q, want it to contain %q", fix, w)
		}
	}
	return out
}

// submittingCommands are the confirms whose server submits the
// transaction, and the command that shows its status.
var submittingCommands = []struct {
	name  string
	args  []string
	check string
}{
	{"pay", []string{"pay", "confirm", "tx-1"}, "botwallet pay list --id tx-1"},
	{"withdraw", []string{"withdraw", "confirm", "wd-1"}, "botwallet withdraw get wd-1"},
}

func TestSignCompleteErrorCodesArePassedThrough(t *testing.T) {
	for _, c := range submittingCommands {
		t.Run(c.name, func(t *testing.T) {
			home, server := newSigningServer(t)
			status, body := apiFailWithFix(http.StatusBadRequest, "TRANSACTION_FAILED", "Blockhash not found",
				"The FROST signature may be invalid or the transaction expired. Create a new payment.")
			api := newFakeAPI(t, replyTo(server, "frost_sign_complete", status, body))

			out := checkError(t, home, api, c.args, "TRANSACTION_FAILED", "Create a new payment")
			if msg, _ := out["message"].(string); msg != "Blockhash not found" {
				t.Errorf("message = %q, want the server's", msg)
			}
		})
	}
}

func TestNoAnswerFromSignCompleteIsStatusUnknown(t *testing.T) {
	replies := []struct {
		name   string
		status int
		body   interface{}
	}{
		{"gateway timeout page", http.StatusGatewayTimeout, "upstream request timeout"},
		{"catch-all server error", http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   map[string]interface{}{"code": "INTERNAL_ERROR", "message": "An unexpected error occurred. Please try again."},
		}},
	}
	for _, c := range submittingCommands {
		for _, r := range replies {
			t.Run(c.name+"/"+r.name, func(t *testing.T) {
				home, server := newSigningServer(t)
				api := newFakeAPI(t, replyTo(server, "frost_sign_complete", r.status, r.body))

				out := checkError(t, home, api, c.args, "SUBMIT_STATUS_UNKNOWN", "Do not create", c.check)
				details, _ := out["details"].(map[string]interface{})
				if details["check_command"] != c.check {
					t.Errorf("details.check_command = %v, want %q", details["check_command"], c.check)
				}
			})
		}
	}
}

func TestX402NoAnswerFromSignCompleteSaysRunAgain(t *testing.T) {
	home, server := newSigningServer(t)
	api := newFakeAPI(t, replyTo(server, "x402_sign_complete", http.StatusGatewayTimeout, "upstream request timeout"))

	checkError(t, home, api, []string{"x402", "fetch", "confirm", "fetch-1"}, "SIGNING_ERROR",
		"Nothing was sent", "botwallet x402 fetch confirm fetch-1")
}

func TestSessionExpiredSaysHowToRetry(t *testing.T) {
	home, server := newSigningServer(t)
	status, body := apiFailWithFix(http.StatusBadRequest, "SESSION_EXPIRED", "Signing session expired (60 second limit)",
		"Start a new signing flow")
	api := newFakeAPI(t, replyTo(server, "frost_sign_complete", status, body))

	checkError(t, home, api, []string{"pay", "confirm", "tx-1"}, "SESSION_EXPIRED",
		"Nothing was sent", "botwallet pay confirm tx-1", "INVALID_STATUS", "create a new payment")
}

func TestSignInitNetworkErrorSentNothing(t *testing.T) {
	home, server := newSigningServer(t)
	api := newFakeAPI(t, replyTo(server, "frost_sign_init", http.StatusBadGateway, "bad gateway"))

	checkError(t, home, api, []string{"pay", "confirm", "tx-1"}, "SIGNING_ERROR",
		"Nothing was sent", "botwallet pay confirm tx-1")
	if api.received("frost_sign_complete") {
		t.Error("frost_sign_complete was sent after frost_sign_init failed")
	}
}

// sentSignature stands in for the Solana signature of a sent transaction.
const sentSignature = "5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW"

// sentUnconfirmedReply is the server's answer (HTTP 202) for a payment or
// withdrawal it sent to Solana that is not confirmed yet.
func sentUnconfirmedReply(id, check string) (int, interface{}) {
	return http.StatusAccepted, map[string]interface{}{
		"success": false,
		"error": map[string]interface{}{
			"code":             "SUBMISSION_UNCONFIRMED",
			"message":          "This payment was sent to Solana but is not confirmed yet, so it may still go through. Do not pay again.",
			"how_to_fix":       "Check its status in about a minute with: " + check + ". It will show completed or failed.",
			"transaction_id":   id,
			"status":           "pending",
			"solana_signature": sentSignature,
			"explorer_url":     "https://solscan.io/tx/" + sentSignature,
			"check_command":    check,
		},
	}
}

func TestSentButUnconfirmedIsPendingNotAnError(t *testing.T) {
	confirms := map[string]string{"pay": "confirm_payment", "withdraw": "confirm_withdrawal"}
	for _, c := range submittingCommands {
		id := c.args[len(c.args)-1]
		// From frost_sign_complete; from frost_sign_init and the confirm call
		// when it was sent earlier
		for _, action := range []string{"frost_sign_complete", "frost_sign_init", confirms[c.name]} {
			t.Run(c.name+"/"+action, func(t *testing.T) {
				home, server := newSigningServer(t)
				status, body := sentUnconfirmedReply(id, c.check)
				api := newFakeAPI(t, replyTo(server, action, status, body))

				run := runCLI(t, home, api.URL, nil, c.args...)
				if run.exitCode != 0 {
					t.Fatalf("exit %d, want 0: it was sent\n%s", run.exitCode, run.stdout)
				}
				out := run.json(t)
				if out["error"] != nil {
					t.Fatalf("reported as an error: %v", out)
				}
				want := map[string]interface{}{
					"status":           "pending",
					"sent":             true,
					"transaction_id":   id,
					"solana_signature": sentSignature,
					"explorer_url":     "https://solscan.io/tx/" + sentSignature,
					"check_command":    c.check,
				}
				for k, v := range want {
					if out[k] != v {
						t.Errorf("%s = %v, want %v", k, out[k], v)
					}
				}
				hint, _ := out["agent_hint"].(string)
				for _, w := range []string{"do not create", c.check} {
					if !strings.Contains(hint, w) {
						t.Errorf("agent_hint = %q, want it to contain %q", hint, w)
					}
				}
				if action != "frost_sign_complete" && api.received("frost_sign_complete") {
					t.Error("signed again after the server said it was sent")
				}
			})
		}
	}
}

func TestSentButUnconfirmedHumanOutput(t *testing.T) {
	home, server := newSigningServer(t)
	status, body := sentUnconfirmedReply("tx-1", "botwallet pay list --id tx-1")
	api := newFakeAPI(t, replyTo(server, "frost_sign_complete", status, body))

	run := runCLI(t, home, api.URL, nil, "--human", "pay", "confirm", "tx-1")
	if run.exitCode != 0 {
		t.Fatalf("exit %d, want 0\n%s\n%s", run.exitCode, run.stdout, run.stderr)
	}
	for _, w := range []string{"not confirmed yet", "Do not send it again", "botwallet pay list --id tx-1", "https://solscan.io/tx/" + sentSignature} {
		if !strings.Contains(run.stdout, w) {
			t.Errorf("output does not contain %q:\n%s", w, run.stdout)
		}
	}
	if strings.Contains(run.stderr, "❌") {
		t.Errorf("shown as an error:\n%s", run.stderr)
	}
}

func TestConfirmInvalidStatusSaysHowToCheck(t *testing.T) {
	confirms := map[string]string{"pay": "confirm_payment", "withdraw": "confirm_withdrawal"}
	for _, c := range submittingCommands {
		t.Run(c.name, func(t *testing.T) {
			home, server := newSigningServer(t)
			status, body := apiFail(http.StatusBadRequest, "INVALID_STATUS", "Cannot confirm transaction with status: pending")
			api := newFakeAPI(t, replyTo(server, confirms[c.name], status, body))

			checkError(t, home, api, c.args, "INVALID_STATUS", c.check, "do not create")
			if api.received("frost_sign_init") {
				t.Error("signing started although confirm failed")
			}
		})
	}
}

// frozenReply is the server's answer when the owner has frozen the wallet.
func frozenReply() (int, interface{}) {
	return http.StatusForbidden, map[string]interface{}{
		"success": false,
		"error": map[string]interface{}{
			"code":       "WALLET_SUSPENDED",
			"message":    "This wallet is frozen by its owner, so it can't send money. Ask your owner to unfreeze it in the Botwallet dashboard.",
			"how_to_fix": "Ask your owner to unfreeze this wallet in the Botwallet dashboard, then try again.",
			"guard_rail": map[string]interface{}{"key": "wallet_suspended", "current_value": "Frozen"},
		},
	}
}

func TestFrozenWalletSaysWhichConfirmFinishesItLater(t *testing.T) {
	cases := []struct {
		args   []string
		action string
		retry  string
	}{
		{[]string{"pay", "confirm", "tx-1"}, "confirm_payment", "botwallet pay confirm tx-1"},
		{[]string{"pay", "confirm", "tx-1"}, "frost_sign_init", "botwallet pay confirm tx-1"},
		{[]string{"pay", "confirm", "tx-1"}, "frost_sign_complete", "botwallet pay confirm tx-1"},
		{[]string{"withdraw", "confirm", "wd-1"}, "confirm_withdrawal", "botwallet withdraw confirm wd-1"},
		{[]string{"x402", "fetch", "confirm", "fetch-1"}, "x402_sign_complete", "botwallet x402 fetch confirm fetch-1"},
	}
	for _, c := range cases {
		t.Run(c.args[0]+"/"+c.action, func(t *testing.T) {
			home, server := newSigningServer(t)
			status, body := frozenReply()
			api := newFakeAPI(t, replyTo(server, c.action, status, body))

			out := checkError(t, home, api, c.args, "WALLET_SUSPENDED", "Nothing was sent", "unfreeze", c.retry)
			if msg, _ := out["message"].(string); !strings.Contains(msg, "frozen by its owner") {
				t.Errorf("message = %q, want the server's", msg)
			}
		})
	}
}

func TestOwnerSigningTransferIsLeftToTheOwner(t *testing.T) {
	confirms := map[string]string{"pay": "confirm_payment", "withdraw": "confirm_withdrawal"}
	for _, c := range submittingCommands {
		t.Run(c.name, func(t *testing.T) {
			home, server := newSigningServer(t)
			status, body := apiFailWithFix(http.StatusBadRequest, "INVALID_STATUS",
				"This transfer was started by the wallet owner on the signing page, and only the owner can finish it there",
				"Leave it to the owner. Check it with: "+c.check)
			api := newFakeAPI(t, replyTo(server, confirms[c.name], status, body))

			out := checkError(t, home, api, c.args, "INVALID_STATUS", "Leave it to the owner", c.check)
			if fix, _ := out["how_to_fix"].(string); strings.Contains(fix, "do not create") {
				t.Errorf("how_to_fix = %q, want the server's advice, not the one for a pending item", fix)
			}
			if api.received("frost_sign_init") {
				t.Error("signing started for the owner's own transfer")
			}
		})
	}
}
