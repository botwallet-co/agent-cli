package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/botwallet-co/agent-cli/api"
)

// parseItem parses a single --item flag value into a line item.
//
// Accepted formats:
//
//	"Description, price"           → quantity defaults to 1
//	"Description, price, quantity" → explicit quantity
//
// Price accepts: 5, 5.00 (also $5, $5.00 if properly quoted), with at most
// 2 decimal places: line items are stored in whole cents.
// Descriptions containing commas are handled correctly (parsing works from the right).
func parseItem(raw string) (api.LineItem, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return api.LineItem{}, fmt.Errorf("item cannot be empty")
	}

	parts := splitList(raw)
	if len(parts) < 2 {
		return api.LineItem{}, fmt.Errorf("cannot parse %q — expected \"description, price\"\n\nExamples:\n  --item \"API Calls, 5.00\"\n  --item \"Setup Fee, 10.00, 2\"", raw)
	}

	var description string
	var priceCents int64
	var quantity = 1

	last := parts[len(parts)-1]

	if len(parts) >= 3 {
		secondToLast := parts[len(parts)-2]

		if looksLikeQuantity(last) && looksLikePrice(secondToLast) {
			// Clear 3-field format: description, price, quantity
			quantity, _ = strconv.Atoi(last)
			p, err := parseCents(secondToLast)
			if err != nil {
				return api.LineItem{}, itemPriceError(secondToLast, raw, err)
			}
			priceCents = p
			description = strings.Join(parts[:len(parts)-2], ", ")
		} else if looksLikeQuantity(last) {
			// last is a bare integer but secondToLast is not a valid price.
			// Ambiguous: could be "desc, broken_price, qty" or "desc_with_comma, integer_price".
			// Require an explicit price format to resolve.
			return api.LineItem{}, fmt.Errorf(
				"ambiguous input %q — %q is not a valid price and %q could be a price or quantity\n\n"+
					"If using 3 fields (description, price, quantity), fix the price:\n"+
					"  --item \"description, 5.00, 2\"\n\n"+
					"If %q is the price, add a decimal to be explicit:\n"+
					"  --item \"..., %s.00\"",
				raw, secondToLast, last, last, last,
			)
		} else {
			// Last field is not a bare integer (has $ or decimal), so it's
			// unambiguously a price. Everything before it is the description.
			p, err := parseCents(last)
			if err != nil {
				return api.LineItem{}, itemPriceError(last, raw, err)
			}
			priceCents = p
			description = strings.Join(parts[:len(parts)-1], ", ")
		}
	} else {
		p, err := parseCents(last)
		if err != nil {
			return api.LineItem{}, itemPriceError(last, raw, err)
		}
		priceCents = p
		description = parts[0]
	}

	if description == "" {
		return api.LineItem{}, fmt.Errorf("item description is empty in %q", raw)
	}
	if priceCents <= 0 {
		return api.LineItem{}, fmt.Errorf("price must be greater than $0.00 (got $%.2f) in %q", centsToDollars(priceCents), raw)
	}
	if quantity <= 0 {
		return api.LineItem{}, fmt.Errorf("quantity must be at least 1 (got %d) in %q", quantity, raw)
	}
	if int64(quantity) > maxAmountCents/priceCents {
		return api.LineItem{}, fmt.Errorf("item total is too large in %q", raw)
	}

	return api.LineItem{
		Description:    description,
		Quantity:       quantity,
		UnitPriceCents: priceCents,
		TotalCents:     priceCents * int64(quantity),
	}, nil
}

// parseItems parses all --item flag values and returns line items with
// their total in cents. Everything is summed in cents, so no float error
// gets into the total.
func parseItems(items []string) ([]api.LineItem, int64, error) {
	if len(items) == 0 {
		return nil, 0, fmt.Errorf("no items provided")
	}

	var lineItems []api.LineItem
	var totalCents int64

	for i, raw := range items {
		item, err := parseItem(raw)
		if err != nil {
			return nil, 0, fmt.Errorf("--item #%d: %w", i+1, err)
		}
		if item.TotalCents > maxAmountCents-totalCents {
			return nil, 0, fmt.Errorf("--item #%d: the items' total is too large", i+1)
		}
		lineItems = append(lineItems, item)
		totalCents += item.TotalCents
	}

	return lineItems, totalCents, nil
}

// looksLikePrice returns true if s is written as a dollar amount, even one
// parseItem then refuses (negative, too large, fractions of a cent), so the
// error names the real problem.
func looksLikePrice(s string) bool {
	_, err := parseCents(s)
	return !errors.Is(err, errAmountFormat)
}

// looksLikeQuantity returns true if s is a bare positive integer (no $ or decimal point).
func looksLikeQuantity(s string) bool {
	s = strings.TrimSpace(s)
	if strings.Contains(s, ".") || strings.Contains(s, "$") {
		return false
	}
	n, err := strconv.Atoi(s)
	return err == nil && n > 0
}

// itemPriceError explains why priceField in item raw is not a valid price.
func itemPriceError(priceField, raw string, err error) error {
	switch {
	case errors.Is(err, errAmountDecimals):
		return fmt.Errorf("price %q in %q has more than 2 decimal places; prices are in dollars and cents, e.g. 0.05", priceField, raw)
	case errors.Is(err, errAmountNotPositive):
		return fmt.Errorf("price must be greater than $0.00 (got %s) in %q", priceField, raw)
	case errors.Is(err, errAmountTooLarge):
		return fmt.Errorf("price %q in %q is too large", priceField, raw)
	}
	return itemParseError(priceField, raw)
}

func itemParseError(priceField, raw string) error {
	return fmt.Errorf("cannot parse price from %q in %q\n\nFormat: --item \"description, price[, quantity]\"\n\nExamples:\n  --item \"API Calls, 5.00\"\n  --item \"Setup Fee, 10.00, 2\"", priceField, raw)
}
