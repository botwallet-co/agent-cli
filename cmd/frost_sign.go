// FROST 2-of-2 signing flow shared by pay confirm and withdraw confirm.
//
// Protocol:
//
//	Round 1: Exchange nonce commitments (R1 ↔ R2)
//	Round 2: Compute partial signature z1 = r1 + k·s1, send to server
//	Server:  Aggregates z = z1 + z2, assembles full Ed25519 sig, submits to Solana
package cmd

import (
	"encoding/base64"
	"fmt"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/output"
	"github.com/botwallet-co/agent-cli/solana/frost"
)

// frostSignAndSubmit performs the full FROST threshold signing flow.
// It loads the local key share, exchanges nonce commitments with the server,
// computes a partial signature, and sends it for aggregation and on-chain submission.
//
// Returns the server's response from frost_sign_complete (contains tx result).
func frostSignAndSubmit(client *api.Client, transactionID string, messageBase64 string, walletFlag string) (map[string]interface{}, error) {
	messageBytes, err := base64.StdEncoding.DecodeString(messageBase64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode message: %w", err)
	}

	// Load key share (S1) from local seed file
	_, localName, err := config.GetCurrentWallet(walletFlag)
	if err != nil {
		return nil, fmt.Errorf("no wallet configured: %w. Run 'botwallet wallet create' first, or use --wallet flag", err)
	}

	seedPath, err := config.GetCurrentWalletSeedPath(walletFlag)
	if err != nil {
		return nil, fmt.Errorf("failed to find seed file: %w", err)
	}

	mnemonic, err := config.LoadSeedFromPath(seedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load key share from %s (wallet: %s): %w", seedPath, localName, err)
	}

	keyShare, err := frost.KeyShareFromMnemonic(mnemonic)
	if err != nil {
		return nil, fmt.Errorf("failed to derive key share: %w", err)
	}

	// Round 1: Generate nonce, exchange commitments with server
	if output.IsHumanOutput() {
		output.InfoMsg("Signing with threshold protocol...")
	}

	nonce, err := frost.GenerateNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate signing nonce: %w", err)
	}

	nonceCommitmentB64 := base64.StdEncoding.EncodeToString(frost.EncodePoint(nonce.Commitment))

	signInitResult, err := client.FrostSignInit(transactionID, nonceCommitmentB64)
	if err != nil {
		return nil, fmt.Errorf("FROST sign init failed: %w", err)
	}

	sessionID, _ := signInitResult["session_id"].(string)
	serverNonceB64, _ := signInitResult["server_nonce_commitment"].(string)

	if sessionID == "" || serverNonceB64 == "" {
		return nil, fmt.Errorf("server returned invalid signing session")
	}

	serverNonceBytes, err := base64.StdEncoding.DecodeString(serverNonceB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode server nonce: %w", err)
	}

	serverNonceCommitment, err := frost.DecodePoint(serverNonceBytes)
	if err != nil {
		return nil, fmt.Errorf("server returned invalid nonce commitment: %w", err)
	}

	// Round 2: Compute partial signature z1 = r1 + k·s1
	groupKeyB64, _ := signInitResult["group_key"].(string)
	if groupKeyB64 == "" {
		return nil, fmt.Errorf("server did not return group key for signing")
	}

	groupKeyBytes, err := base64.StdEncoding.DecodeString(groupKeyB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode group key: %w", err)
	}

	groupKey, err := frost.DecodePoint(groupKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid group key: %w", err)
	}

	partialResult, err := frost.PartialSign(
		messageBytes,
		keyShare.Secret,
		nonce,
		serverNonceCommitment,
		groupKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to compute partial signature: %w", err)
	}

	// Send partial sig to server for aggregation and on-chain submission
	partialSigB64 := base64.StdEncoding.EncodeToString(frost.EncodeScalar(partialResult.PartialSig))

	submitResult, err := client.FrostSignComplete(sessionID, partialSigB64)
	if err != nil {
		return nil, fmt.Errorf("FROST sign complete failed: %w", err)
	}

	return submitResult, nil
}

// frostSignForX402 performs FROST threshold signing for x402 payments.
// Unlike frostSignAndSubmit, the server does NOT submit the transaction to
// Solana. Instead, it returns the fully signed transaction bytes so the CLI
// can include them in the X-Payment header for the external API.
func frostSignForX402(client *api.Client, transactionID string, messageBase64 string, walletFlag string) (map[string]interface{}, error) {
	messageBytes, err := base64.StdEncoding.DecodeString(messageBase64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode message: %w", err)
	}

	_, localName, err := config.GetCurrentWallet(walletFlag)
	if err != nil {
		return nil, fmt.Errorf("no wallet configured: %w. Run 'botwallet wallet create' first, or use --wallet flag", err)
	}

	seedPath, err := config.GetCurrentWalletSeedPath(walletFlag)
	if err != nil {
		return nil, fmt.Errorf("failed to find seed file: %w", err)
	}

	mnemonic, err := config.LoadSeedFromPath(seedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load key share from %s (wallet: %s): %w", seedPath, localName, err)
	}

	keyShare, err := frost.KeyShareFromMnemonic(mnemonic)
	if err != nil {
		return nil, fmt.Errorf("failed to derive key share: %w", err)
	}

	if output.IsHumanOutput() {
		output.InfoMsg("Signing x402 payment with threshold protocol...")
	}

	nonce, err := frost.GenerateNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate signing nonce: %w", err)
	}

	nonceCommitmentB64 := base64.StdEncoding.EncodeToString(frost.EncodePoint(nonce.Commitment))

	// Round 1: same as regular FROST — exchange nonce commitments
	signInitResult, err := client.FrostSignInit(transactionID, nonceCommitmentB64)
	if err != nil {
		return nil, fmt.Errorf("FROST sign init failed: %w", err)
	}

	sessionID, _ := signInitResult["session_id"].(string)
	serverNonceB64, _ := signInitResult["server_nonce_commitment"].(string)

	if sessionID == "" || serverNonceB64 == "" {
		return nil, fmt.Errorf("server returned invalid signing session")
	}

	serverNonceBytes, err := base64.StdEncoding.DecodeString(serverNonceB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode server nonce: %w", err)
	}

	serverNonceCommitment, err := frost.DecodePoint(serverNonceBytes)
	if err != nil {
		return nil, fmt.Errorf("server returned invalid nonce commitment: %w", err)
	}

	groupKeyB64, _ := signInitResult["group_key"].(string)
	if groupKeyB64 == "" {
		return nil, fmt.Errorf("server did not return group key for signing")
	}

	groupKeyBytes, err := base64.StdEncoding.DecodeString(groupKeyB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode group key: %w", err)
	}

	groupKey, err := frost.DecodePoint(groupKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid group key: %w", err)
	}

	partialResult, err := frost.PartialSign(
		messageBytes,
		keyShare.Secret,
		nonce,
		serverNonceCommitment,
		groupKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to compute partial signature: %w", err)
	}

	// Round 2: call x402_sign_complete instead of frost_sign_complete.
	// Server aggregates but does NOT submit to Solana — returns signed tx bytes.
	partialSigB64 := base64.StdEncoding.EncodeToString(frost.EncodeScalar(partialResult.PartialSig))

	signResult, err := client.X402SignComplete(sessionID, partialSigB64)
	if err != nil {
		return nil, fmt.Errorf("x402 sign complete failed: %w", err)
	}

	return signResult, nil
}
