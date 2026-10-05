package txcheck

import (
	"crypto/sha256"
	"testing"

	solanago "github.com/gagliardetto/solana-go"

	"github.com/botwallet-co/agent-cli/solana"
	"github.com/botwallet-co/agent-cli/solana/txcheck/txchecktest"
)

var (
	mint          = solanago.MustPublicKeyFromBase58(solana.USDCMintMainnet)
	devnetMint    = solanago.MustPublicKeyFromBase58(solana.USDCMintDevnet)
	feePayer      = testKey("fee payer")
	wallet        = testKey("agent wallet")
	recipient     = testKey("recipient")
	feeCollection = testKey("fee collection")
	attacker      = testKey("attacker")
	otherAccount  = testKey("recipient's other USDC account")
	memoProgram   = solanago.MemoProgramID
	token2022     = testKey("another token program") // stands in for Token-2022
)

func testKey(name string) solanago.PublicKey {
	h := sha256.Sum256([]byte(name))
	return solanago.PublicKeyFromBytes(h[:])
}

// serverPayment is a 10 USDC payment with a 0.25 USDC fee, both token
// accounts already open: the most common confirm.
func serverPayment() txchecktest.Payment {
	return txchecktest.Payment{
		FeePayer:               feePayer,
		Wallet:                 wallet,
		Source:                 txchecktest.ATA(wallet, mint),
		Recipient:              recipient,
		RecipientAccount:       txchecktest.ATA(recipient, mint),
		RecipientAccountExists: true,
		FeeCollection:          feeCollection,
		FeeAccount:             txchecktest.ATA(feeCollection, mint),
		FeeAccountExists:       true,
		Mint:                   mint,
		Amount:                 10_000_000,
		Fee:                    250_000,
		Blockhash:              solanago.Hash(testKey("blockhash")),
	}
}

// expectation is what the confirm response for serverPayment says.
func expectation() Expectation {
	return Expectation{
		Wallet:    wallet,
		Mint:      mint,
		Recipient: recipient,
		MinAmount: 10_000_000,
		MaxAmount: 10_000_000,
		MaxFee:    250_000,
	}
}

func TestVerifyReportsTheTransfers(t *testing.T) {
	p := serverPayment()
	checked, err := Verify(txchecktest.BuildPayment(p).Bytes(), expectation())
	if err != nil {
		t.Fatal(err)
	}
	if checked.FeePayer != feePayer {
		t.Errorf("FeePayer = %s", checked.FeePayer)
	}
	want := Transfer{Source: p.Source, Destination: p.RecipientAccount, Amount: p.Amount}
	if checked.Payment != want {
		t.Errorf("Payment = %+v, want %+v", checked.Payment, want)
	}
	if checked.Fee == nil || *checked.Fee != (Transfer{Source: p.Source, Destination: p.FeeAccount, Amount: p.Fee}) {
		t.Errorf("Fee = %+v", checked.Fee)
	}

	p.Fee = 0
	checked, err = Verify(txchecktest.BuildTransfer(p).Bytes(), expectation())
	if err != nil {
		t.Fatal(err)
	}
	if checked.Fee != nil {
		t.Errorf("Fee = %+v, want none", checked.Fee)
	}
}

func TestFormatUSDC(t *testing.T) {
	for units, want := range map[uint64]string{
		0:          "0.00",
		10_000:     "0.01",
		250_000:    "0.25",
		10_000_000: "10.00",
		10_000_001: "10.000001",
		1_234_500:  "1.2345",
	} {
		if got := FormatUSDC(units); got != want {
			t.Errorf("FormatUSDC(%d) = %q, want %q", units, got, want)
		}
	}
}
