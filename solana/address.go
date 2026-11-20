package solana

import (
	"fmt"
	"strings"

	solanago "github.com/gagliardetto/solana-go"
)

// base58Alphabet is the Bitcoin base58 alphabet Solana addresses use: no 0,
// O, I or l.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// LooksLikeAddress reports whether s has the length and characters of a
// Solana address. The server treats such a recipient as an address, not a
// username.
func LooksLikeAddress(s string) bool {
	if len(s) < 32 || len(s) > 44 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune(base58Alphabet, c) {
			return false
		}
	}
	return true
}

// IsValidAddress reports whether addr looks like a Solana address and
// decodes to exactly 32 bytes. Addresses off the ed25519 curve are allowed:
// program-owned vaults (multisigs, escrows) are valid destinations. Telling
// a token account from a wallet takes a Solana lookup, so that check is the
// server's.
func IsValidAddress(addr string) bool {
	if !LooksLikeAddress(addr) {
		return false
	}
	_, err := solanago.PublicKeyFromBase58(addr)
	return err == nil
}

// AddressString returns the base58 Solana address of a 32-byte public key.
// Anything else is not an address and is shown in hex.
func AddressString(key []byte) string {
	if len(key) != solanago.PublicKeyLength {
		return fmt.Sprintf("%x (%d bytes, not an address)", key, len(key))
	}
	return solanago.PublicKeyFromBytes(key).String()
}
