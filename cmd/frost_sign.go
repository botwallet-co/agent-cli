// FROST 2-of-2 signing flow shared by pay confirm, withdraw confirm and
// x402 fetch confirm.
//
// Protocol:
//
//	Round 1: Exchange nonce commitments (R1 ↔ R2)
//	Round 2: Compute partial signature z1 = r1 + k·s1, send to server
//	Server:  Aggregates z = z1 + z2, assembles full Ed25519 sig, submits to Solana
//
// Before any partial signature is computed, the CLI checks that it signs for
// the right wallet: the local Key 1 belongs to the wallet the API key belongs
// to (config.ResolveWallet), and the group key the server signs with is that
// wallet's deposit address (checkGroupKey). It also decodes the transaction
// and signs only a USDC payment from this wallet to the recipient and amount
// the confirm response showed, plus at most the quoted fee (checkMessage,
// rules in package txcheck).
//
// When signing fails, the error says whether anything can have been sent:
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
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	solanago "github.com/gagliardetto/solana-go"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/output"
	"github.com/botwallet-co/agent-cli/solana/frost"
	"github.com/botwallet-co/agent-cli/solana/txcheck"
)

// signingWallet is the local wallet whose Key 1 signs for this command.
type signingWallet struct {
	localName string
	address   string // deposit address (base58) from config.json; "" for entries written by older MCP versions
	keyShare  *frost.KeyShare
}

// walletMismatchError means the server asked to sign for a different wallet
// than the one whose Key 1 is loaded.
type walletMismatchError struct {
	localName    string
	localAddress string
	groupAddress string
}

func (e *walletMismatchError) Error() string {
	return fmt.Sprintf("the server asked to sign for wallet address %s, but Key 1 loaded here is for wallet '%s' (address %s); nothing was signed",
		e.groupAddress, e.localName, e.localAddress)
}

// keyLoadError means the wallet entry or its Key 1 could not be loaded,
// before anything was asked of the server.
type keyLoadError struct{ err error }

func (e *keyLoadError) Error() string { return e.err.Error() }
func (e *keyLoadError) Unwrap() error { return e.err }

// submitUnknownError means the partial signature was sent with
// frost_sign_complete but no usable answer came back, so the server may
// have submitted the transaction.
type submitUnknownError struct{ err error }

func (e *submitUnknownError) Error() string { return e.err.Error() }
func (e *submitUnknownError) Unwrap() error { return e.err }

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

// loadSigningWallet resolves the wallet this command acts as and loads its
// Key 1. Commands call it before asking the server to prepare a transaction,
// so a missing key or a --wallet / API key mix-up stops them before anything
// changes on the server.
func loadSigningWallet() (*signingWallet, error) {
	w, err := config.ResolveWallet(apiKeyFlag, walletFlag)
	if err != nil {
		return nil, &keyLoadError{err}
	}
	if err := w.RequireLocal(); err != nil {
		return nil, &keyLoadError{err}
	}

	seedPath := w.SeedPath()
	mnemonic, err := config.LoadSeedFromPath(seedPath)
	if err != nil {
		return nil, &keyLoadError{fmt.Errorf("failed to load key share from %s (wallet: %s): %w", seedPath, w.LocalName, err)}
	}

	keyShare, err := frost.KeyShareFromMnemonic(mnemonic)
	if err != nil {
		return nil, &keyLoadError{fmt.Errorf("failed to derive key share: %w", err)}
	}

	return &signingWallet{localName: w.LocalName, address: w.Entry.PublicKey, keyShare: keyShare}, nil
}

// checkGroupKey refuses to sign unless the signing session is for this
// wallet. group_key from frost_sign_init is base64 of the wallet's 32-byte
// address; config.json stores the same address in base58.
func (s *signingWallet) checkGroupKey(groupKeyBytes []byte) error {
	if s.address == "" {
		// No address on file to compare with. The server still rejects a
		// partial signature made with the wrong Key 1.
		return nil
	}
	groupAddress := solanaBase58Encode(groupKeyBytes)
	local, err := solanago.PublicKeyFromBase58(s.address)
	if err != nil || !bytes.Equal(local[:], groupKeyBytes) {
		return &walletMismatchError{localName: s.localName, localAddress: s.address, groupAddress: groupAddress}
	}
	return nil
}

// exitSigningError prints a signing failure for target with the most
// specific error code and exits. A transaction that was sent and is not
// confirmed yet is not a failure (exitIfSentUnconfirmed).
func exitSigningError(err error, target signingTarget) {
	exitIfSentUnconfirmed(err, target)
	var mismatch *walletMismatchError
	var refused *txcheck.RefusedError
	var notFound *config.WalletNotFoundError
	var conflict *config.WalletConflictError
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
		output.APIError("SUBMIT_STATUS_UNKNOWN",
			fmt.Sprintf("The %s was handed to Botwallet for sending, but no clear answer came back (%s). It may or may not have been sent", target.what, cause),
			fmt.Sprintf("Do not create %s. Run '%s' to see whether it was sent", target.newItem, target.check),
			map[string]interface{}{"id": target.id, "check_command": target.check})
	case errors.As(err, &mismatch):
		output.APIError("WALLET_MISMATCH", "Refusing to sign: "+mismatch.Error(),
			"The API key and Key 1 in use may belong to different wallets. Check this wallet's entry with 'botwallet wallet list'; if it looks right, report this to Botwallet", nil)
	case errors.As(err, &refused):
		output.APIError("TRANSACTION_MISMATCH", "Refusing to sign: "+refused.Reason+". Nothing was signed and no money moved",
			"Do not retry this "+target.what+". Check that the CLI talks to the real Botwallet API (--api-url, BOTWALLET_API_URL and base_url in config.json), then report this to Botwallet", nil)
	case errors.As(err, &notFound), errors.As(err, &conflict), errors.As(err, &noLocal),
		errors.Is(err, config.ErrNoWallet), errors.Is(err, config.ErrNoHomeDir):
		exitWalletError(err)
	case errors.As(err, &keyLoad):
		output.APIError("SIGNING_ERROR", err.Error(),
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
		output.APIError(apiErr.Code, transformErrorMessage(apiErr.Message), howToFix, apiErr.Details)
	default:
		output.APIError("SIGNING_ERROR", err.Error(), target.retryAdvice(), nil)
	}
}

// frostPartialSign runs FROST round 1 with the server and computes this
// wallet's partial signature over the message. It returns the signing
// session ID and the partial signature (base64), ready for the server's
// complete call.
func frostPartialSign(client *api.Client, transactionID string, messageBase64 string, signer *signingWallet, intent *signingIntent) (string, string, error) {
	messageBytes, err := base64.StdEncoding.DecodeString(messageBase64)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode message: %w", err)
	}

	// Check the transaction before a signing session starts. Without an
	// address on file, the check waits for the group key below.
	checked := false
	if signer.address != "" {
		wallet, err := solanago.PublicKeyFromBase58(signer.address)
		if err != nil {
			return "", "", fmt.Errorf("wallet '%s' has an invalid address in config.json (%s)", signer.localName, signer.address)
		}
		if err := checkMessage(messageBytes, wallet, intent); err != nil {
			return "", "", err
		}
		checked = true
	}

	// Round 1: Generate nonce, exchange commitments with server
	nonce, err := frost.GenerateNonce()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate signing nonce: %w", err)
	}

	nonceCommitmentB64 := base64.StdEncoding.EncodeToString(frost.EncodePoint(nonce.Commitment))

	signInitResult, err := client.FrostSignInit(transactionID, nonceCommitmentB64)
	if err != nil {
		return "", "", fmt.Errorf("FROST sign init failed: %w", err)
	}

	sessionID, _ := signInitResult["session_id"].(string)
	serverNonceB64, _ := signInitResult["server_nonce_commitment"].(string)

	if sessionID == "" || serverNonceB64 == "" {
		return "", "", fmt.Errorf("server returned invalid signing session")
	}

	serverNonceBytes, err := base64.StdEncoding.DecodeString(serverNonceB64)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode server nonce: %w", err)
	}

	serverNonceCommitment, err := frost.DecodePoint(serverNonceBytes)
	if err != nil {
		return "", "", fmt.Errorf("server returned invalid nonce commitment: %w", err)
	}

	// Round 2: Compute partial signature z1 = r1 + k·s1
	groupKeyB64, _ := signInitResult["group_key"].(string)
	if groupKeyB64 == "" {
		return "", "", fmt.Errorf("server did not return group key for signing")
	}

	groupKeyBytes, err := base64.StdEncoding.DecodeString(groupKeyB64)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode group key: %w", err)
	}

	// Only sign for this wallet's own address.
	if err := signer.checkGroupKey(groupKeyBytes); err != nil {
		return "", "", err
	}
	if !checked {
		if len(groupKeyBytes) != 32 {
			return "", "", fmt.Errorf("invalid group key: %d bytes", len(groupKeyBytes))
		}
		if err := checkMessage(messageBytes, solanago.PublicKeyFromBytes(groupKeyBytes), intent); err != nil {
			return "", "", err
		}
	}

	// The server signs the message it stored at confirm time (sent back as
	// message_to_sign by current servers). It must be the one checked here.
	if stored, _ := signInitResult["message_to_sign"].(string); stored != "" {
		storedBytes, err := base64.StdEncoding.DecodeString(stored)
		if err != nil || !bytes.Equal(storedBytes, messageBytes) {
			return "", "", txcheck.Refuse("the server's signing session is for a different transaction than the one it showed here")
		}
	}

	groupKey, err := frost.DecodePoint(groupKeyBytes)
	if err != nil {
		return "", "", fmt.Errorf("invalid group key: %w", err)
	}

	partialResult, err := frost.PartialSign(
		messageBytes,
		signer.keyShare.Secret,
		nonce,
		serverNonceCommitment,
		groupKey,
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to compute partial signature: %w", err)
	}

	return sessionID, base64.StdEncoding.EncodeToString(frost.EncodeScalar(partialResult.PartialSig)), nil
}

// frostSignAndSubmit performs the full FROST threshold signing flow with the
// signer's Key 1 and sends the partial signature for aggregation and
// on-chain submission.
//
// Returns the server's response from frost_sign_complete (contains tx result).
func frostSignAndSubmit(client *api.Client, transactionID string, messageBase64 string, signer *signingWallet, intent *signingIntent) (map[string]interface{}, error) {
	if output.IsHumanOutput() {
		output.InfoMsg("Signing with threshold protocol...")
	}

	sessionID, partialSigB64, err := frostPartialSign(client, transactionID, messageBase64, signer, intent)
	if err != nil {
		return nil, err
	}

	// Send partial sig to server for aggregation and on-chain submission
	submitResult, err := client.FrostSignComplete(sessionID, partialSigB64)
	if err != nil {
		// A coded answer comes from a known point in the server's flow:
		// either before submission or the result of it (TRANSACTION_FAILED,
		// or SUBMISSION_UNCONFIRMED when it was sent and is not confirmed
		// yet). Anything else (no answer, a gateway page, the catch-all
		// INTERNAL_ERROR) can come after the transaction was submitted.
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.Code != "" && apiErr.Code != "INTERNAL_ERROR" {
			return nil, err
		}
		return nil, &submitUnknownError{err}
	}

	return submitResult, nil
}

// frostSignForX402 performs FROST threshold signing for x402 payments.
// Unlike frostSignAndSubmit, the server does NOT submit the transaction to
// Solana. Instead, it returns the fully signed transaction bytes so the CLI
// can include them in the X-Payment header for the external API.
func frostSignForX402(client *api.Client, transactionID string, messageBase64 string, signer *signingWallet, intent *signingIntent) (map[string]interface{}, error) {
	if output.IsHumanOutput() {
		output.InfoMsg("Signing x402 payment with threshold protocol...")
	}

	sessionID, partialSigB64, err := frostPartialSign(client, transactionID, messageBase64, signer, intent)
	if err != nil {
		return nil, err
	}

	// Round 2: call x402_sign_complete instead of frost_sign_complete.
	// Server aggregates but does NOT submit to Solana — returns signed tx bytes.
	signResult, err := client.X402SignComplete(sessionID, partialSigB64)
	if err != nil {
		return nil, fmt.Errorf("x402 sign complete failed: %w", err)
	}

	return signResult, nil
}
