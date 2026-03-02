// Package frost implements FROST 2-of-2 threshold signatures for Ed25519.
//
// This file handles key share encoding: converting between FROST key share
// scalars and 12-word BIP39 mnemonics.
//
// ENCODING SCHEME:
//  1. Generate 16 bytes (128 bits) of cryptographic randomness
//  2. Encode as 12-word BIP39 mnemonic (standard, human-readable)
//  3. Derive the Ed25519 scalar via SHA-512(entropy || domain) → reduce mod l
//
// WHY 12 WORDS: 128-bit security (AES-128 level). The full ~252-bit scalar
// is derived via SHA-512 expansion, so no entropy is lost.
//
// DETERMINISM: Same mnemonic always produces the same scalar. The .seed file
// IS the complete key share — no hidden state.
//
// SECURITY: The mnemonic IS the key share. The domain separator
// "botwallet/frost/v1/key-share" prevents cross-protocol reuse attacks.
package frost

import (
	"crypto/sha512"
	"fmt"

	"filippo.io/edwards25519"
	"github.com/tyler-smith/go-bip39"
)

// GenerateShareMnemonic creates a new random 12-word mnemonic suitable
// for encoding a FROST key share.
//
// Returns the mnemonic string (12 space-separated words).
// The caller must save this to the seed file and NEVER print it to stdout.
func GenerateShareMnemonic() (string, error) {
	// 128 bits of entropy → 12-word BIP39 mnemonic
	entropy, err := bip39.NewEntropy(128)
	if err != nil {
		return "", fmt.Errorf("frost: failed to generate entropy: %w", err)
	}

	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", fmt.Errorf("frost: failed to encode mnemonic: %w", err)
	}

	return mnemonic, nil
}

// ScalarFromMnemonic derives a FROST key share scalar from a 12-word
// BIP39 mnemonic.
//
// Process:
//  1. Decode mnemonic → 16 bytes of entropy
//  2. SHA-512(entropy || domain_separator) → 64 bytes
//  3. Reduce mod l → uniform scalar
//
// This is deterministic: the same mnemonic always yields the same scalar.
func ScalarFromMnemonic(mnemonic string) (*edwards25519.Scalar, error) {
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, fmt.Errorf("frost: invalid mnemonic")
	}

	// Decode mnemonic to raw entropy bytes (16 bytes for 12-word mnemonic).
	entropy, err := bip39.EntropyFromMnemonic(mnemonic)
	if err != nil {
		return nil, fmt.Errorf("frost: failed to decode mnemonic: %w", err)
	}

	return ScalarFromEntropy(entropy)
}

// ScalarFromEntropy derives a FROST key share scalar from raw entropy bytes.
//
// This is the core derivation function:
//
//	SHA-512(entropy || "botwallet/frost/v1/key-share") → reduce mod l
func ScalarFromEntropy(entropy []byte) (*edwards25519.Scalar, error) {
	if len(entropy) < 16 {
		return nil, fmt.Errorf("frost: entropy too short (%d bytes, need ≥16)", len(entropy))
	}

	// Hash with domain separator to get 64 uniform bytes.
	// The domain separator ensures this derivation is unique to Botwallet FROST.
	h := sha512.New()
	h.Write(entropy)
	h.Write([]byte(domainSeparator))
	digest := h.Sum(nil) // 64 bytes

	// Reduce mod l to get a uniform scalar in [0, l-1].
	// SetUniformBytes requires exactly 64 bytes and performs proper reduction.
	scalar, err := new(edwards25519.Scalar).SetUniformBytes(digest)
	if err != nil {
		return nil, fmt.Errorf("frost: failed to reduce scalar: %w", err)
	}

	return scalar, nil
}

// KeyShareFromMnemonic derives a complete KeyShare (secret + public point)
// from a 12-word mnemonic.
//
// This is the function called when loading a key share from the seed file.
//
// SECURITY: The returned KeyShare.Secret must be handled with the same
// care as any private key material.
func KeyShareFromMnemonic(mnemonic string) (*KeyShare, error) {
	secret, err := ScalarFromMnemonic(mnemonic)
	if err != nil {
		return nil, err
	}

	public := new(edwards25519.Point).ScalarBaseMult(secret)

	return &KeyShare{
		Secret: secret,
		Public: public,
	}, nil
}

// EncodePoint encodes an Ed25519 point to 32 bytes (compressed form).
// This is the standard Ed25519 point encoding used by Solana.
func EncodePoint(p *edwards25519.Point) []byte {
	return p.Bytes()
}

// DecodePoint decodes a 32-byte compressed Ed25519 point.
// Returns an error if the bytes don't represent a valid curve point.
func DecodePoint(b []byte) (*edwards25519.Point, error) {
	if len(b) != 32 {
		return nil, fmt.Errorf("frost: point must be 32 bytes, got %d", len(b))
	}
	p, err := new(edwards25519.Point).SetBytes(b)
	if err != nil {
		return nil, fmt.Errorf("frost: invalid point encoding: %w", err)
	}
	return p, nil
}

// EncodeScalar encodes an Ed25519 scalar to 32 bytes (little-endian).
func EncodeScalar(s *edwards25519.Scalar) []byte {
	return s.Bytes()
}

// DecodeScalar decodes a 32-byte little-endian Ed25519 scalar.
// Returns an error if the bytes don't represent a canonical scalar.
func DecodeScalar(b []byte) (*edwards25519.Scalar, error) {
	if len(b) != 32 {
		return nil, fmt.Errorf("frost: scalar must be 32 bytes, got %d", len(b))
	}
	s, err := new(edwards25519.Scalar).SetCanonicalBytes(b)
	if err != nil {
		return nil, fmt.Errorf("frost: invalid scalar encoding: %w", err)
	}
	return s, nil
}
