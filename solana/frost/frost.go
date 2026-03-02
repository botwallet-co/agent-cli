// FROST 2-of-2 threshold signatures for Ed25519/Solana.
//
// SECURITY MODEL:
//   - Bot holds S1, server holds S2. Full key s = s1+s2 is NEVER constructed.
//   - Signing produces partial sigs that aggregate into a standard Ed25519 sig.
//   - Solana validators cannot distinguish FROST-signed from conventional.
//
// DKG:
//  1. Server generates s2, sends A2 = s2*G
//  2. CLI generates s1, computes A1 = s1*G, group key A = A1+A2
//  3. CLI sends A1; server verifies A = A1+A2; saves s1 locally
//
// Signing:
//  1. Exchange nonce commitments R1, R2
//  2. Both compute R = R1+R2, challenge k = H(R||A||M)
//  3. CLI sends z1 = r1 + k*s1; server aggregates z = z1+z2
//  4. Final signature: (R, z)
package frost

import (
	"crypto/rand"
	"crypto/sha512"
	"fmt"

	"filippo.io/edwards25519"
)

// Ed25519 group order: l = 2^252 + 27742317777372353535851937790883648493
// All scalar arithmetic is performed mod l by the edwards25519 package.

// domainSeparator prevents cross-protocol attacks when deriving scalars.
// Changing this would produce different key shares from the same entropy.
const domainSeparator = "botwallet/frost/v1/key-share"

// GenerateKeyShare creates a new random key share.
//
// SECURITY: The returned KeyShare.Secret is the most sensitive value in
// the entire system. It must be:
//   - Saved to local disk immediately (via encoding.go → mnemonic → seed file)
//   - NEVER serialized into API requests
//   - NEVER printed to stdout/stderr
//   - NEVER logged
//
// The returned KeyShare.Public (= Secret * G) is safe to send to the server.
func GenerateKeyShare() (*KeyShare, error) {
	// Generate 64 bytes of cryptographic randomness.
	// We use 64 bytes (not 32) because SetUniformBytes requires exactly 64
	// bytes and reduces mod l to produce a uniformly random scalar.
	// This avoids modular bias that would occur with 32-byte reduction.
	randomBytes := make([]byte, 64)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("frost: failed to generate randomness: %w", err)
	}

	// Create scalar from random bytes (uniform reduction mod l).
	secret, err := new(edwards25519.Scalar).SetUniformBytes(randomBytes)
	if err != nil {
		return nil, fmt.Errorf("frost: failed to create scalar: %w", err)
	}

	// Compute public key share: Public = Secret * G (base point multiplication).
	public := new(edwards25519.Point).ScalarBaseMult(secret)

	return &KeyShare{
		Secret: secret,
		Public: public,
	}, nil
}

// ComputeGroupKey computes the combined FROST group public key from two
// public key shares: A = A1 + A2.
//
// This is the Solana deposit address. It's a standard Ed25519 public key
// that validators treat normally — they cannot tell it's a FROST key.
//
// SECURITY: Only public values are involved. Safe to call anywhere.
func ComputeGroupKey(ourPublic, theirPublic *edwards25519.Point) *edwards25519.Point {
	return new(edwards25519.Point).Add(ourPublic, theirPublic)
}

// =============================================================================
// SIGNING — ROUND 1: NONCE GENERATION
// =============================================================================

// GenerateNonce creates a random signing nonce for one round of FROST signing.
//
// SECURITY: The nonce secret (r) is as sensitive as the key share during
// the signing round. If r is reused across two different messages, an
// attacker can extract the key share. The nonce MUST be:
//   - Generated fresh for EVERY signing operation
//   - NEVER persisted to disk
//   - NEVER reused
//   - Discarded immediately after PartialSign completes
//
// The returned NonceCommitment (R = r*G) is safe to send to the server.
func GenerateNonce() (*SigningNonce, error) {
	// Generate 64 uniform random bytes for nonce scalar.
	randomBytes := make([]byte, 64)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("frost: failed to generate nonce randomness: %w", err)
	}

	// Reduce to a uniform scalar mod l.
	secret, err := new(edwards25519.Scalar).SetUniformBytes(randomBytes)
	if err != nil {
		return nil, fmt.Errorf("frost: failed to create nonce scalar: %w", err)
	}

	// Nonce commitment: R = r * G
	commitment := new(edwards25519.Point).ScalarBaseMult(secret)

	return &SigningNonce{
		Secret:     secret,
		Commitment: commitment,
	}, nil
}

// =============================================================================
// SIGNING — ROUND 2: PARTIAL SIGNATURE
// =============================================================================

// PartialSign computes this party's partial signature for a message.
//
// Inputs:
//   - message:          the Solana transaction message bytes (what we're signing)
//   - keyShare:         our key share secret (s1)
//   - nonce:            our nonce for this round (r1, from GenerateNonce)
//   - ourCommitment:    our nonce commitment (R1 = r1*G)
//   - theirCommitment:  the other party's nonce commitment (R2)
//   - groupKey:         the combined public key (A = A1 + A2)
//
// Returns:
//   - partialSig: z1 = r1 + k * s1 (mod l)
//   - groupNonce: R = R1 + R2 (needed for verification and aggregation)
//
// SECURITY:
//   - The partial signature z1 is safe to send to the server. It cannot
//     be used to extract s1 without knowing r1, which is ephemeral.
//   - After calling PartialSign, the nonce MUST be discarded.
//   - NEVER call PartialSign twice with the same nonce.
//
// The Ed25519 challenge is computed as:
//
//	k = SHA-512(R_bytes || A_bytes || message) mod l
//
// This matches the standard Ed25519 signing equation, making the
// aggregated signature indistinguishable from a normal Ed25519 signature.
func PartialSign(
	message []byte,
	keyShare *edwards25519.Scalar,
	nonce *SigningNonce,
	theirCommitment *edwards25519.Point,
	groupKey *edwards25519.Point,
) (*PartialSignResult, error) {

	if message == nil || len(message) == 0 {
		return nil, fmt.Errorf("frost: message cannot be empty")
	}

	// Step 1: Compute group nonce R = R1 + R2 (our commitment + their commitment).
	groupNonce := new(edwards25519.Point).Add(nonce.Commitment, theirCommitment)

	// Step 2: Compute Ed25519 challenge.
	//   k = SHA-512(R || A || M) mod l
	// This is identical to how standard Ed25519 computes the challenge,
	// ensuring the final signature passes Ed25519 verification.
	challenge, err := computeChallenge(groupNonce, groupKey, message)
	if err != nil {
		return nil, fmt.Errorf("frost: %w", err)
	}

	// Step 3: Compute partial signature.
	//   z1 = r1 + k * s1 (mod l)
	//
	// Breakdown:
	//   k_times_s1 = k * s1          — challenge scaled by our key share
	//   z1 = r1 + k_times_s1         — add our nonce secret
	kTimesS := new(edwards25519.Scalar).Multiply(challenge, keyShare)
	partialSig := new(edwards25519.Scalar).Add(nonce.Secret, kTimesS)

	return &PartialSignResult{
		PartialSig: partialSig,
		GroupNonce: groupNonce,
		Challenge:  challenge,
	}, nil
}

// VerifyPartialSig verifies a partial signature from the other party.
//
// Checks: z_i * G == R_i + k * A_i
//
// This ensures the other party used their actual key share and the
// correct nonce. If this check fails, the aggregated signature would
// be invalid — catching bugs or malicious behavior early.
//
// SECURITY: Only uses public values. Safe to call anywhere.
func VerifyPartialSig(
	partialSig *edwards25519.Scalar,
	theirCommitment *edwards25519.Point,
	theirPublicShare *edwards25519.Point,
	challenge *edwards25519.Scalar,
) bool {
	// Left side: z_i * G
	lhs := new(edwards25519.Point).ScalarBaseMult(partialSig)

	// Right side: R_i + k * A_i
	kTimesA := new(edwards25519.Point).ScalarMult(challenge, theirPublicShare)
	rhs := new(edwards25519.Point).Add(theirCommitment, kTimesA)

	// Compare: z_i * G == R_i + k * A_i
	return lhs.Equal(rhs) == 1
}

// AggregateSignatures combines two partial signatures into a final
// Ed25519 signature: (R, z) where z = z1 + z2 (mod l).
//
// The result is a standard 64-byte Ed25519 signature:
//
//	bytes[0:32]  = R (compressed point, little-endian)
//	bytes[32:64] = z (scalar, little-endian)
//
// SECURITY: The aggregated signature is public (it goes on-chain).
func AggregateSignatures(
	groupNonce *edwards25519.Point,
	partialSig1 *edwards25519.Scalar,
	partialSig2 *edwards25519.Scalar,
) [64]byte {
	// z = z1 + z2 (mod l)
	z := new(edwards25519.Scalar).Add(partialSig1, partialSig2)

	// Encode as 64-byte Ed25519 signature: R || z
	var signature [64]byte
	copy(signature[:32], groupNonce.Bytes())
	copy(signature[32:], z.Bytes())

	return signature
}

// =============================================================================
// INTERNAL: Ed25519 Challenge Hash
// =============================================================================

// computeChallenge computes the Ed25519 challenge scalar:
//
//	k = SHA-512(R_bytes || A_bytes || message) mod l
//
// This matches the standard Ed25519 signing equation exactly.
func computeChallenge(
	R *edwards25519.Point,
	A *edwards25519.Point,
	message []byte,
) (*edwards25519.Scalar, error) {
	// SHA-512(R || A || M) — standard Ed25519 challenge computation.
	h := sha512.New()
	h.Write(R.Bytes())   // 32 bytes: compressed nonce point
	h.Write(A.Bytes())   // 32 bytes: compressed public key
	h.Write(message)     // variable: the message being signed
	digest := h.Sum(nil) // 64 bytes

	// Reduce the 64-byte hash mod l to get a uniform scalar.
	// SetUniformBytes handles the mod-l reduction correctly.
	k, err := new(edwards25519.Scalar).SetUniformBytes(digest)
	if err != nil {
		return nil, fmt.Errorf("failed to reduce challenge hash: %w", err)
	}
	return k, nil
}
