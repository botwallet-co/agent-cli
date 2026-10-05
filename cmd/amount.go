package cmd

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/botwallet-co/agent-cli/output"
)

// Dollar amounts for pay, pay preview, withdraw, fund ask and paylink create
// (and --item prices) are kept in whole cents by the server. They are parsed
// straight to cents, so float rounding never changes an amount, and anything
// strconv.ParseFloat would otherwise let through is refused: NaN, Inf,
// exponents (1e3), hex (0x10), signs and fractions of a cent.
// x402 prices are not parsed here: they come in USDC base units and can
// legitimately be below one cent.

var (
	errAmountFormat      = errors.New("not a dollar amount")
	errAmountDecimals    = errors.New("more than 2 decimal places")
	errAmountTooLarge    = errors.New("amount is too large")
	errAmountNotPositive = errors.New("amount must be greater than 0")
)

// dollarAmountPattern is ASCII digits with an optional decimal point ("5",
// "5.", "5.5", ".50"). The number of decimals is checked separately, so that
// "0.001" gets its own error.
var dollarAmountPattern = regexp.MustCompile(`^(\d*)(?:\.(\d*))?$`)

// maxAmountDigits caps the whole-dollar part, and maxAmountCents any total,
// so cents stay exact in a float64 (and in an int64) on the way to the API.
const (
	maxAmountDigits = 13
	maxAmountCents  = 999_999_999_999_999
)

// parseCents parses a dollar amount such as "10", "10.5", "10.50" or
// "$10.50" into whole cents. Zero parses; callers decide whether it is
// allowed. A leading minus sign returns errAmountNotPositive.
func parseCents(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSpace(strings.TrimPrefix(s, "$"))
	if strings.HasPrefix(s, "-") {
		if _, err := parseCents(s[1:]); err == nil {
			return 0, errAmountNotPositive
		}
		return 0, errAmountFormat
	}

	m := dollarAmountPattern.FindStringSubmatch(s)
	if m == nil || (m[1] == "" && m[2] == "") {
		return 0, errAmountFormat
	}
	whole, frac := strings.TrimLeft(m[1], "0"), m[2]
	if len(frac) > 2 {
		if strings.TrimRight(frac[2:], "0") != "" {
			return 0, errAmountDecimals
		}
		frac = frac[:2] // "5.000" is 5.00
	}
	if len(whole) > maxAmountDigits {
		return 0, errAmountTooLarge
	}

	var dollars int64
	if whole != "" {
		n, err := strconv.ParseInt(whole, 10, 64)
		if err != nil {
			return 0, errAmountFormat
		}
		dollars = n
	}
	frac = (frac + "00")[:2]
	cents, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, errAmountFormat
	}
	return dollars*100 + cents, nil
}

// centsToDollars converts whole cents to the dollar amount the API takes.
func centsToDollars(cents int64) float64 {
	return float64(cents) / 100
}

// parseAmountArg parses the amount argument of pay, pay preview, withdraw,
// fund ask and paylink create. On a bad amount it prints the validation
// error (which exits) and returns ok=false.
func parseAmountArg(arg, example string) (cents int64, ok bool) {
	cents, err := parseCents(arg)
	switch {
	case errors.Is(err, errAmountNotPositive), err == nil && cents <= 0:
		output.ValidationError("Amount must be greater than 0", "Provide a positive number")
		return 0, false
	case errors.Is(err, errAmountDecimals):
		output.ValidationError("Invalid amount: "+arg,
			"Amounts are in dollars and cents, with at most 2 decimal places, e.g., "+example)
		return 0, false
	case errors.Is(err, errAmountTooLarge):
		output.ValidationError("Invalid amount: "+arg, "Amount is too large")
		return 0, false
	case err != nil:
		output.ValidationError("Invalid amount: "+arg, "Amount should be a number, e.g., "+example)
		return 0, false
	}
	return cents, true
}
