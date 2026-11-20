package cmd

import (
	"crypto/sha512"
	"encoding/base64"
	"net/http"
	"sync"
	"testing"

	"filippo.io/edwards25519"
	solanago "github.com/gagliardetto/solana-go"

	"github.com/botwallet-co/agent-cli/solana"
	"github.com/botwallet-co/agent-cli/solana/frost"
	"github.com/botwallet-co/agent-cli/solana/txcheck/txchecktest"
)

// The fake server side of the confirm commands and their FROST signing,
// shared by the signing, x402 and golden tests.

// payeeAddress is the recipient the fake confirm responses show.
var payeeAddress = testAddress("payee")

// usdcMint is the mainnet USDC mint.
var usdcMint = solanago.MustPublicKeyFromBase58(solana.USDCMintMainnet)

// signingCommands are the commands that co-sign with Key 1, and the call
// that would send the partial signature.
var signingCommands = []struct {
	name     string
	args     []string
	complete string
}{
	{"pay", []string{"pay", "confirm", "tx-1"}, "frost_sign_complete"},
	{"withdraw", []string{"withdraw", "confirm", "wd-1"}, "frost_sign_complete"},
	{"x402", []string{"x402", "fetch", "confirm", "fetch-1"}, "x402_sign_complete"},
}

// submittingCommands are the confirms whose server submits the
// transaction, their confirm call and the command that shows its status.
var submittingCommands = []struct {
	name    string
	args    []string
	confirm string
	check   string
}{
	{"pay", []string{"pay", "confirm", "tx-1"}, "confirm_payment", "botwallet pay list --id tx-1"},
	{"withdraw", []string{"withdraw", "confirm", "wd-1"}, "confirm_withdrawal", "botwallet withdraw get wd-1"},
}

// serverMessage builds the message the server's confirm returns for a
// 10 USDC payment with a 0.25 USDC fee from w to payTo, and the confirm
// fields that describe it (always to payeeAddress).
func serverMessage(w testWallet, payTo solanago.PublicKey) ([]byte, map[string]interface{}) {
	return serverMessageTo(w, payTo, txchecktest.ATA(payTo, usdcMint))
}

// serverMessageTo is serverMessage with the payment going to token account
// payToAccount.
func serverMessageTo(w testWallet, payTo, payToAccount solanago.PublicKey) ([]byte, map[string]interface{}) {
	mint := usdcMint
	wallet := solanago.MustPublicKeyFromBase58(w.address)
	feeCollection := testAddress("fee collection")
	msg := txchecktest.BuildPayment(txchecktest.Payment{
		FeePayer:               testAddress("fee payer"),
		Wallet:                 wallet,
		Source:                 txchecktest.ATA(wallet, mint),
		Recipient:              payTo,
		RecipientAccount:       payToAccount,
		RecipientAccountExists: true,
		FeeCollection:          feeCollection,
		FeeAccount:             txchecktest.ATA(feeCollection, mint),
		FeeAccountExists:       true,
		Mint:                   mint,
		Amount:                 10_000_000,
		Fee:                    250_000,
	})
	return msg.Bytes(), map[string]interface{}{
		"to_address":  payeeAddress.String(),
		"pay_to":      payeeAddress.String(),
		"amount_usdc": 10.0,
		"fee_usdc":    0.25,
		"total_usdc":  10.25,
		"network":     solana.ClusterMainnet,
	}
}

// signingAPI plays the server side of a confirm + FROST signing flow. It
// answers frost_sign_init with groupKey and checks the agent's partial
// signature against agentPart (the public share of the expected Key 1).
type signingAPI struct {
	groupKey  []byte
	agentPart *edwards25519.Point
	message   []byte
	confirm   map[string]interface{} // extra confirm response fields
	stored    []byte                 // message_to_sign from frost_sign_init; message when nil

	mu          sync.Mutex
	agentNonce  *edwards25519.Point
	serverNonce *frost.SigningNonce
	validSig    bool
}

// newSigningServer sets up a wallet and a server that would sign its
// 10 USDC payment.
func newSigningServer(t *testing.T) (string, *signingAPI) {
	t.Helper()
	home := t.TempDir()
	w := addTestWallet(t, home, "main")
	message, confirm := serverMessage(w, payeeAddress)
	return home, &signingAPI{groupKey: w.groupKey, agentPart: w.agentPart, message: message, confirm: confirm}
}

func (s *signingAPI) handle(action string, req map[string]interface{}) (int, interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch action {
	case "confirm_payment", "confirm_withdrawal", "x402_confirm":
		resp := map[string]interface{}{
			"message":        base64.StdEncoding.EncodeToString(s.message),
			"transaction_id": "tx-1",
			"url":            "https://example.invalid/paid",
		}
		for k, v := range s.confirm {
			resp[k] = v
		}
		return apiOK(resp)
	case "frost_sign_init":
		raw, _ := base64.StdEncoding.DecodeString(req["nonce_commitment"].(string))
		agentNonce, err := frost.DecodePoint(raw)
		if err != nil {
			return apiFail(http.StatusBadRequest, "VALIDATION_ERROR", "bad nonce commitment")
		}
		serverNonce, _ := frost.GenerateNonce()
		s.agentNonce, s.serverNonce = agentNonce, serverNonce
		stored := s.stored
		if stored == nil {
			stored = s.message
		}
		return apiOK(map[string]interface{}{
			"session_id":              "session-1",
			"server_nonce_commitment": base64.StdEncoding.EncodeToString(frost.EncodePoint(serverNonce.Commitment)),
			"group_key":               base64.StdEncoding.EncodeToString(s.groupKey),
			"message_to_sign":         base64.StdEncoding.EncodeToString(stored),
		})
	case "frost_sign_complete", "x402_sign_complete":
		raw, _ := base64.StdEncoding.DecodeString(req["partial_sig"].(string))
		z1, err := frost.DecodeScalar(raw)
		if err != nil {
			return apiFail(http.StatusBadRequest, "VALIDATION_ERROR", "bad partial signature")
		}
		// k = SHA-512(R || A || M) mod l, with R = R1 + R2 (standard Ed25519 challenge)
		groupNonce := new(edwards25519.Point).Add(s.agentNonce, s.serverNonce.Commitment)
		h := sha512.New()
		h.Write(groupNonce.Bytes())
		h.Write(s.groupKey)
		h.Write(s.message)
		k, _ := new(edwards25519.Scalar).SetUniformBytes(h.Sum(nil))
		s.validSig = frost.VerifyPartialSig(z1, s.agentNonce, s.agentPart, k)
		if !s.validSig {
			return apiFail(http.StatusBadRequest, "INVALID_PARTIAL_SIG", "partial signature does not verify")
		}
		return apiOK(map[string]interface{}{"status": "completed", "signed_transaction": "dGVzdA=="})
	case "balance":
		return apiOK(map[string]interface{}{"balance": 0})
	}
	return apiFail(http.StatusBadRequest, "VALIDATION_ERROR", "unexpected action "+action)
}

func (s *signingAPI) signatureWasValid() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validSig
}

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
