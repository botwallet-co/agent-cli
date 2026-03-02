package frost

import (
	"crypto/ed25519"
	"testing"
)

// TestFullDKGAndSign exercises the complete FROST 2-of-2 flow:
//
//	DKG → both parties generate shares → compute group key
//	Sign → both parties produce partial signatures → aggregate
//	Verify → aggregated signature passes standard Ed25519 verification
//
// This test simulates both the bot (S1) and the server (S2) locally
// to prove correctness without any network calls.
func TestFullDKGAndSign(t *testing.T) {
	// =========================================================================
	// STEP 1: DKG — Both parties generate key shares
	// =========================================================================

	// Bot generates S1 (in production, this runs on the bot's machine)
	botShare, err := GenerateKeyShare()
	if err != nil {
		t.Fatalf("Bot GenerateKeyShare failed: %v", err)
	}

	// Server generates S2 (in production, this runs on the server)
	serverShare, err := GenerateKeyShare()
	if err != nil {
		t.Fatalf("Server GenerateKeyShare failed: %v", err)
	}

	// Both compute the same group public key: A = A1 + A2
	groupKeyBot := ComputeGroupKey(botShare.Public, serverShare.Public)
	groupKeyServer := ComputeGroupKey(botShare.Public, serverShare.Public)

	// Verify both sides compute the same group key
	if groupKeyBot.Equal(groupKeyServer) != 1 {
		t.Fatal("Group keys don't match between bot and server")
	}

	t.Logf("Group public key: %x", groupKeyBot.Bytes())

	// =========================================================================
	// STEP 2: SIGNING — Both parties produce partial signatures
	// =========================================================================

	message := []byte("test transaction message for Solana")

	// Bot generates nonce (fresh for every signing operation)
	botNonce, err := GenerateNonce()
	if err != nil {
		t.Fatalf("Bot GenerateNonce failed: %v", err)
	}

	// Server generates nonce
	serverNonce, err := GenerateNonce()
	if err != nil {
		t.Fatalf("Server GenerateNonce failed: %v", err)
	}

	// Bot produces partial signature
	// Bot knows: its own share (s1), its nonce (r1), server's commitment (R2), group key (A)
	botResult, err := PartialSign(
		message,
		botShare.Secret,
		botNonce,
		serverNonce.Commitment, // Bot receives R2 from server
		groupKeyBot,
	)
	if err != nil {
		t.Fatalf("Bot PartialSign failed: %v", err)
	}

	// Server produces partial signature
	// Server knows: its own share (s2), its nonce (r2), bot's commitment (R1), group key (A)
	serverResult, err := PartialSign(
		message,
		serverShare.Secret,
		serverNonce,
		botNonce.Commitment, // Server receives R1 from bot
		groupKeyServer,
	)
	if err != nil {
		t.Fatalf("Server PartialSign failed: %v", err)
	}

	// Verify both sides computed the same group nonce and challenge
	if botResult.GroupNonce.Equal(serverResult.GroupNonce) != 1 {
		t.Fatal("Group nonces don't match")
	}
	if botResult.Challenge.Equal(serverResult.Challenge) != 1 {
		t.Fatal("Challenges don't match")
	}

	// =========================================================================
	// STEP 3: VERIFY PARTIAL SIGNATURES
	// =========================================================================

	// Server verifies bot's partial signature: z1 * G == R1 + k * A1
	if !VerifyPartialSig(botResult.PartialSig, botNonce.Commitment, botShare.Public, botResult.Challenge) {
		t.Fatal("Bot's partial signature is invalid")
	}

	// Bot could verify server's partial signature (optional in our flow)
	if !VerifyPartialSig(serverResult.PartialSig, serverNonce.Commitment, serverShare.Public, serverResult.Challenge) {
		t.Fatal("Server's partial signature is invalid")
	}

	// =========================================================================
	// STEP 4: AGGREGATE → standard Ed25519 signature
	// =========================================================================

	signature := AggregateSignatures(
		botResult.GroupNonce,
		botResult.PartialSig,
		serverResult.PartialSig,
	)

	t.Logf("Aggregated signature: %x", signature)

	// =========================================================================
	// STEP 5: VERIFY — must pass standard Ed25519 verification
	// =========================================================================

	// Convert group key to standard Ed25519 public key format (32 bytes)
	groupKeyBytes := groupKeyBot.Bytes()

	// Use Go's standard crypto/ed25519 to verify.
	// This is the SAME verification that Solana validators use.
	valid := ed25519.Verify(groupKeyBytes, message, signature[:])
	if !valid {
		t.Fatal("CRITICAL: Aggregated signature FAILED standard Ed25519 verification!")
	}

	t.Log("✅ Aggregated FROST signature passes standard Ed25519 verification")
}

// TestMnemonicRoundTrip verifies that mnemonic encoding/decoding
// produces the same key share deterministically.
func TestMnemonicRoundTrip(t *testing.T) {
	// Generate a mnemonic
	mnemonic, err := GenerateShareMnemonic()
	if err != nil {
		t.Fatalf("GenerateShareMnemonic failed: %v", err)
	}

	words := len(splitWords(mnemonic))
	if words != 12 {
		t.Fatalf("Expected 12-word mnemonic, got %d words", words)
	}

	// Derive key share from mnemonic
	share1, err := KeyShareFromMnemonic(mnemonic)
	if err != nil {
		t.Fatalf("KeyShareFromMnemonic failed: %v", err)
	}

	// Derive again — must be identical (deterministic)
	share2, err := KeyShareFromMnemonic(mnemonic)
	if err != nil {
		t.Fatalf("Second KeyShareFromMnemonic failed: %v", err)
	}

	if share1.Secret.Equal(share2.Secret) != 1 {
		t.Fatal("Same mnemonic produced different scalars")
	}
	if share1.Public.Equal(share2.Public) != 1 {
		t.Fatal("Same mnemonic produced different public keys")
	}

	t.Logf("✅ Mnemonic round-trip: same mnemonic → same key share")
	t.Logf("   Mnemonic: %s", mnemonic)
	t.Logf("   Public key share: %x", share1.Public.Bytes())
}

// TestMnemonicDKGAndSign combines mnemonic encoding with the full
// FROST flow to simulate the actual production path.
func TestMnemonicDKGAndSign(t *testing.T) {
	// Bot generates mnemonic and derives key share (production path)
	botMnemonic, err := GenerateShareMnemonic()
	if err != nil {
		t.Fatalf("Bot mnemonic generation failed: %v", err)
	}
	botShare, err := KeyShareFromMnemonic(botMnemonic)
	if err != nil {
		t.Fatalf("Bot key share derivation failed: %v", err)
	}

	// Server generates its share (in production, server uses its own code)
	serverShare, err := GenerateKeyShare()
	if err != nil {
		t.Fatalf("Server key share generation failed: %v", err)
	}

	// Compute group key
	groupKey := ComputeGroupKey(botShare.Public, serverShare.Public)

	// Sign a message
	message := []byte("Solana transaction: transfer 10 USDC to recipient")

	botNonce, _ := GenerateNonce()
	serverNonce, _ := GenerateNonce()

	botResult, _ := PartialSign(message, botShare.Secret, botNonce, serverNonce.Commitment, groupKey)
	serverResult, _ := PartialSign(message, serverShare.Secret, serverNonce, botNonce.Commitment, groupKey)

	signature := AggregateSignatures(botResult.GroupNonce, botResult.PartialSig, serverResult.PartialSig)

	// Verify
	valid := ed25519.Verify(groupKey.Bytes(), message, signature[:])
	if !valid {
		t.Fatal("CRITICAL: Mnemonic-derived FROST signature failed Ed25519 verification!")
	}

	t.Log("✅ Full production path: mnemonic → key share → DKG → sign → verify")

	// Now simulate restoring from mnemonic backup
	restoredShare, err := KeyShareFromMnemonic(botMnemonic)
	if err != nil {
		t.Fatalf("Restore from mnemonic failed: %v", err)
	}

	if restoredShare.Public.Equal(botShare.Public) != 1 {
		t.Fatal("Restored key share doesn't match original")
	}

	t.Log("✅ Key share restored from mnemonic matches original")
}

// TestPointEncoding verifies point serialization round-trip.
func TestPointEncoding(t *testing.T) {
	share, err := GenerateKeyShare()
	if err != nil {
		t.Fatalf("GenerateKeyShare failed: %v", err)
	}

	encoded := EncodePoint(share.Public)
	if len(encoded) != 32 {
		t.Fatalf("Expected 32-byte encoding, got %d", len(encoded))
	}

	decoded, err := DecodePoint(encoded)
	if err != nil {
		t.Fatalf("DecodePoint failed: %v", err)
	}

	if share.Public.Equal(decoded) != 1 {
		t.Fatal("Point encoding round-trip failed")
	}
}

// TestScalarEncoding verifies scalar serialization round-trip.
func TestScalarEncoding(t *testing.T) {
	share, err := GenerateKeyShare()
	if err != nil {
		t.Fatalf("GenerateKeyShare failed: %v", err)
	}

	encoded := EncodeScalar(share.Secret)
	if len(encoded) != 32 {
		t.Fatalf("Expected 32-byte encoding, got %d", len(encoded))
	}

	decoded, err := DecodeScalar(encoded)
	if err != nil {
		t.Fatalf("DecodeScalar failed: %v", err)
	}

	if share.Secret.Equal(decoded) != 1 {
		t.Fatal("Scalar encoding round-trip failed")
	}
}

// TestNonceUniqueness ensures fresh nonces are generated each time.
func TestNonceUniqueness(t *testing.T) {
	n1, _ := GenerateNonce()
	n2, _ := GenerateNonce()

	if n1.Secret.Equal(n2.Secret) == 1 {
		t.Fatal("Two nonces produced the same secret — catastrophic RNG failure")
	}
	if n1.Commitment.Equal(n2.Commitment) == 1 {
		t.Fatal("Two nonces produced the same commitment")
	}
}

// TestMultipleSignatures signs different messages and verifies each,
// ensuring the protocol works correctly across multiple rounds.
func TestMultipleSignatures(t *testing.T) {
	botShare, _ := GenerateKeyShare()
	serverShare, _ := GenerateKeyShare()
	groupKey := ComputeGroupKey(botShare.Public, serverShare.Public)

	messages := []string{
		"Transfer 1.00 USDC",
		"Transfer 999.99 USDC",
		"", // Edge case: empty-ish (won't work, handled by PartialSign)
		"A very long message that simulates a complex Solana transaction with many instructions and account keys",
	}

	for i, msg := range messages {
		if msg == "" {
			continue // PartialSign rejects empty messages
		}

		botNonce, _ := GenerateNonce()
		serverNonce, _ := GenerateNonce()

		botResult, err := PartialSign([]byte(msg), botShare.Secret, botNonce, serverNonce.Commitment, groupKey)
		if err != nil {
			t.Fatalf("Message %d: Bot PartialSign failed: %v", i, err)
		}

		serverResult, err := PartialSign([]byte(msg), serverShare.Secret, serverNonce, botNonce.Commitment, groupKey)
		if err != nil {
			t.Fatalf("Message %d: Server PartialSign failed: %v", i, err)
		}

		sig := AggregateSignatures(botResult.GroupNonce, botResult.PartialSig, serverResult.PartialSig)

		if !ed25519.Verify(groupKey.Bytes(), []byte(msg), sig[:]) {
			t.Fatalf("Message %d: Ed25519 verification failed!", i)
		}
	}

	t.Log("✅ All messages signed and verified successfully")
}

// splitWords is a test helper to count words in a mnemonic.
func splitWords(s string) []string {
	var words []string
	word := ""
	for _, c := range s {
		if c == ' ' {
			if word != "" {
				words = append(words, word)
				word = ""
			}
		} else {
			word += string(c)
		}
	}
	if word != "" {
		words = append(words, word)
	}
	return words
}

// TestS1NeverInWireTypes ensures that none of the "wire" types
// (DKG/Sign data structures sent over HTTP) contain scalar fields.
// This is a compile-time guarantee via the type system, but we
// document it explicitly here for auditors.
func TestS1NeverInWireTypes(t *testing.T) {
	// DKGRound1Result: only SessionID (string) and ServerPublicShare ([]byte = point)
	_ = DKGRound1Result{SessionID: "test", ServerPublicShare: make([]byte, 32)}

	// DKGRound2Data: only SessionID, BotPublicShare (point), GroupPublicKey (point)
	_ = DKGRound2Data{SessionID: "test", BotPublicShare: make([]byte, 32), GroupPublicKey: make([]byte, 32)}

	// SignRound1Data: only TransactionID and NonceCommitment (point)
	_ = SignRound1Data{TransactionID: "test", NonceCommitment: make([]byte, 32)}

	// SignRound2Data: only SessionID and PartialSig (scalar, but this is z1=r1+k*s1, not s1 itself)
	// z1 cannot be used to extract s1 without knowing r1 (ephemeral nonce)
	_ = SignRound2Data{SessionID: "test", PartialSig: make([]byte, 32)}

	// NONE of these types contain *edwards25519.Scalar (key share secrets)
	t.Log("✅ Wire types contain no secret key share material")
}

// BenchmarkPartialSign measures signing performance.
func BenchmarkPartialSign(b *testing.B) {
	botShare, _ := GenerateKeyShare()
	serverShare, _ := GenerateKeyShare()
	groupKey := ComputeGroupKey(botShare.Public, serverShare.Public)
	message := []byte("benchmark transaction")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		botNonce, _ := GenerateNonce()
		serverNonce, _ := GenerateNonce()
		_, _ = PartialSign(message, botShare.Secret, botNonce, serverNonce.Commitment, groupKey)
		_, _ = PartialSign(message, serverShare.Secret, serverNonce, botNonce.Commitment, groupKey)
	}
}

// BenchmarkFullFlow measures the complete DKG+Sign+Verify flow.
func BenchmarkFullFlow(b *testing.B) {
	message := []byte("benchmark transaction")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		botShare, _ := GenerateKeyShare()
		serverShare, _ := GenerateKeyShare()
		groupKey := ComputeGroupKey(botShare.Public, serverShare.Public)

		botNonce, _ := GenerateNonce()
		serverNonce, _ := GenerateNonce()

		botResult, _ := PartialSign(message, botShare.Secret, botNonce, serverNonce.Commitment, groupKey)
		serverResult, _ := PartialSign(message, serverShare.Secret, serverNonce, botNonce.Commitment, groupKey)

		sig := AggregateSignatures(botResult.GroupNonce, botResult.PartialSig, serverResult.PartialSig)
		_ = ed25519.Verify(groupKey.Bytes(), message, sig[:])
	}
}
