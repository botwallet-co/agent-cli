package solana

import (
	"testing"
)

func TestKeypairFromMnemonic(t *testing.T) {
	// Standard BIP39 test vector (24 words) — never used on any real system
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"

	// Expected public key derived from this mnemonic
	expectedPubKey := "31fsSBAugfgtWp4WZLgr1D9TBkgiS13d5eK3GWBQwRct"

	// Derive keypair
	kp, err := KeypairFromMnemonic(mnemonic)
	if err != nil {
		t.Fatalf("Failed to derive keypair: %v", err)
	}

	// Verify public key matches
	actualPubKey := kp.PublicKey.String()
	if actualPubKey != expectedPubKey {
		t.Errorf("Public key mismatch!\nExpected: %s\nGot: %s", expectedPubKey, actualPubKey)
	} else {
		t.Logf("✅ Mnemonic correctly derives public key: %s", actualPubKey)
	}
}

func TestGenerateKeypairUniqueness(t *testing.T) {
	// Generate two keypairs and verify they're different
	kp1, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("Failed to generate keypair 1: %v", err)
	}

	kp2, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("Failed to generate keypair 2: %v", err)
	}

	if kp1.PublicKey.String() == kp2.PublicKey.String() {
		t.Error("Generated keypairs should be unique!")
	} else {
		t.Logf("✅ Two generated keypairs are unique")
		t.Logf("   Key 1: %s", kp1.PublicKey.String())
		t.Logf("   Key 2: %s", kp2.PublicKey.String())
	}
}

func TestMnemonicValidation(t *testing.T) {
	// Test invalid mnemonic
	_, err := KeypairFromMnemonic("invalid mnemonic words")
	if err == nil {
		t.Error("Should reject invalid mnemonic")
	} else {
		t.Logf("✅ Invalid mnemonic correctly rejected: %v", err)
	}

	// Test valid mnemonic with wrong word count
	_, err = KeypairFromMnemonic("abandon abandon abandon")
	if err == nil {
		t.Error("Should reject mnemonic with wrong word count")
	} else {
		t.Logf("✅ Short mnemonic correctly rejected: %v", err)
	}
}

func TestPublicKeyValidation(t *testing.T) {
	tests := []struct {
		name   string
		pubkey string
		valid  bool
	}{
		{"Valid Solana address", "31fsSBAugfgtWp4WZLgr1D9TBkgiS13d5eK3GWBQwRct", true},
		{"Valid USDC mint", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", true},
		{"Too short", "Fn1WqPnd", false},
		{"Invalid chars (0, O, I, l)", "0OIl1234567890123456789012345678901234", false},
		{"Empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidatePublicKey(tt.pubkey)
			if result != tt.valid {
				t.Errorf("ValidatePublicKey(%q) = %v, want %v", tt.pubkey, result, tt.valid)
			}
		})
	}
}
