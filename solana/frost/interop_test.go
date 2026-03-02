package frost

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"

	edwards "filippo.io/edwards25519"
)

// TestInteropVector generates deterministic test vectors that the Deno
// implementation MUST reproduce exactly. This ensures the Go CLI and
// Deno server produce compatible key shares and signatures.
//
// To verify cross-language compatibility:
//  1. Run: go test -v -run TestInteropVector
//  2. Take the logged hex values
//  3. Feed the same inputs into the Deno frost.ts functions
//  4. Compare outputs byte-for-byte
func TestInteropVector(t *testing.T) {
	// =========================================================================
	// TEST VECTOR 1: ScalarFromEntropy with known input
	// =========================================================================
	entropy, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")

	scalar, err := ScalarFromEntropy(entropy)
	if err != nil {
		t.Fatalf("ScalarFromEntropy failed: %v", err)
	}

	scalarHex := hex.EncodeToString(EncodeScalar(scalar))
	publicPoint := new(edwards.Point).ScalarBaseMult(scalar)
	publicHex := hex.EncodeToString(EncodePoint(publicPoint))

	t.Logf("VECTOR 1: ScalarFromEntropy")
	t.Logf("  entropy = 000102030405060708090a0b0c0d0e0f")
	t.Logf("  scalar  = %s", scalarHex)
	t.Logf("  public  = %s", publicHex)

	// =========================================================================
	// TEST VECTOR 2: Full DKG + Sign with fixed inputs
	// =========================================================================

	// Bot: derive from fixed entropy
	botEntropy, _ := hex.DecodeString("00000000000000000000000000000000")
	botScalar, _ := ScalarFromEntropy(botEntropy)
	botPublic := new(edwards.Point).ScalarBaseMult(botScalar)

	// Server: derive from different fixed entropy
	serverEntropy, _ := hex.DecodeString("01010101010101010101010101010101")
	serverScalar, _ := ScalarFromEntropy(serverEntropy)
	serverPublic := new(edwards.Point).ScalarBaseMult(serverScalar)

	groupKey := ComputeGroupKey(botPublic, serverPublic)

	t.Logf("VECTOR 2: DKG")
	t.Logf("  bot_scalar    = %s", hex.EncodeToString(EncodeScalar(botScalar)))
	t.Logf("  bot_public    = %s", hex.EncodeToString(EncodePoint(botPublic)))
	t.Logf("  server_scalar = %s", hex.EncodeToString(EncodeScalar(serverScalar)))
	t.Logf("  server_public = %s", hex.EncodeToString(EncodePoint(serverPublic)))
	t.Logf("  group_key     = %s", hex.EncodeToString(EncodePoint(groupKey)))

	// Signing with deterministic nonces
	message := []byte("test message for interop")

	// Deterministic bot nonce (64 bytes for SetUniformBytes)
	botNonceBytes, _ := hex.DecodeString(
		"aabbccddaabbccddaabbccddaabbccdd" +
			"aabbccddaabbccddaabbccddaabbccdd" +
			"aabbccddaabbccddaabbccddaabbccdd" +
			"aabbccddaabbccddaabbccddaabbccdd")
	botNonceScalar, _ := new(edwards.Scalar).SetUniformBytes(botNonceBytes)
	botNonceCommitment := new(edwards.Point).ScalarBaseMult(botNonceScalar)

	// Deterministic server nonce
	serverNonceBytes, _ := hex.DecodeString(
		"1122334411223344112233441122334411223344112233441122334411223344" +
			"1122334411223344112233441122334411223344112233441122334411223344")
	serverNonceScalar, _ := new(edwards.Scalar).SetUniformBytes(serverNonceBytes)
	serverNonceCommitment := new(edwards.Point).ScalarBaseMult(serverNonceScalar)

	// Partial sign (bot)
	botNonce := &SigningNonce{Secret: botNonceScalar, Commitment: botNonceCommitment}
	botResult, err := PartialSign(message, botScalar, botNonce, serverNonceCommitment, groupKey)
	if err != nil {
		t.Fatalf("Bot PartialSign failed: %v", err)
	}

	// Partial sign (server)
	serverNonce := &SigningNonce{Secret: serverNonceScalar, Commitment: serverNonceCommitment}
	serverResult, err := PartialSign(message, serverScalar, serverNonce, botNonceCommitment, groupKey)
	if err != nil {
		t.Fatalf("Server PartialSign failed: %v", err)
	}

	sig := AggregateSignatures(botResult.GroupNonce, botResult.PartialSig, serverResult.PartialSig)

	t.Logf("VECTOR 2: Signing")
	t.Logf("  message           = %s", hex.EncodeToString(message))
	t.Logf("  bot_nonce_commit  = %s", hex.EncodeToString(EncodePoint(botNonceCommitment)))
	t.Logf("  server_nonce_comm = %s", hex.EncodeToString(EncodePoint(serverNonceCommitment)))
	t.Logf("  group_nonce       = %s", hex.EncodeToString(EncodePoint(botResult.GroupNonce)))
	t.Logf("  challenge         = %s", hex.EncodeToString(EncodeScalar(botResult.Challenge)))
	t.Logf("  bot_partial_sig   = %s", hex.EncodeToString(EncodeScalar(botResult.PartialSig)))
	t.Logf("  server_partial    = %s", hex.EncodeToString(EncodeScalar(serverResult.PartialSig)))
	t.Logf("  final_signature   = %s", hex.EncodeToString(sig[:]))

	valid := ed25519.Verify(EncodePoint(groupKey), message, sig[:])
	if !valid {
		t.Fatal("CRITICAL: Interop vector failed Ed25519 verification!")
	}
	t.Log("✅ Interop vector: valid Ed25519 signature")
}
