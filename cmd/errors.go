package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/output"
)

// =============================================================================
// Errors: codes, advice and the exit
// =============================================================================

// noHomeDirHowToFix is the advice when there is no folder for wallet keys.
const noHomeDirHowToFix = "Set HOME, or set " + config.ConfigDirEnv + " to the absolute path of a private folder for Botwallet keys"

// exitError prints an error (output.PrintError) and exits with code 1.
// Commands end every failure here.
func exitError(code, message, howToFix string, details map[string]interface{}) {
	output.PrintError(code, message, howToFix, details)
	os.Exit(1)
}

// exitValidationError is exitError for a client-side validation error,
// one found before any API call.
func exitValidationError(message, howToFix string) {
	exitError("VALIDATION_ERROR", message, howToFix, nil)
}

// exitWalletError prints an error about which wallet to use, or about the
// local wallet files, and exits.
func exitWalletError(err error) {
	var notFound *config.WalletNotFoundError
	var conflict *config.WalletConflictError
	var noLocal *config.NoLocalWalletError

	code, howToFix := "CONFIG_ERROR", "Check "+config.ConfigPath()
	switch {
	case errors.As(err, &notFound):
		code, howToFix = "WALLET_NOT_FOUND", "Use 'botwallet wallet list' to see available wallets"
	case errors.As(err, &conflict):
		code = "WALLET_CONFLICT"
		howToFix = fmt.Sprintf("Use one wallet: drop --wallet to use the key from %s, or remove that key to use --wallet %s",
			conflict.KeySource, conflict.Wallet)
	case errors.As(err, &noLocal):
		code = "NO_LOCAL_WALLET"
		howToFix = fmt.Sprintf("Use --wallet <name> for a wallet in 'botwallet wallet list' (and remove the key from %s), "+
			"or import this wallet with 'botwallet wallet import'", noLocal.KeySource)
	case errors.Is(err, config.ErrNoWallet):
		code, howToFix = "NO_WALLET", "Run 'botwallet wallet create' first, or use --wallet flag"
	case errors.Is(err, config.ErrNoHomeDir):
		code, howToFix = "NO_HOME_DIR", noHomeDirHowToFix
	}
	exitError(code, err.Error(), howToFix, nil)
}

// handleAPIError prints an error from an API call and exits.
func handleAPIError(err error) {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		// Provide default how_to_fix if API didn't supply one
		howToFix := apiErr.HowToFix
		if howToFix == "" {
			howToFix = getDefaultHowToFix(apiErr.Code, apiErr.Details)
		}
		// Transform terminology for consistency
		message := transformErrorMessage(apiErr.Message)
		exitError(apiErr.Code, message, howToFix, apiErr.Details)
	} else {
		exitError("CLI_ERROR", err.Error(), "Check your network connection and try again", nil)
	}
}

// transformErrorMessage updates error messages to use current CLI terminology
func transformErrorMessage(msg string) string {
	// Replace "Payment request" with "Paylink" for consistency
	msg = strings.ReplaceAll(msg, "Payment request", "Paylink")
	msg = strings.ReplaceAll(msg, "payment request", "paylink")
	return msg
}

// getDefaultHowToFix provides default fix suggestions for common errors
func getDefaultHowToFix(code string, details map[string]interface{}) string {
	switch code {
	case "INSUFFICIENT_FUNDS":
		if url, ok := details["funding_url"].(string); ok {
			return "Add funds via " + url + " or use 'botwallet fund ask' command"
		}
		return "Add funds to your wallet using 'botwallet wallet deposit' or 'botwallet fund ask'"
	case "RECIPIENT_NOT_FOUND":
		return "Use 'botwallet lookup <username>' to verify the recipient exists"
	case "VALIDATION_ERROR":
		return "Check command parameters and try again"
	case "INVALID_STATUS":
		return "Check its current status first ('botwallet pay list --id <id>' for a payment, 'botwallet withdraw get <id>' for a withdrawal)"
	case "UNAUTHORIZED":
		return "Check your API key is correct. Use --api-key flag or set BOTWALLET_API_KEY"
	case "WALLET_NOT_CLAIMED":
		claimURL, hasURL := details["claim_url"].(string)
		claimCode, hasCode := details["claim_code"].(string)
		pledgedTo, hasPledged := details["pledged_to"].(string)

		if hasURL && hasCode {
			msg := fmt.Sprintf("⚠️ BLOCKED: Your wallet must be claimed by a human owner first! "+
				"Tell your human: \"Please claim my wallet at %s using code %s\"", claimURL, claimCode)
			if hasPledged && pledgedTo != "" {
				msg += fmt.Sprintf(" (Pledged to: %s)", pledgedTo)
			}
			return msg
		}
		return "Your wallet needs to be claimed by a human owner. Run 'botwallet wallet info' to get the claim URL and code, then ask your human to claim it."
	case "WALLET_ALREADY_CLAIMED":
		return "This wallet is already claimed. Ask your owner to release it from their Human Portal first."
	// Codes newer servers send. Their replies carry their own how_to_fix;
	// these are for one that does not.
	case "WALLET_SUSPENDED":
		return "Your owner has frozen this wallet, so it can't send money. Ask them to unfreeze it in the Botwallet dashboard, then run the same command again"
	case "IDEMPOTENCY_KEY_CONFLICT":
		return "If you are retrying the same request, run it again with the same arguments and --idempotency-key to get its result. " +
			"Otherwise use a new, unique key (for example a UUID) for each payment or withdrawal"
	case "DEPRECATED":
		return "Botwallet no longer supports what this version of the CLI asked for. Update the CLI (npm install -g @botwallet/agent-cli@latest), then try again"
	case "SIGNING_IN_PROGRESS", "SUBMISSION_UNCONFIRMED":
		check := "'botwallet pay list --id <id>' for a payment, 'botwallet withdraw get <id>' for a withdrawal"
		if c, _ := details["check_command"].(string); c != "" {
			check = "'" + c + "'"
		}
		return "It is on its way and may still go through, so do not pay again. Check its status in about a minute with " + check
	default:
		return ""
	}
}
