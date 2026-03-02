// =============================================================================
// Botwallet CLI - Solana Keypair Management
// =============================================================================
// Handles BIP39 mnemonic generation and Solana keypair derivation.
// Used during FROST DKG for share encoding and public key operations.
//
// Uses well-audited packages:
// - github.com/tyler-smith/go-bip39 (BIP39 standard)
// - github.com/gagliardetto/solana-go (Solana SDK)
// =============================================================================

package solana

import (
	"crypto/ed25519"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/tyler-smith/go-bip39"
)

// Keypair represents a Solana keypair with its mnemonic
type Keypair struct {
	PublicKey  solana.PublicKey  // Solana public key (deposit address)
	PrivateKey solana.PrivateKey // 64-byte ed25519 private key
	Mnemonic   string            // BIP39 mnemonic (12 or 24 words)
}

// =============================================================================
// Keypair Generation
// =============================================================================

// GenerateKeypair creates a new Solana keypair with a 24-word mnemonic.
// NOTE: For FROST wallets, use the frost package DKG instead.
// This is kept for utility/testing purposes.
func GenerateKeypair() (*Keypair, error) {
	entropy, err := bip39.NewEntropy(256)
	if err != nil {
		return nil, fmt.Errorf("failed to generate entropy: %w", err)
	}

	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return nil, fmt.Errorf("failed to generate mnemonic: %w", err)
	}

	return KeypairFromMnemonic(mnemonic)
}

// KeypairFromMnemonic derives a Solana keypair from a BIP39 mnemonic.
// Supports both 12-word (128-bit) and 24-word (256-bit) mnemonics.
func KeypairFromMnemonic(mnemonic string) (*Keypair, error) {
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, fmt.Errorf("invalid mnemonic phrase")
	}

	// BIP39 standard: PBKDF2(mnemonic, "mnemonic" + passphrase)
	seed := bip39.NewSeed(mnemonic, "")

	// Ed25519 keypair from first 32 bytes of seed
	privateKey := ed25519.NewKeyFromSeed(seed[:32])

	return &Keypair{
		PublicKey:  solana.PublicKeyFromBytes(privateKey.Public().(ed25519.PublicKey)),
		PrivateKey: solana.PrivateKey(privateKey),
		Mnemonic:   mnemonic,
	}, nil
}

// =============================================================================
// Public Key Helpers
// =============================================================================

// ValidatePublicKey checks if a string is a valid Solana public key
func ValidatePublicKey(pubkey string) bool {
	_, err := solana.PublicKeyFromBase58(pubkey)
	return err == nil
}
