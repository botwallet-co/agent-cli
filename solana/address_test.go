package solana

import (
	"crypto/sha256"
	"strings"
	"testing"

	solanago "github.com/gagliardetto/solana-go"

	"github.com/botwallet-co/agent-cli/solana/txcheck/txchecktest"
)

// testKey is a made-up 32-byte public key.
func testKey(name string) solanago.PublicKey {
	h := sha256.Sum256([]byte(name))
	return solanago.PublicKeyFromBytes(h[:])
}

func TestIsValidAddress(t *testing.T) {
	wallet := testKey("wallet")
	usdcAccount := txchecktest.ATA(wallet, solanago.MustPublicKeyFromBase58(USDCMintMainnet))
	if solanago.IsOnCurve(usdcAccount[:]) {
		t.Fatal("test setup: the associated token account should be off the curve")
	}

	cases := []struct {
		addr string
		want bool
	}{
		{wallet.String(), true},
		{usdcAccount.String(), true}, // off-curve addresses (vaults, escrows) stay allowed
		{"11111111111111111111111111111111", true},
		{strings.Repeat("z", 44), false}, // base58, but 33 bytes
		{strings.Repeat("2", 32), false}, // base58, but fewer than 32 bytes
		{wallet.String()[:31], false},
		{wallet.String()[:30] + "0OIl", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsValidAddress(c.addr); got != c.want {
			t.Errorf("IsValidAddress(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}

// The deposit address of a new wallet is its group key in base58. It must
// decode back to the same 32 bytes, also for keys with leading zero bytes.
func TestAddressStringRoundTrip(t *testing.T) {
	for zeros := 0; zeros <= 32; zeros += 4 {
		key := testKey("key")
		for i := 0; i < zeros && i < len(key); i++ {
			key[i] = 0
		}
		addr := AddressString(key[:])
		back, err := solanago.PublicKeyFromBase58(addr)
		if err != nil || back != key {
			t.Errorf("%d leading zero bytes: %s decodes to %v, %v", zeros, addr, back, err)
		}
	}
	if got := AddressString([]byte{1, 2, 3}); !strings.Contains(got, "not an address") {
		t.Errorf("AddressString of 3 bytes = %q", got)
	}
}
