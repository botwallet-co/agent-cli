package cmd

import (
	"fmt"
	"regexp"
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

// ParseBreakdown parses a simple text breakdown into structured line items
//
// Supported formats:
// 1. With quantity: "2x API Calls @ $5.00" → 2 units at $5 each = $10 total
// 2. Without quantity: "Setup Fee - $10.00" → 1 unit at $10 = $10 total
// 3. With equals: "3x Processing @ $2.00 = $6.00" → explicit total
//
// Returns: (lineItems, totalAmount, error)
func ParseBreakdown(breakdown string) ([]LineItem, float64, error) {
	if breakdown == "" {
		return nil, 0, fmt.Errorf("breakdown cannot be empty")
	}

	lines := strings.Split(breakdown, "\n")
	items := []LineItem{}
	totalAmount := 0.0

	// Regex patterns
	// Pattern 1: "2x Item @ $5.00" or "2x Item @ $5.00 = $10.00"
	withQuantityPattern := regexp.MustCompile(`^(\d+)x\s+(.+?)\s+@\s+\$?([\d.]+)(?:\s*=\s*\$?([\d.]+))?$`)

	// Pattern 2: "Item @ $5.00" (no quantity, defaults to 1)
	noQuantityAtPattern := regexp.MustCompile(`^(.+?)\s+@\s+\$?([\d.]+)$`)

	// Pattern 3: "Item - $10.00" or "Item - $10.00 - Optional description"
	simplePattern := regexp.MustCompile(`^(.+?)\s+-\s+\$?([\d.]+)(?:\s+-\s+(.+))?$`)

	for lineNum, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue // Skip empty lines
		}

		var item LineItem

		// Try pattern 1: with quantity
		if matches := withQuantityPattern.FindStringSubmatch(line); matches != nil {
			quantity, qErr := strconv.Atoi(matches[1])
			if qErr != nil {
				return nil, 0, fmt.Errorf("line %d: invalid quantity", lineNum+1)
			}
			description := strings.TrimSpace(matches[2])
			unitPrice, pErr := strconv.ParseFloat(matches[3], 64)
			if pErr != nil {
				return nil, 0, fmt.Errorf("line %d: invalid unit price", lineNum+1)
			}

			if quantity <= 0 {
				return nil, 0, fmt.Errorf("line %d: quantity must be greater than 0 (got %d)\n\nExample: \"2x Item @ $5.00\" (quantity=2)", lineNum+1, quantity)
			}
			if unitPrice <= 0 {
				return nil, 0, fmt.Errorf("line %d: unit price must be greater than 0 (got $%.2f)\n\nExample: \"2x Item @ $5.00\" (price=$5.00)", lineNum+1, unitPrice)
			}

			calculatedTotal := float64(quantity) * unitPrice

			// If explicit total provided, validate it matches
			if matches[4] != "" {
				explicitTotal, tErr := strconv.ParseFloat(matches[4], 64)
				if tErr != nil {
					return nil, 0, fmt.Errorf("line %d: invalid total", lineNum+1)
				}
				if !floatsEqual(explicitTotal, calculatedTotal) {
					return nil, 0, fmt.Errorf("line %d: total $%.2f doesn't match %dx$%.2f = $%.2f",
						lineNum+1, explicitTotal, quantity, unitPrice, calculatedTotal)
				}
			}

			item = LineItem{
				Description:    description,
				Quantity:       quantity,
				UnitPriceCents: dollarsToCentsInt(unitPrice),
				TotalCents:     dollarsToCentsInt(calculatedTotal),
			}
			totalAmount += calculatedTotal
		} else if matches := noQuantityAtPattern.FindStringSubmatch(line); matches != nil {
			// Pattern 2: "Item @ $5.00" — treat as quantity 1
			description := strings.TrimSpace(matches[1])
			unitPrice, pErr := strconv.ParseFloat(matches[2], 64)
			if pErr != nil {
				return nil, 0, fmt.Errorf("line %d: invalid price", lineNum+1)
			}
			if unitPrice <= 0 {
				return nil, 0, fmt.Errorf("line %d: price must be greater than 0 (got $%.2f)", lineNum+1, unitPrice)
			}

			item = LineItem{
				Description:    description,
				Quantity:       1,
				UnitPriceCents: dollarsToCentsInt(unitPrice),
				TotalCents:     dollarsToCentsInt(unitPrice),
			}
			totalAmount += unitPrice
		} else if matches := simplePattern.FindStringSubmatch(line); matches != nil {
			// Pattern 3: simple format
			description := strings.TrimSpace(matches[1])
			price, pErr := strconv.ParseFloat(matches[2], 64)
			if pErr != nil {
				return nil, 0, fmt.Errorf("line %d: invalid price", lineNum+1)
			}

			if price <= 0 {
				return nil, 0, fmt.Errorf("line %d: price must be greater than 0 (got $%.2f)\n\nExample: \"Setup Fee - $10.00\"", lineNum+1, price)
			}

			// Optional additional description after second dash
			if matches[3] != "" {
				description = description + " - " + strings.TrimSpace(matches[3])
			}

			item = LineItem{
				Description:    description,
				Quantity:       1,
				UnitPriceCents: dollarsToCentsInt(price),
				TotalCents:     dollarsToCentsInt(price),
			}
			totalAmount += price
		} else {
			// Provide helpful error message with examples and common mistakes
			return nil, 0, fmt.Errorf(`line %d: invalid format "%s"

VALID FORMATS:
  • With quantity: "2x API Calls @ $5.00"
  • Simple: "Item Name - $10.00"

COMMON MISTAKES:
  ❌ "2 API Calls for $5"     → Use "2x" and "@", not "for"
  ❌ "Setup: $10"             → Use "-" not ":"
  ❌ "10x@$5"                 → Add spaces: "10x Item @ $5.00"
  ❌ "$5.00 - Setup"          → Price goes AFTER description

CORRECT EXAMPLES:
  ✓ "2x API Calls @ $5.00"
  ✓ "Setup Fee - $10.00"
  ✓ "Monthly Plan - $25.00"`, lineNum+1, line)
		}

		items = append(items, item)
	}

	if len(items) == 0 {
		return nil, 0, fmt.Errorf(`no valid line items found

Your breakdown appears to be empty or contains only invalid lines.

REQUIRED FORMAT (one item per line):
  2x API Calls @ $5.00
  Setup Fee - $10.00

Make sure each line follows one of these patterns.`)
	}

	return items, totalAmount, nil
}

// Helper: convert dollars to cents (int64)
// Uses rounding to avoid floating point precision issues
// e.g., 19.99 * 100 = 1998.9999... which would truncate to 1998 without rounding
func dollarsToCentsInt(dollars float64) int64 {
	return int64(dollars*100 + 0.5) // Round to nearest cent
}

// Helper: compare floats with tolerance
func floatsEqual(a, b float64) bool {
	tolerance := 0.01 // 1 cent tolerance
	return (a-b) < tolerance && (b-a) < tolerance
}

// ValidateBreakdownAmount checks if breakdown total matches the provided amount
func ValidateBreakdownAmount(breakdown string, expectedAmount float64) error {
	_, total, err := ParseBreakdown(breakdown)
	if err != nil {
		return err
	}

	if !floatsEqual(total, expectedAmount) {
		diff := expectedAmount - total
		return fmt.Errorf(`breakdown total doesn't match payment amount

  Breakdown total: $%.2f
  Payment amount:  $%.2f
  Difference:      $%.2f

FIX: Either adjust your line items to add up to $%.2f,
     or change the payment amount to $%.2f`, total, expectedAmount, diff, expectedAmount, total)
	}

	return nil
}

// FormatBreakdownExamples returns example breakdown formats for help text
func FormatBreakdownExamples() string {
	return `
═══════════════════════════════════════════════════════════════
BREAKDOWN FORMAT GUIDE
═══════════════════════════════════════════════════════════════

TWO VALID FORMATS:

  Format 1 - With quantity:
    <number>x <description> @ $<price>
    Example: "2x API Calls @ $5.00"

  Format 2 - Simple:
    <description> - $<price>
    Example: "Setup Fee - $10.00"

───────────────────────────────────────────────────────────────
FULL EXAMPLE (for $20 payment):

  botwallet paylink create 20.00 --desc "Services" --breakdown '2x API @ $5.00
  1x Setup @ $10.00'

───────────────────────────────────────────────────────────────
RULES:
  ✓ Use SINGLE QUOTES around breakdown (not double quotes)
  ✓ One item per line (press Enter between items)
  ✓ Items must add up to the payment amount
  ✓ Use "x" for quantity: 2x (not 2*)
  ✓ Use "@" for unit price: @ $5.00 (not "at" or "for")
  ✓ Use "-" for simple format: Item - $10.00

───────────────────────────────────────────────────────────────
COMMON MISTAKES:

  ❌ "2 API Calls for $5"    → Use: "2x API Calls @ $5.00"
  ❌ "Setup: $10"            → Use: "Setup - $10.00"
  ❌ "$10 - Setup"           → Price goes LAST: "Setup - $10.00"
  ❌ Using double quotes ""  → Use single quotes ''
═══════════════════════════════════════════════════════════════`
}
