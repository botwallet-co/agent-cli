package output

import "fmt"

// =============================================================================
// API Error Formatting
// =============================================================================

// PrintError prints an error. In JSON mode it is one object with error,
// message, how_to_fix and details. In human mode the message goes to stderr
// and the fix and any balance details follow on stdout. It does not exit:
// the command does that, with exit code 1.
func PrintError(code string, message string, howToFix string, details map[string]interface{}) {
	// JSON output (default for bots)
	if !humanOutput {
		result := map[string]interface{}{
			"error":      code,
			"message":    message,
			"how_to_fix": howToFix,
		}
		if details != nil {
			result["details"] = details
		}
		JSON(result)
		return
	}

	// Human-readable output
	ErrorMsg("%s", message)

	if howToFix != "" {
		fmt.Println()
		styleInfo.Print("   → ")
		fmt.Println(howToFix)
	}

	// Show relevant details
	if details != nil {
		// Payments send balance/required, withdrawals balance_usdc/required_usdc.
		if balance, ok := moneyDetail(details, "balance", "balance_usdc"); ok {
			fmt.Println()
			KeyValueMoney("Current balance", balance)
		}
		if required, ok := moneyDetail(details, "required", "required_usdc"); ok {
			KeyValueMoney("Required", required)
		}
		if shortfall, ok := details["shortfall"].(float64); ok {
			KeyValueMoney("Shortfall", shortfall)
		}
		if fundingURL, ok := details["funding_url"].(string); ok {
			KeyValueURL("Add funds at", fundingURL)
		}
	}
}

// moneyDetail returns the first of keys that holds a number in details.
func moneyDetail(details map[string]interface{}, keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := details[k].(float64); ok {
			return v, true
		}
	}
	return 0, false
}
