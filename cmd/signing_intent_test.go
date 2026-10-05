package cmd

import (
	"errors"
	"testing"

	"github.com/botwallet-co/agent-cli/solana/txcheck"
)

func TestSigningIntents(t *testing.T) {
	confirm := func(fields map[string]interface{}) map[string]interface{} {
		c := map[string]interface{}{
			"to_address":  "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
			"amount_usdc": 10.07,
			"fee_usdc":    0.29,
			"network":     "mainnet-beta",
		}
		for k, v := range fields {
			if v == nil {
				delete(c, k)
			} else {
				c[k] = v
			}
		}
		return c
	}

	got, err := paymentIntent(confirm(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := signingIntent{network: "mainnet-beta", recipient: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
		minAmount: 10_070_000, maxAmount: 10_070_000, maxFee: 290_000}
	if *got != want {
		t.Errorf("paymentIntent = %+v, want %+v", *got, want)
	}

	// x402: the API's base units may be up to a cent below the rounded-up
	// amount, and the percentage fee on them up to a cent above the quote.
	got, err = x402Intent(confirm(map[string]interface{}{"amount_usdc": 0.01, "fee_usdc": 0.01}))
	if err != nil {
		t.Fatal(err)
	}
	if got.minAmount != 1 || got.maxAmount != 10_000 || got.maxFee != 20_000 {
		t.Errorf("x402Intent = %+v", *got)
	}

	// No fee_usdc means no fee may be charged.
	if got, err = paymentIntent(confirm(map[string]interface{}{"fee_usdc": nil})); err != nil || got.maxFee != 0 {
		t.Errorf("without fee_usdc: %+v, %v", got, err)
	}

	for name, fields := range map[string]map[string]interface{}{
		"no recipient":    {"to_address": nil},
		"no amount":       {"amount_usdc": nil},
		"amount as text":  {"amount_usdc": "10.07"},
		"negative amount": {"amount_usdc": -1.0},
		"negative fee":    {"fee_usdc": -0.25},
	} {
		_, err := paymentIntent(confirm(fields))
		var refused *txcheck.RefusedError
		if !errors.As(err, &refused) {
			t.Errorf("%s: err = %v, want a refusal", name, err)
		}
	}
}
