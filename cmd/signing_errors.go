// How a confirm command reports a failure of the signing flow
// (frost_sign.go). The error says whether anything can have been sent:
//
//   - Before frost_sign_complete is called, nothing was sent. The confirm
//     call in the same run moved the item to pending, so no earlier
//     signing session exists for it either.
//   - frost_sign_complete answers with a server error code: passed through
//     as is (TRANSACTION_FAILED, SESSION_EXPIRED, INVALID_PARTIAL_SIG, ...).
//   - The server sent the transaction to Solana but it is not confirmed yet
//     (SUBMISSION_UNCONFIRMED, from frost_sign_complete, or from the confirm
//     and frost_sign_init calls for an item sent earlier). That is not a
//     failure: the item is reported as pending with its Solana signature,
//     the command exits 0, and the output says to check its status instead
//     of paying again.
//   - frost_sign_complete gets no usable answer (network error, timeout,
//     non-JSON reply, INTERNAL_ERROR): the server may have submitted the
//     transaction, so SUBMIT_STATUS_UNKNOWN says to check its status and
//     not to create a new payment.
//   - x402_sign_complete never submits to Solana; the CLI sends the signed
//     transaction to the API itself, so a lost answer means nothing was
//     paid and x402 fetch confirm can simply be run again.

package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/output"
	"github.com/botwallet-co/agent-cli/solana/txcheck"
)

// signingTarget is the item a confirm command signs for. Errors use it to
// say how to retry the confirm or check where the item is.
type signingTarget struct {
	id      string
	what    string // "payment", "withdrawal", "x402 payment"
	retry   string // runs this confirm again
	check   string // shows the item's status; "" when there is no such command
	newItem string // what to create if this one can no longer be sent
}

func paymentTarget(id string) signingTarget {
	return signingTarget{id: id, what: "payment",
		retry:   "botwallet pay confirm " + id,
		check:   "botwallet pay list --id " + id,
		newItem: "a new payment"}
}

func withdrawalTarget(id string) signingTarget {
	return signingTarget{id: id, what: "withdrawal",
		retry:   "botwallet withdraw confirm " + id,
		check:   "botwallet withdraw get " + id,
		newItem: "a new withdrawal request"}
}

func x402Target(fetchID string) signingTarget {
	return signingTarget{id: fetchID, what: "x402 payment",
		retry: "botwallet x402 fetch confirm " + fetchID}
}

// retryAdvice is the how_to_fix for a signing attempt that sent nothing.
func (t signingTarget) retryAdvice() string {
	return "Nothing was sent. " + t.rerunAdvice()
}

// rerunAdvice says how to try a confirm again after an attempt that sent
// nothing. x402 confirm resumes a pending payment. Pay and withdraw confirm
// refuse a pending item with INVALID_STATUS on current servers; the failed
// attempt never sent a partial signature, so the item can never be sent and
// a new one is safe.
func (t signingTarget) rerunAdvice() string {
	if t.check == "" {
		return fmt.Sprintf("Run '%s' again", t.retry)
	}
	return fmt.Sprintf("Run '%s' again. If that stops with INVALID_STATUS, this %s can no longer be sent: create %s",
		t.retry, t.what, t.newItem)
}

// invalidStatusAdvice is the how_to_fix when the server will not confirm
// the item because of its status, usually pending after an earlier
// confirm.
func (t signingTarget) invalidStatusAdvice() string {
	if t.check == "" {
		return fmt.Sprintf("This %s cannot be confirmed in its current state", t.what)
	}
	return fmt.Sprintf("Check where it is with '%s'. If it shows pending, it may already be on its way: do not create %s, "+
		"unless an earlier confirm for it said that nothing was sent or signed", t.check, t.newItem)
}

// frozenAdvice is the how_to_fix when the owner has frozen the wallet
// (WALLET_SUSPENDED). The server turns a frozen wallet's confirm and signing
// calls away before anything changes, so the same confirm finishes the item
// once the wallet is unfrozen.
func (t signingTarget) frozenAdvice() string {
	return fmt.Sprintf("Nothing was sent. Ask your owner to unfreeze this wallet in the Botwallet dashboard, then run '%s' again", t.retry)
}

// exitConfirmError prints an error from a confirm call and exits.
func exitConfirmError(err error, target signingTarget) {
	exitIfSentUnconfirmed(err, target)
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Code == "INVALID_STATUS" && apiErr.HowToFix == "":
			apiErr.HowToFix = target.invalidStatusAdvice()
		case apiErr.Code == "WALLET_SUSPENDED":
			apiErr.HowToFix = target.frozenAdvice()
		}
	}
	handleAPIError(err)
}

// exitIfSentUnconfirmed handles the server's answer for a payment or
// withdrawal it sent to Solana that is not confirmed yet
// (SUBMISSION_UNCONFIRMED, HTTP 202). It may still go through, so it is
// reported as pending, not as an error, with its Solana signature and the
// command that shows how it ends, and the command exits 0. Any other error
// is left to the caller.
func exitIfSentUnconfirmed(err error, target signingTarget) {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "SUBMISSION_UNCONFIRMED" || target.check == "" {
		return
	}

	id := target.id
	if txID, _ := apiErr.Details["transaction_id"].(string); txID != "" {
		id = txID
	}
	message := apiErr.Message
	if message == "" {
		message = fmt.Sprintf("This %s was sent to Solana but is not confirmed yet, so it may still go through", target.what)
	}
	result := map[string]interface{}{
		"status":         "pending",
		"sent":           true,
		"transaction_id": id,
		"message":        message,
		"check_command":  target.check,
		"agent_hint": fmt.Sprintf("This %s may still go through, so do not create %s for it. "+
			"Check it in about a minute with '%s': it will show completed or failed.", target.what, target.newItem, target.check),
	}
	if sig, _ := apiErr.Details["solana_signature"].(string); sig != "" {
		result["solana_signature"] = sig
	}
	if url, _ := apiErr.Details["explorer_url"].(string); url != "" {
		result["explorer_url"] = url
	}
	output.FormatSentUnconfirmed(target.what, result)
	os.Exit(0)
}

// exitSigningError prints a signing failure for target with the most
// specific error code and exits. A transaction that was sent and is not
// confirmed yet is not a failure (exitIfSentUnconfirmed).
func exitSigningError(err error, target signingTarget) {
	exitIfSentUnconfirmed(err, target)
	var mismatch *walletMismatchError
	var refused *txcheck.RefusedError
	var noLocal *config.NoLocalWalletError
	var keyLoad *keyLoadError
	var unknown *submitUnknownError
	var apiErr *api.APIError
	switch {
	case errors.As(err, &unknown):
		cause := unknown.err.Error()
		if errors.As(unknown.err, &apiErr) {
			// Not the server's "please try again": retrying is what must not happen.
			cause = "server error " + apiErr.Code
		}
		exitError("SUBMIT_STATUS_UNKNOWN",
			fmt.Sprintf("The %s was handed to Botwallet for sending, but no clear answer came back (%s). It may or may not have been sent", target.what, cause),
			fmt.Sprintf("Do not create %s. Run '%s' to see whether it was sent", target.newItem, target.check),
			map[string]interface{}{"id": target.id, "check_command": target.check})
	case errors.As(err, &mismatch):
		exitError("WALLET_MISMATCH", "Refusing to sign: "+mismatch.Error(),
			"The API key and Key 1 in use may belong to different wallets. Check this wallet's entry with 'botwallet wallet list'; if it looks right, report this to Botwallet", nil)
	case errors.As(err, &refused):
		exitError("TRANSACTION_MISMATCH", "Refusing to sign: "+refused.Reason+". Nothing was signed and no money moved",
			"Do not retry this "+target.what+". Check that the CLI talks to the real Botwallet API (--api-url, BOTWALLET_API_URL and base_url in config.json), then report this to Botwallet", nil)
	case errors.As(err, &noLocal), errors.Is(err, config.ErrNoWallet), errors.Is(err, config.ErrNoHomeDir):
		// RequireLocal found no Key 1 for the wallet in use
		exitWalletError(err)
	case errors.As(err, &keyLoad):
		exitError("SIGNING_ERROR", err.Error(),
			"Check your wallet configuration and try again", nil)
	case errors.As(err, &apiErr) && apiErr.Code != "":
		// The server's own code and advice, except where its advice
		// ("start a new signing flow", "try again") is not a command an
		// agent can run.
		howToFix := apiErr.HowToFix
		switch apiErr.Code {
		case "SESSION_EXPIRED":
			howToFix = target.retryAdvice()
		case "WALLET_SUSPENDED":
			howToFix = target.frozenAdvice()
		}
		if howToFix == "" {
			howToFix = getDefaultHowToFix(apiErr.Code, apiErr.Details)
		}
		exitError(apiErr.Code, transformErrorMessage(apiErr.Message), howToFix, apiErr.Details)
	default:
		exitError("SIGNING_ERROR", err.Error(), target.retryAdvice(), nil)
	}
}
