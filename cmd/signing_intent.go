package cmd

import (
	"math"

	solanago "github.com/gagliardetto/solana-go"

	"github.com/botwallet-co/agent-cli/output"
	"github.com/botwallet-co/agent-cli/solana"
	"github.com/botwallet-co/agent-cli/solana/txcheck"
)

// centUnits is one cent of USDC in base units.
const centUnits = 10_000

// signingIntent is what a confirm response says the transaction does. Key 1
// signs only a message that does exactly this (see package txcheck).
type signingIntent struct {
	network   string // cluster the server reports, e.g. "mainnet-beta"
	recipient string // wallet (owner) address that receives the payment
	minAmount uint64 // payment amount range, in base units
	maxAmount uint64
	maxFee    uint64 // in base units
}

// paymentIntent reads a pay or withdraw confirm response. The server moves
// exactly amount_usdc to to_address and fee_usdc to Botwallet, in whole cents.
func paymentIntent(confirm map[string]interface{}) (*signingIntent, error) {
	intent, err := intentFromConfirm(confirm)
	if err != nil {
		return nil, err
	}
	intent.minAmount = intent.maxAmount
	return intent, nil
}

// x402Intent reads an x402 confirm response. The API prices in base units:
// amount_usdc is that price rounded up to a whole cent, so the payment can be
// up to one base unit short of a cent below amount_usdc. Servers before Oct
// 2026 charged their percentage fee on the base units, up to a cent above
// fee_usdc.
func x402Intent(confirm map[string]interface{}) (*signingIntent, error) {
	intent, err := intentFromConfirm(confirm)
	if err != nil {
		return nil, err
	}
	if intent.maxAmount >= centUnits-1 {
		intent.minAmount = intent.maxAmount - (centUnits - 1)
	}
	intent.maxFee += centUnits
	return intent, nil
}

func intentFromConfirm(confirm map[string]interface{}) (*signingIntent, error) {
	recipient, _ := confirm["to_address"].(string)
	if recipient == "" {
		return nil, txcheck.Refuse("the server did not say which address receives this payment")
	}
	amountUSDC, ok := confirm["amount_usdc"].(float64)
	if !ok {
		return nil, txcheck.Refuse("the server did not say how much this payment is")
	}
	amount, err := usdcToUnits(amountUSDC)
	if err != nil {
		return nil, err
	}
	feeUSDC, _ := confirm["fee_usdc"].(float64) // no fee_usdc: no fee may be charged
	fee, err := usdcToUnits(feeUSDC)
	if err != nil {
		return nil, err
	}
	network, _ := confirm["network"].(string)
	return &signingIntent{network: network, recipient: recipient, maxAmount: amount, maxFee: fee}, nil
}

// usdcToUnits converts a whole-cent USDC amount from the API to base units.
func usdcToUnits(usdc float64) (uint64, error) {
	if math.IsNaN(usdc) || usdc < 0 || usdc > 1e12 {
		return 0, txcheck.Refuse("the server sent an amount that is not valid (%v)", usdc)
	}
	return uint64(math.Round(usdc*100)) * centUnits, nil
}

// checkMessage refuses message unless it is the payment intent describes,
// sent from wallet.
func checkMessage(message []byte, wallet solanago.PublicKey, intent *signingIntent) error {
	mint, ok := solana.USDCMint(intent.network)
	if !ok {
		return txcheck.Refuse("the server reports network %q, which this CLI does not pay on", intent.network)
	}
	recipient, err := solanago.PublicKeyFromBase58(intent.recipient)
	if err != nil {
		return txcheck.Refuse("the server sent a recipient address that is not valid (%q)", intent.recipient)
	}
	checked, err := txcheck.Verify(message, txcheck.Expectation{
		Wallet:    wallet,
		Mint:      solanago.MustPublicKeyFromBase58(mint),
		Recipient: recipient,
		MinAmount: intent.minAmount,
		MaxAmount: intent.maxAmount,
		MaxFee:    intent.maxFee,
	})
	if err != nil {
		return err
	}
	if output.IsHumanOutput() {
		fee := "no fee"
		if checked.Fee != nil {
			fee = "a " + txcheck.FormatUSDC(checked.Fee.Amount) + " USDC fee"
		}
		output.InfoMsg("Checked the transaction: %s USDC to %s, plus %s.",
			txcheck.FormatUSDC(checked.Payment.Amount), intent.recipient, fee)
	}
	return nil
}
