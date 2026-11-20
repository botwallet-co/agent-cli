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
// How a failure is reported, and what it says about whether anything was
// sent, is in signing_errors.go.

package cmd

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"

	solanago "github.com/gagliardetto/solana-go"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/output"
	"github.com/botwallet-co/agent-cli/solana"
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

// loadSigningWallet loads Key 1 of w, the wallet this command acts as (the
// one its API key belongs to). Commands call it before asking the server to
// prepare a transaction, so a missing key or a --wallet / API key mix-up
// stops them before anything changes on the server.
func loadSigningWallet(w *config.ResolvedWallet) (*signingWallet, error) {
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
	groupAddress := solana.AddressString(groupKeyBytes)
	local, err := solanago.PublicKeyFromBase58(s.address)
	if err != nil || !bytes.Equal(local[:], groupKeyBytes) {
		return &walletMismatchError{localName: s.localName, localAddress: s.address, groupAddress: groupAddress}
	}
	return nil
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
