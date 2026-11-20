// =============================================================================
// Botwallet — FROST Types
// =============================================================================
// Core data types for FROST 2-of-2 threshold signatures.
//
// SECURITY NOTE ON TYPES:
//   - Types containing *edwards25519.Scalar hold SECRET material.
//     These must NEVER be serialized to JSON, logged, or sent over the network.
//   - Types containing *edwards25519.Point hold PUBLIC material.
//     These are safe to transmit.
//
// The naming convention makes this explicit:
//   - "Secret" fields → NEVER leave local memory
//   - "Public" / "Commitment" fields → safe to share
// =============================================================================

package frost

import "filippo.io/edwards25519"

// KeyShare represents one party's share of the FROST group key.
//
// For the bot (S1):
//
//	Secret = the bot's key share scalar (NEVER leaves this machine)
//	Public = Secret * G (sent to server during DKG)
//
// For the server (S2):
//
//	Secret = the server's key share scalar (NEVER sent to CLI)
//	Public = Secret * G (sent to CLI during DKG)
type KeyShare struct {
	Secret *edwards25519.Scalar // SECRET: the key share scalar
	Public *edwards25519.Point  // PUBLIC: the corresponding point (Secret * G)
}

// SigningNonce holds the ephemeral nonce for one FROST signing round.
//
// CRITICAL: A nonce MUST be used exactly once. Reusing a nonce across
// two different messages allows extraction of the key share.
//
// The Secret is used in PartialSign and then MUST be discarded.
// The Commitment (= Secret * G) is sent to the other party.
type SigningNonce struct {
	Secret     *edwards25519.Scalar // SECRET: ephemeral nonce scalar (use once, then discard)
	Commitment *edwards25519.Point  // PUBLIC: nonce commitment (Secret * G), safe to send
}

// PartialSignResult holds the output of a partial signing operation.
type PartialSignResult struct {
	PartialSig *edwards25519.Scalar // PUBLIC: partial signature (safe to send to server)
	GroupNonce *edwards25519.Point  // PUBLIC: R = R1 + R2 (the combined nonce point)
	Challenge  *edwards25519.Scalar // PUBLIC: k = H(R||A||M) (the Ed25519 challenge)
}
