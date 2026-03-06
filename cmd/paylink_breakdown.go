package cmd

import (
	"fmt"
	"strconv"
	"strings"
)

// LineItem represents a single line in an invoice
type LineItem struct {
	Description    string `json:"description"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	TotalCents     int64  `json:"total_cents"`
}

// ParseItem parses a single --item flag value into a LineItem.
//
// Accepted formats:
//
//	"Description, price"           → quantity defaults to 1
//	"Description, price, quantity" → explicit quantity
//
// Price accepts: 5, 5.00 (also $5, $5.00 if properly quoted)
// Descriptions containing commas are handled correctly (parsing works from the right).
func ParseItem(raw string) (LineItem, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return LineItem{}, fmt.Errorf("item cannot be empty")
	}

	parts := splitAndTrim(raw)
	if len(parts) < 2 {
		return LineItem{}, fmt.Errorf("cannot parse %q — expected \"description, price\"\n\nExamples:\n  --item \"API Calls, 5.00\"\n  --item \"Setup Fee, 10.00, 2\"", raw)
	}

	var description string
	var price float64
	var quantity = 1

	last := parts[len(parts)-1]

	if len(parts) >= 3 {
		secondToLast := parts[len(parts)-2]

		if looksLikeQuantity(last) && looksLikePrice(secondToLast) {
			// Clear 3-field format: description, price, quantity
			quantity, _ = strconv.Atoi(last)
			price, _ = parsePrice(secondToLast)
			description = strings.Join(parts[:len(parts)-2], ", ")
		} else if looksLikeQuantity(last) {
			// last is a bare integer but secondToLast is not a valid price.
			// Ambiguous: could be "desc, broken_price, qty" or "desc_with_comma, integer_price".
			// Require an explicit price format to resolve.
			return LineItem{}, fmt.Errorf(
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
			p, err := parsePrice(last)
			if err != nil {
				return LineItem{}, itemParseError(last, raw)
			}
			price = p
			description = strings.Join(parts[:len(parts)-1], ", ")
		}
	} else {
		p, err := parsePrice(last)
		if err != nil {
			return LineItem{}, itemParseError(last, raw)
		}
		price = p
		description = parts[0]
	}

	if description == "" {
		return LineItem{}, fmt.Errorf("item description is empty in %q", raw)
	}
	if price <= 0 {
		return LineItem{}, fmt.Errorf("price must be greater than $0.00 (got $%.2f) in %q", price, raw)
	}
	if quantity <= 0 {
		return LineItem{}, fmt.Errorf("quantity must be at least 1 (got %d) in %q", quantity, raw)
	}

	totalPrice := float64(quantity) * price

	return LineItem{
		Description:    description,
		Quantity:       quantity,
		UnitPriceCents: dollarsToCentsInt(price),
		TotalCents:     dollarsToCentsInt(totalPrice),
	}, nil
}

// ParseItems parses all --item flag values and returns line items with their total.
// Total is computed from cents to avoid floating-point accumulation errors.
func ParseItems(items []string) ([]LineItem, float64, error) {
	if len(items) == 0 {
		return nil, 0, fmt.Errorf("no items provided")
	}

	var lineItems []LineItem
	var totalCents int64

	for i, raw := range items {
		item, err := ParseItem(raw)
		if err != nil {
			return nil, 0, fmt.Errorf("--item #%d: %w", i+1, err)
		}
		lineItems = append(lineItems, item)
		totalCents += item.TotalCents
	}

	total := float64(totalCents) / 100.0
	return lineItems, total, nil
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func parsePrice(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty price")
	}
	return strconv.ParseFloat(s, 64)
}

// looksLikePrice returns true if s can be parsed as a dollar amount.
func looksLikePrice(s string) bool {
	_, err := parsePrice(s)
	return err == nil
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

func dollarsToCentsInt(dollars float64) int64 {
	return int64(dollars*100 + 0.5)
}

func floatsEqual(a, b float64) bool {
	tolerance := 0.01
	return (a-b) < tolerance && (b-a) < tolerance
}

func itemParseError(priceField, raw string) error {
	return fmt.Errorf("cannot parse price from %q in %q\n\nFormat: --item \"description, price[, quantity]\"\n\nExamples:\n  --item \"API Calls, 5.00\"\n  --item \"Setup Fee, 10.00, 2\"", priceField, raw)
}
