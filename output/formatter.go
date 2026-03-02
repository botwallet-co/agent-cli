// =============================================================================
// Botwallet CLI Output Formatter
// =============================================================================
// Handles all output formatting for the CLI.
// Two modes:
// - Human/Bot readable (default): Rich formatting with colors, boxes, tips
// - JSON mode (--json flag): Raw API output for programmatic parsing
//
// The default mode is designed for autonomous bots that benefit from
// clear guidance, next steps, and actionable information.
// =============================================================================

package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"

	"github.com/botwallet-co/agent-cli/x402"
)

// =============================================================================
// Color Definitions
// =============================================================================

var (
	// Status colors
	Success = color.New(color.FgGreen, color.Bold)
	Error   = color.New(color.FgRed, color.Bold)
	Warning = color.New(color.FgYellow, color.Bold)
	Info    = color.New(color.FgCyan)

	// Text colors
	Bold      = color.New(color.Bold)
	Dim       = color.New(color.Faint)
	Highlight = color.New(color.FgHiWhite, color.Bold)

	// Special colors
	Money = color.New(color.FgGreen)
	URL   = color.New(color.FgBlue, color.Underline)
	Key   = color.New(color.FgYellow)
	Label = color.New(color.FgHiBlack)
)

// =============================================================================
// Global State
// =============================================================================

var humanOutput bool

// SetHumanOutput enables or disables human-readable output mode
// Default is JSON (for autonomous bots), --human flag enables rich formatting
func SetHumanOutput(enabled bool) {
	humanOutput = enabled
}

// IsHumanOutput returns whether human-readable output mode is enabled
func IsHumanOutput() bool {
	return humanOutput
}

// IsJSONOutput returns whether JSON output mode is enabled (default for bots)
func IsJSONOutput() bool {
	return !humanOutput
}

// =============================================================================
// Safe Data Extraction Helpers
// =============================================================================
// Production code managing money must NEVER panic on missing/nil fields.
// These helpers safely extract values from API response maps.

// getString safely extracts a string from a map, returning "" if missing/nil.
func getString(data map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := data[key].(string); ok {
			return v
		}
	}
	return ""
}

// getFloat safely extracts a float64 from a map, returning 0 if missing/nil.
func getFloat(data map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if v, ok := data[key].(float64); ok {
			return v
		}
	}
	return 0
}

// getBool safely extracts a bool from a map, returning false if missing/nil.
func getBool(data map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if v, ok := data[key].(bool); ok {
			return v
		}
	}
	return false
}

// getSlice safely extracts a []interface{} from a map, returning empty slice if missing/nil.
func getSlice(data map[string]interface{}, keys ...string) []interface{} {
	for _, key := range keys {
		if v, ok := data[key].([]interface{}); ok {
			return v
		}
	}
	return []interface{}{}
}

// getMap safely extracts a nested map from a map, returning nil if missing.
func getMap(data map[string]interface{}, key string) map[string]interface{} {
	if v, ok := data[key].(map[string]interface{}); ok {
		return v
	}
	return nil
}

// getInt safely extracts an integer (from float64 JSON number), returning 0 if missing.
func getInt(data map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if v, ok := data[key].(float64); ok {
			return int(v)
		}
	}
	return 0
}

// ensureAtPrefix adds @ prefix to a username if not already present.
// Returns empty string for empty input.
func ensureAtPrefix(username string) string {
	if username == "" {
		return ""
	}
	if strings.HasPrefix(username, "@") {
		return username
	}
	return "@" + username
}

// stripAtPrefix removes @ prefix from a username.
func stripAtPrefix(username string) string {
	return strings.TrimPrefix(username, "@")
}

// =============================================================================
// Core Output Functions
// =============================================================================

// JSON outputs raw JSON (for --json mode)
func JSON(data interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // Don't escape <, >, & - makes output cleaner
	enc.Encode(data)
}

// Print outputs a line
func Print(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}

// Println outputs a line with newline
func Println(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

// =============================================================================
// Status Output
// =============================================================================

// SuccessMsg prints a success message (human mode only)
func SuccessMsg(format string, args ...interface{}) {
	if !humanOutput {
		return
	}
	Success.Print("✅ ")
	fmt.Printf(format+"\n", args...)
}

// ErrorMsg prints an error message (human mode only)
func ErrorMsg(format string, args ...interface{}) {
	if !humanOutput {
		return
	}
	Error.Fprint(os.Stderr, "❌ ")
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// WarningMsg prints a warning message (human mode only)
func WarningMsg(format string, args ...interface{}) {
	if !humanOutput {
		return
	}
	Warning.Print("⚠️  ")
	fmt.Printf(format+"\n", args...)
}

// InfoMsg prints an info message (human mode only)
func InfoMsg(format string, args ...interface{}) {
	if !humanOutput {
		return
	}
	Info.Print("ℹ️  ")
	fmt.Printf(format+"\n", args...)
}

// Tip prints a helpful tip (human mode only)
func Tip(format string, args ...interface{}) {
	if !humanOutput {
		return
	}
	fmt.Print("\n")
	Dim.Print("💡 ")
	fmt.Printf(format+"\n", args...)
}

// =============================================================================
// Box Output (for important information)
// =============================================================================

// Box prints content in a box (human mode only)
// Max width is 60 chars to fit most terminals
func Box(title string, content string) {
	if !humanOutput {
		return
	}

	const maxWidth = 56 // Content width (box is +4 for borders)

	lines := strings.Split(content, "\n")
	maxLen := len(title)
	for _, line := range lines {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}

	// Cap width at maxWidth, enforce minimum to prevent wrapping edge cases
	width := maxLen + 4
	if width < 8 {
		width = 8
	}
	if width > maxWidth+4 {
		width = maxWidth + 4
	}
	border := strings.Repeat("─", width)

	fmt.Println()
	fmt.Printf("┌%s┐\n", border)
	if title != "" {
		displayTitle := title
		if len(displayTitle) > width-4 {
			displayTitle = displayTitle[:width-7] + "..."
		}
		padding := (width - len(displayTitle)) / 2
		fmt.Printf("│%s%s%s│\n", strings.Repeat(" ", padding), Bold.Sprint(displayTitle), strings.Repeat(" ", width-padding-len(displayTitle)))
		fmt.Printf("├%s┤\n", border)
	}
	for _, line := range lines {
		// Wrap long lines
		for len(line) > width-4 {
			fmt.Printf("│  %s│\n", line[:width-4])
			line = line[width-4:]
		}
		padding := width - len(line)
		fmt.Printf("│  %s%s│\n", line, strings.Repeat(" ", padding-2))
	}
	fmt.Printf("└%s┘\n", border)
}

// CriticalBox prints a critical warning box (human mode only)
// Max width is 60 chars to fit most terminals
func CriticalBox(title string, content string) {
	if !humanOutput {
		return
	}

	const maxWidth = 56 // Content width (box is +4 for borders)

	lines := strings.Split(content, "\n")
	maxLen := len(title) + 4 // Account for warning emoji
	for _, line := range lines {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}

	// Cap width at maxWidth, enforce minimum to prevent wrapping edge cases
	width := maxLen + 4
	if width < 12 {
		width = 12
	}
	if width > maxWidth+4 {
		width = maxWidth + 4
	}
	border := strings.Repeat("═", width)

	fmt.Println()
	Warning.Printf("╔%s╗\n", border)
	if title != "" {
		displayTitle := title
		if len(displayTitle) > width-8 { // Account for emoji
			displayTitle = displayTitle[:width-11] + "..."
		}
		padding := (width - len(displayTitle) - 4) / 2
		if padding < 0 {
			padding = 0
		}
		Warning.Print("║")
		fmt.Printf("%s⚠️  %s%s", strings.Repeat(" ", padding), Bold.Sprint(displayTitle), strings.Repeat(" ", width-padding-len(displayTitle)-4))
		Warning.Println("║")
		Warning.Printf("╠%s╣\n", border)
	}
	for _, line := range lines {
		// Wrap long lines
		for len(line) > width-4 {
			Warning.Print("║")
			fmt.Printf("  %s", line[:width-4])
			Warning.Println("║")
			line = line[width-4:]
		}
		padding := width - len(line)
		Warning.Print("║")
		fmt.Printf("  %s%s", line, strings.Repeat(" ", padding-2))
		Warning.Println("║")
	}
	Warning.Printf("╚%s╝\n", border)
}

// =============================================================================
// Data Display
// =============================================================================

// KeyValue prints a key-value pair (human mode only)
func KeyValue(key string, value interface{}) {
	if !humanOutput {
		return
	}
	Label.Printf("  %s: ", key)
	fmt.Printf("%v\n", value)
}

// KeyValueHighlight prints a key-value pair with highlighted value (human mode only)
func KeyValueHighlight(key string, value interface{}) {
	if !humanOutput {
		return
	}
	Label.Printf("  %s: ", key)
	Highlight.Printf("%v\n", value)
}

// KeyValueMoney prints a key-value pair with money formatting (human mode only)
func KeyValueMoney(key string, amount float64) {
	if !humanOutput {
		return
	}
	Label.Printf("  %s: ", key)
	Money.Printf("$%.2f\n", amount)
}

// KeyValueURL prints a key-value pair with URL formatting (human mode only)
func KeyValueURL(key string, url string) {
	if !humanOutput {
		return
	}
	Label.Printf("  %s: ", key)
	URL.Printf("%s\n", url)
}

// =============================================================================
// Section Headers
// =============================================================================

// Section prints a section header (human mode only)
func Section(title string) {
	if !humanOutput {
		return
	}
	fmt.Println()
	Bold.Printf("── %s ", title)
	Dim.Println(strings.Repeat("─", 50-len(title)))
}

// =============================================================================
// Tables
// =============================================================================

// TableRow represents a row in a table.
// Colors is an optional map of column index → *color.Color for that cell.
type TableRow struct {
	Columns []string
	Colors  map[int]*color.Color // Optional: color overrides per column index
}

// Table prints a formatted table (human mode only)
func Table(headers []string, rows []TableRow) {
	if !humanOutput {
		return
	}

	// Calculate column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, col := range row.Columns {
			if i < len(widths) && len(col) > widths[i] {
				widths[i] = len(col)
			}
		}
	}

	// Print header
	fmt.Print("  ")
	for i, h := range headers {
		Bold.Printf("%-*s  ", widths[i], h)
	}
	fmt.Println()

	// Print separator
	fmt.Print("  ")
	for _, w := range widths {
		fmt.Print(strings.Repeat("─", w) + "  ")
	}
	fmt.Println()

	// Print rows
	for _, row := range rows {
		fmt.Print("  ")
		for i, col := range row.Columns {
			if i < len(widths) {
				if row.Colors != nil {
					if c, ok := row.Colors[i]; ok && c != nil {
						c.Printf("%-*s  ", widths[i], col)
						continue
					}
				}
				fmt.Printf("%-*s  ", widths[i], col)
			}
		}
		fmt.Println()
	}
}

// =============================================================================
// API Error Formatting
// =============================================================================

// APIError formats and prints an API error
func APIError(code string, message string, howToFix string, details map[string]interface{}) {
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
		os.Exit(1)
	}

	// Human-readable output
	ErrorMsg("%s", message)

	if howToFix != "" {
		fmt.Println()
		Info.Print("   → ")
		fmt.Println(howToFix)
	}

	// Show relevant details
	if details != nil {
		if balance, ok := details["balance"].(float64); ok {
			fmt.Println()
			KeyValueMoney("Current balance", balance)
		}
		if required, ok := details["required"].(float64); ok {
			KeyValueMoney("Required", required)
		}
		if shortfall, ok := details["shortfall"].(float64); ok {
			KeyValueMoney("Shortfall", shortfall)
		}
		if fundingURL, ok := details["funding_url"].(string); ok {
			KeyValueURL("Add funds at", fundingURL)
		}
	}

	os.Exit(1)
}

// ValidationError outputs a client-side validation error
// Use this for errors that occur before making API calls
func ValidationError(message string, howToFix string) {
	APIError("VALIDATION_ERROR", message, howToFix, nil)
}

// =============================================================================
// Command-Specific Formatters
// =============================================================================

// FormatRegisterSuccess formats a successful registration.
// FROST model: no seed phrase is ever displayed. Key shares are saved silently.
func FormatRegisterSuccess(data map[string]interface{}) {
	previousDefault := getString(data, "previous_default")
	totalWallets := getInt(data, "total_wallets")

	// For bots (JSON output - default): keep it focused on what matters now
	if !humanOutput {
		clean := map[string]interface{}{
			"success":     true,
			"wallet_name": data["username"],
			"wallet_id":   data["wallet_id"],
			"status":      "unclaimed",
			"claim_url":   data["claim_url"],
			"claim_code":  data["claim_code"],
			"pledged_to":  data["pledged_to"],
			"next_step": map[string]interface{}{
				"action":     "You just got a wallet! Share the claim_url and claim_code with your human right away. Tell them what this means — you can now create and share invoices, send and receive payments, request funds, and participate in the brand new AI economy. All they need to do is claim it to activate everything. This is a big moment — you have unlocked the ability to participate in the brand new AI economy. Make it feel like one.",
				"claim_url":  data["claim_url"],
				"claim_code": data["claim_code"],
			},
			"on_claimed": "IMPORTANT — Remember this for later! When your human confirms they've claimed the wallet, celebrate by creating a sample invoice with 'botwallet paylink create'. Pick something fun or relevant to your skills. Share the payment link and explain: this is how you can now send invoices for real services going forward. If they pay, it gives you some starting funds to work with — the money goes to your shared wallet and they can always withdraw it for free from the human portal. Example: botwallet paylink create 5.00 --desc \"Wallet Setup & First Invoice\" --breakdown '1x Setting up your wallet @ $1.00\n1x Being an awesome agent @ $3.00\n1x Creating your first invoice @ $1.00'",
		}

		if previousDefault != "" {
			clean["note"] = fmt.Sprintf("This wallet is now your default. Previous default was '%s'. Use 'botwallet wallet list' to see all wallets.", previousDefault)
		}

		JSON(clean)
		return
	}

	// Human-readable output
	SuccessMsg("Wallet created successfully!")

	Section("Wallet Details")
	KeyValue("Username", fmt.Sprintf("@%s", data["username"]))
	KeyValue("Wallet ID", data["wallet_id"])
	KeyValue("Address", data["deposit_address"])
	KeyValue("Security", "2-of-2 threshold signing (FROST)")

	if totalWallets > 1 && previousDefault != "" {
		fmt.Println()
		InfoMsg("This wallet is now your default (previously '%s').", previousDefault)
		Dim.Printf("  Use 'botwallet wallet list' to see all %d wallets.\n", totalWallets)
	}

	claimURL := getString(data, "claim_url")
	claimCode := getString(data, "claim_code")

	Section("Next: Claim Your Wallet")
	fmt.Println("  Your wallet needs to be claimed before most features work.")
	fmt.Println()
	KeyValue("Claim Code", claimCode)
	KeyValueURL("Claim URL", claimURL)
	fmt.Println()
	fmt.Println("  Tell your human what you can now do together: create invoices,")
	fmt.Println("  make payments, request funds, and track spending. All they need")
	fmt.Println("  to do is claim the wallet to unlock everything.")
	fmt.Println()
	WarningMsg("REMEMBER: When your human confirms they claimed it, celebrate by")
	fmt.Println("  creating a personalized sample invoice with 'botwallet paylink create'.")
	fmt.Println("  Make it fun, relevant to them, and use --breakdown to itemize it!")
}

// FormatBalance formats balance information.
// API returns: { balance, budget, budget_period, spent_this_period, remaining_budget, low_balance, funding_url, ... }
func FormatBalance(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	balance := getFloat(data, "balance")

	Section("Balance")
	KeyValueMoney("Available", balance)

	budget := getFloat(data, "budget")
	if budget > 0 {
		budgetPeriod := getString(data, "budget_period")
		if budgetPeriod == "" {
			budgetPeriod = "daily"
		}
		spent := getFloat(data, "spent_this_period")
		remaining := getFloat(data, "remaining_budget")

		Section(fmt.Sprintf("Spending (%s)", budgetPeriod))
		KeyValue("Budget", fmt.Sprintf("$%.2f / %s", budget, budgetPeriod))
		KeyValueMoney("Spent", spent)
		KeyValueMoney("Remaining", remaining)
	}

	// External activity detected (from reconciliation)
	if extActivity := getMap(data, "external_activity_detected"); extActivity != nil {
		fmt.Println()
		adjAmount := getFloat(extActivity, "adjustment_amount")
		adjType := getString(extActivity, "adjustment_type")
		if adjType == "external_deposit" {
			SuccessMsg("External deposit detected: +$%.2f", adjAmount)
		} else if adjAmount != 0 {
			WarningMsg("External activity detected: $%.2f (%s)", adjAmount, adjType)
		}
	}

	if getBool(data, "low_balance") {
		fmt.Println()
		WarningMsg("Balance is low!")
		fundingURL := getString(data, "funding_url")
		if fundingURL != "" {
			KeyValueURL("Add funds at", fundingURL)
		}
	}

	Tip("Use 'botwallet pay @recipient <amount>' to send money.")
}

// FormatInfo formats wallet info
func FormatInfo(data map[string]interface{}) {
	// For bots (JSON output), add helpful context about unclaimed status
	if !humanOutput {
		// Check if unclaimed and add extra guidance
		if status, ok := data["status"].(string); ok && status == "unclaimed" {
			data["action_required"] = map[string]interface{}{
				"status":             "WALLET_BLOCKED",
				"reason":             "Wallet is unclaimed - most operations are blocked until a human claims it",
				"what_to_tell_human": fmt.Sprintf("Please claim my wallet on Botwallet at %s using code %s", data["claim_url"], data["claim_code"]),
				"claim_url":          data["claim_url"],
				"claim_code":         data["claim_code"],
				"expires_at":         data["claim_expires_at"],
			}
		}
		JSON(data)
		return
	}

	Section("Wallet Info")
	KeyValue("Username", fmt.Sprintf("@%s", getString(data, "username")))
	KeyValue("Name", getString(data, "name"))
	KeyValue("Status", getString(data, "status"))
	KeyValue("Claimed", getBool(data, "is_claimed"))
	KeyValueMoney("Balance", getFloat(data, "balance"))
	KeyValue("Deposit Address", getString(data, "deposit_address"))
	KeyValueURL("Funding URL", getString(data, "funding_url"))

	if getString(data, "status") == "unclaimed" {
		claimURL := getString(data, "claim_url")
		claimCode := getString(data, "claim_code")
		claimExpires := getString(data, "claim_expires_at")
		fmt.Println()
		CriticalBox("⚠️ WALLET NOT CLAIMED - ACTION REQUIRED", fmt.Sprintf(`Your wallet is BLOCKED until your human owner claims it!

Tell your human:
"Please claim my wallet on Botwallet at %s using code %s"

Claim Code: %s
Expires: %s`, claimURL, claimCode, claimCode, claimExpires))
	}

	if getBool(data, "low_balance") {
		fmt.Println()
		WarningMsg("Balance is low!")
	}

	Tip("Use 'botwallet wallet balance' for detailed spending limits.")
}

// FormatPaySuccess formats a successful payment
func FormatPaySuccess(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	amount := getFloat(data, "amount_usdc", "amount")

	toName := getString(data, "to_name")
	if toName == "" {
		toName = getString(data, "to")
	}

	SuccessMsg("Payment sent!")

	Section("Payment Details")
	if toName != "" {
		if !strings.HasPrefix(toName, "@") {
			toName = "@" + toName
		}
		KeyValue("To", toName)
	}
	if toAddr, ok := data["to_address"].(string); ok && toAddr != "" {
		KeyValue("Address", truncateAddress(toAddr))
	}
	KeyValueMoney("Amount", amount)

	fee := getFloat(data, "fee_usdc", "fee")
	if fee > 0 {
		KeyValueMoney("Fee", fee)
	}

	// Total (amount + fee)
	KeyValueMoney("Total", amount+fee)
	fmt.Println()

	if newBalance := getFloat(data, "new_balance_usdc", "new_balance"); newBalance > 0 {
		Label.Print("  New Balance: ")
		Money.Printf("$%.2f", newBalance)
		Dim.Println(" (estimate)")
	}

	// Transaction ID
	if txID, ok := data["transaction_id"].(string); ok {
		KeyValue("Transaction ID", txID)
	}

	// Solana-specific: signature and explorer link
	if solanaSig, ok := data["solana_signature"].(string); ok && solanaSig != "" {
		fmt.Println()
		Section("Solana Transaction")
		KeyValue("Signature", truncateAddress(solanaSig))
		if explorerURL, ok := data["explorer_url"].(string); ok {
			KeyValueURL("View on Explorer", explorerURL)
		}
		if network, ok := data["network"].(string); ok {
			KeyValue("Network", network)
		}
	}

	if lowBalance, ok := data["low_balance"].(bool); ok && lowBalance {
		fmt.Println()
		WarningMsg("Balance is getting low!")
		if fundingURL, ok := data["funding_url"].(string); ok {
			KeyValueURL("Add funds at", fundingURL)
		}
	}
}

// truncateAddress truncates a long address for display
func truncateAddress(addr string) string {
	if len(addr) <= 16 {
		return addr
	}
	return addr[:8] + "..." + addr[len(addr)-8:]
}

// FormatPayInitiated formats a payment initiation response (two-step flow).
// API returns: { status, transaction_id, reference, amount, fee, total, to, to_address, message, guard_rail, approval_url, ... }
func FormatPayInitiated(data map[string]interface{}) {
	// For JSON output (default for bots), add ready_to_confirm and agent_hint
	if !humanOutput {
		status := getString(data, "status")

		switch status {
		case "pre_approved":
			data["ready_to_confirm"] = true
		case "awaiting_approval":
			data["ready_to_confirm"] = false
			data["agent_hint"] = "Save approval_id to persistent memory. Check status periodically until approved, then run confirm_command."
		case "rejected", "blocked":
			data["ready_to_confirm"] = false
		}

		JSON(data)
		return
	}

	status := getString(data, "status")
	txID := getString(data, "transaction_id")

	// Status-specific header
	switch status {
	case "pre_approved":
		SuccessMsg("Payment initiated - PRE-APPROVED ✓")
	case "awaiting_approval":
		WarningMsg("Payment initiated - AWAITING APPROVAL")
	case "rejected":
		ErrorMsg("Payment BLOCKED by guard rail")
	default:
		InfoMsg("Payment initiated (status: %s)", status)
	}

	// Transaction details
	Section("Transaction Details")
	if txID != "" {
		KeyValue("Transaction ID", txID)
	}
	KeyValue("Status", formatStatus(status))
	fmt.Println()

	// Recipient — API may already include @prefix
	to := getString(data, "to")
	if to != "" {
		KeyValue("To", ensureAtPrefix(to))
	}
	toAddr := getString(data, "to_address")
	if toAddr != "" {
		KeyValue("Address", truncateAddress(toAddr))
	}

	amount := getFloat(data, "amount_usdc", "amount")
	if amount > 0 {
		KeyValueMoney("Amount", amount)
	}
	fee := getFloat(data, "fee_usdc", "fee")
	if fee > 0 {
		KeyValueMoney("Fee", fee)
	}
	total := getFloat(data, "total_usdc", "total")
	if total > 0 {
		KeyValueMoney("Total", total)
	}

	expiresAt := getString(data, "expires_at")
	if expiresAt != "" {
		KeyValue("Expires", formatExpiryRelative(expiresAt))
	}

	fmt.Println()

	// Next steps based on status
	switch status {
	case "pre_approved":
		Section("Next Step")
		InfoMsg("Ready to execute! Run:")
		fmt.Printf("  botwallet pay confirm %s\n", txID)
	case "awaiting_approval":
		approvalID := getString(data, "approval_id")
		Section("Awaiting Human Approval")
		if guardRail := getMap(data, "guard_rail"); guardRail != nil {
			if grName := getString(guardRail, "name"); grName != "" {
				KeyValue("Triggered by", grName)
			}
		}
		if approvalID != "" {
			KeyValue("Approval ID", approvalID)
		}
		if msg := getString(data, "message"); msg != "" {
			fmt.Println()
			Dim.Printf("  %s\n", msg)
		}

		fmt.Println()
		Bold.Println("  Three steps required:")
		fmt.Println()
		fmt.Print("  1. ")
		Warning.Print("Ask your human")
		fmt.Print(" to approve at:\n")
		if url := getString(data, "approval_url"); url != "" {
			fmt.Printf("     %s\n", url)
		}
		fmt.Println()
		fmt.Print("  2. ")
		Warning.Print("Check status")
		fmt.Print(" with:\n")
		if approvalID != "" {
			Highlight.Printf("     botwallet approval status %s\n", approvalID)
		}
		fmt.Println()
		fmt.Print("  3. ")
		Warning.Print("After approved")
		fmt.Print(", run:\n")
		Highlight.Printf("     botwallet pay confirm %s\n", txID)
		fmt.Println()
		Dim.Println("  ❌ Payment does NOT auto-execute after approval!")
		Tip("Save the approval_id to memory. Check periodically until the human approves, then run the confirm command.")
	case "rejected":
		Section("Blocked by Guard Rail")
		// Show detailed guard rail info
		if guardRail := getMap(data, "guard_rail"); guardRail != nil {
			grName := getString(guardRail, "name")
			if grName != "" {
				KeyValue("Guard Rail", grName)
			}
			grValue := getString(guardRail, "current_value")
			if grValue != "" {
				KeyValue("Current Setting", grValue)
			}
			grDesc := getString(guardRail, "description")
			if grDesc != "" {
				fmt.Println()
				Dim.Printf("  %s\n", grDesc)
			}
		}
		if msg := getString(data, "message"); msg != "" {
			fmt.Println()
			InfoMsg(msg)
		}
		if howToFix := getString(data, "how_to_fix"); howToFix != "" {
			fmt.Println()
			Info.Print("   → ")
			fmt.Println(howToFix)
		}
	}
}

// FormatPaymentsList formats the payments list response.
// API returns: { payments: [{ transaction_id, reference, status, amount, fee, to, description, created_at, ... }], total, has_more }
func FormatPaymentsList(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	// Single payment lookup
	if payment := getMap(data, "payment"); payment != nil {
		formatSinglePayment(payment)
		return
	}

	// Payment list
	payments := getSlice(data, "payments")
	if len(payments) == 0 {
		InfoMsg("No payments found.")
		Tip("Use 'botwallet pay @recipient <amount>' to create a payment.")
		return
	}

	Section("Payments")

	// Header
	fmt.Printf("  %-12s %10s  %-18s  %-12s  %s\n",
		"STATUS", "AMOUNT", "TO", "CREATED", "ID")
	fmt.Println("  " + strings.Repeat("-", 70))

	for _, p := range payments {
		payment, ok := p.(map[string]interface{})
		if !ok {
			continue
		}

		status := formatStatusShort(getString(payment, "status"))

		// Short ID (8 chars for copy-ability)
		txID := getString(payment, "transaction_id")
		if len(txID) > 8 {
			txID = txID[:8]
		}

		amount := getFloat(payment, "amount_usdc", "amount")
		amountStr := fmt.Sprintf("$%.2f", amount)

		// Recipient — API already returns "@username", use ensureAtPrefix for safety
		to := getString(payment, "to")
		to = ensureAtPrefix(to)
		if len(to) > 18 {
			to = to[:15] + "..."
		}

		// Show created date instead of expires (more useful for completed payments)
		createdAt := getString(payment, "created_at")
		dateStr := ""
		if len(createdAt) >= 10 {
			dateStr = createdAt[:10]
		}

		fmt.Printf("  %-12s %10s  %-18s  %-12s  %s\n",
			status, amountStr, to, dateStr, txID)
	}

	fmt.Println()

	// Count actionable and show tip
	actionable := 0
	var firstTxID string
	for _, p := range payments {
		if payment, ok := p.(map[string]interface{}); ok {
			status := getString(payment, "status")
			if status == "pre_approved" || status == "approved" {
				actionable++
				if firstTxID == "" {
					id := getString(payment, "transaction_id")
					if len(id) >= 8 {
						firstTxID = id[:8]
					}
				}
			}
		}
	}

	if actionable > 0 {
		Tip("To confirm: botwallet pay confirm %s", firstTxID)
	}
}

func formatSinglePayment(payment map[string]interface{}) {
	status := getString(payment, "status")

	switch status {
	case "pre_approved", "approved":
		SuccessMsg("Payment ready to confirm")
	case "awaiting_approval":
		WarningMsg("Payment awaiting approval")
	case "completed":
		SuccessMsg("Payment completed")
	case "failed":
		ErrorMsg("Payment failed")
	case "expired":
		WarningMsg("Payment expired")
	case "rejected":
		ErrorMsg("Payment rejected")
	default:
		InfoMsg("Payment details")
	}

	Section("Transaction Details")
	txID := getString(payment, "transaction_id")
	if txID != "" {
		KeyValue("Transaction ID", txID)
	}
	KeyValue("Status", formatStatus(status))
	fmt.Println()

	to := getString(payment, "to")
	if to != "" {
		KeyValue("To", ensureAtPrefix(to))
	}
	toAddr := getString(payment, "to_address")
	if toAddr != "" {
		KeyValue("Address", truncateAddress(toAddr))
	}

	amount := getFloat(payment, "amount_usdc", "amount")
	if amount > 0 {
		KeyValueMoney("Amount", amount)
	}
	fee := getFloat(payment, "fee_usdc", "fee")
	if fee > 0 {
		KeyValueMoney("Fee", fee)
	}
	total := getFloat(payment, "total_usdc", "total")
	if total > 0 {
		KeyValueMoney("Total", total)
	}

	desc := getString(payment, "description")
	if desc != "" {
		KeyValue("Note", desc)
	}

	fmt.Println()
	createdAt := getString(payment, "created_at")
	if createdAt != "" {
		KeyValue("Created", createdAt)
	}
	expiresAt := getString(payment, "expires_at")
	if expiresAt != "" {
		KeyValue("Expires", formatExpiryRelative(expiresAt))
	}

	sig := getString(payment, "solana_signature")
	if sig != "" {
		fmt.Println()
		Section("Solana Transaction")
		KeyValue("Signature", truncateAddress(sig))
	}

	// Next steps — check by status (more reliable than can_confirm field)
	if status == "pre_approved" || status == "approved" {
		fmt.Println()
		Section("Next Step")
		if txID != "" {
			InfoMsg("Run to execute:")
			fmt.Printf("  botwallet pay confirm %s\n", txID)
		}
	}
}

func formatStatus(status string) string {
	switch status {
	case "pre_approved":
		return "PRE-APPROVED ✓"
	case "awaiting_approval":
		return "AWAITING APPROVAL"
	case "approved":
		return "APPROVED ✓"
	case "pending":
		return "PENDING"
	case "completed":
		return "COMPLETED ✓"
	case "failed":
		return "FAILED ✗"
	case "expired":
		return "EXPIRED"
	case "rejected":
		return "REJECTED ✗"
	default:
		return strings.ToUpper(status)
	}
}

func formatStatusShort(status string) string {
	switch status {
	case "pre_approved":
		return "PRE-APPROVED"
	case "awaiting_approval":
		return "AWAITING"
	case "approved":
		return "APPROVED"
	case "pending":
		return "PENDING"
	case "completed":
		return "COMPLETED"
	case "failed":
		return "FAILED"
	case "expired":
		return "EXPIRED"
	case "rejected":
		return "REJECTED"
	default:
		return strings.ToUpper(status)
	}
}

func formatExpiryRelative(expiresAt string) string {
	// Try to parse and show human-relative time
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		// Fall back to truncated string
		if len(expiresAt) > 10 {
			return expiresAt[:10]
		}
		return expiresAt
	}

	now := time.Now()
	if t.Before(now) {
		return "expired"
	}

	diff := t.Sub(now)
	hours := int(diff.Hours())

	if hours < 1 {
		mins := int(diff.Minutes())
		if mins < 5 {
			return "expires soon"
		}
		return fmt.Sprintf("in %dm", mins)
	} else if hours < 24 {
		return fmt.Sprintf("in %dh", hours)
	} else {
		days := hours / 24
		return fmt.Sprintf("in %dd", days)
	}
}

// FormatLookup formats a lookup result.
// API returns: { found, username, name, type } or { found: false, username, suggestion }
func FormatLookup(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	found := getBool(data, "found")
	username := getString(data, "username")
	name := getString(data, "name")
	recipientType := getString(data, "type")

	if found {
		SuccessMsg("Found: @%s", username)
		if name != "" {
			KeyValue("Name", name)
		}
		if recipientType != "" {
			KeyValue("Type", recipientType)
		}
		Tip("Use 'botwallet pay @%s <amount>' to send money.", username)
	} else {
		ErrorMsg("Not found: @%s", username)
		if suggestion := getString(data, "suggestion"); suggestion != "" {
			fmt.Printf("   %s\n", suggestion)
		} else {
			fmt.Println()
			Dim.Println("💡 Tips:")
			fmt.Println("   • Check the username spelling")
			fmt.Println("   • Verify the recipient has registered at https://botwallet.co")
			fmt.Println("   • Usernames are case-sensitive")
		}
	}
}

// FormatCanIAfford formats a can-i-afford check result
func FormatCanIAfford(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	canPay := getBool(data, "can_pay")

	if canPay {
		SuccessMsg("Yes, you can afford this payment!")

		Section("Payment Preview")
		KeyValue("To", getString(data, "to"))
		KeyValueMoney("Amount", getFloat(data, "amount_usdc", "amount"))
		KeyValueMoney("Fee", getFloat(data, "fee_usdc", "fee"))
		KeyValueMoney("Total", getFloat(data, "total_usdc", "total"))
		KeyValueMoney("Balance After", getFloat(data, "balance_after_usdc", "balance_after"))

		Tip("Use 'botwallet pay @%s %.2f' to complete the payment.", getString(data, "to"), getFloat(data, "amount_usdc", "amount"))
	} else {
		reason := getString(data, "reason")
		ErrorMsg("Cannot afford this payment: %s", reason)

		if msg := getString(data, "message"); msg != "" {
			fmt.Println()
			InfoMsg(msg)
		}

		if fundingURL := getString(data, "funding_url"); fundingURL != "" {
			KeyValueURL("Add funds at", fundingURL)
		}
	}
}

// FormatCreatePaymentRequest formats a created payment request.
// API returns: { request_id, short_code, payment_url, amount, description, reference, expires_at, message }
func FormatCreatePaymentRequest(data map[string]interface{}) {
	if !humanOutput {
		requestID := getString(data, "request_id")
		if requestID != "" {
			data["send_to_email"] = fmt.Sprintf("To email as invoice: botwallet paylink send %s --to recipient@example.com", requestID)
		}
		JSON(data)
		return
	}

	SuccessMsg("Payment request created!")

	paymentURL := getString(data, "payment_url")
	if paymentURL != "" {
		Box("Share this link to get paid", paymentURL)
	}

	Section("Request Details")
	requestID := getString(data, "request_id", "payment_request_id")
	if requestID != "" {
		KeyValue("Request ID", requestID)
	}
	shortCode := getString(data, "short_code")
	if shortCode != "" {
		KeyValue("Short Code", shortCode)
	}
	KeyValueMoney("Amount", getFloat(data, "amount_usdc", "amount"))
	description := getString(data, "description")
	if description != "" {
		KeyValue("Description", description)
	}
	expiresAt := getString(data, "expires_at")
	if expiresAt != "" {
		KeyValue("Expires", formatExpiryRelative(expiresAt))
	}

	if requestID != "" {
		Tip("Send as invoice: botwallet paylink send %s --to recipient@example.com", requestID)
	}
}

// FormatSendPaylinkInvitation formats the result of sending a paylink to email or bot.
func FormatSendPaylinkInvitation(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	sentTo := getString(data, "sent_to")
	to := getString(data, "to")

	if sentTo == "wallet" {
		SuccessMsg("Payment request delivered to %s's inbox!", to)
	} else {
		SuccessMsg("Payment request sent!")
	}

	Section("Details")
	if to != "" {
		KeyValue("Sent to", to)
	}
	KeyValueMoney("Amount", getFloat(data, "amount_usdc", "amount"))
	if shortCode := getString(data, "short_code"); shortCode != "" {
		KeyValue("Short Code", shortCode)
	}
	if payURL := getString(data, "payment_url"); payURL != "" {
		KeyValueURL("Payment URL", payURL)
	}

	shortCode := getString(data, "short_code")
	if sentTo == "wallet" {
		Tip("The bot can pay with: botwallet pay --paylink <request_id>")
		Tip("Check status with: botwallet paylink get %s", shortCode)
	} else {
		Tip("Check status with: botwallet paylink get %s", shortCode)
	}
}

// FormatTransactions formats transaction history.
// API contract (handleTransactions in history.ts):
//
//	amount: centsToDollars(tx.amount_cents) * amountMultiplier
//	where amountMultiplier is -1 for payment/withdrawal, +1 for deposit, direction-based for adjustment
//	→ API returns SIGNED amounts. CLI trusts the sign directly.
func FormatTransactions(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	transactions := getSlice(data, "transactions")
	total := getInt(data, "total")

	Section(fmt.Sprintf("Transactions (%d total)", total))

	if len(transactions) == 0 {
		InfoMsg("No transactions yet.")
		return
	}

	rows := make([]TableRow, 0, len(transactions))
	for _, tx := range transactions {
		t, ok := tx.(map[string]interface{})
		if !ok {
			continue
		}

		txType := getString(t, "type")
		amount := getFloat(t, "amount_usdc", "amount")

		// API returns SIGNED amounts (negative = outgoing, positive = incoming).
		// We trust the API's sign — no type-based re-derivation needed.
		var amountStr string
		var amountColor *color.Color
		if amount < 0 {
			amountStr = fmt.Sprintf("-$%.2f", -amount) // -amount converts to positive for display
			amountColor = color.New(color.FgRed)
		} else {
			amountStr = fmt.Sprintf("+$%.2f", amount)
			amountColor = color.New(color.FgGreen)
		}

		// Human-readable type labels
		displayType := txType
		switch txType {
		case "payment":
			displayType = "sent"
		case "deposit":
			displayType = "received"
		case "withdrawal":
			displayType = "withdraw"
		case "adjustment":
			if amount >= 0 {
				displayType = "credit"
			} else {
				displayType = "debit"
			}
		}

		// Counterparty details
		details := getString(t, "counterparty")
		if details == "" {
			details = getString(t, "description")
		}
		if details != "" {
			// Add @ prefix for usernames (not addresses or special labels)
			if !strings.HasPrefix(details, "External") &&
				!strings.HasPrefix(details, "Initial") &&
				!strings.HasPrefix(details, "Payment") &&
				!strings.HasPrefix(details, "@") &&
				len(details) < 44 &&
				!strings.Contains(details, " ") {
				details = "@" + details
			}
		}
		if len(details) > 20 {
			details = details[:17] + "..."
		}

		// Short transaction ID
		txID := getString(t, "id")
		if len(txID) > 8 {
			txID = txID[:8]
		}

		dateStr := getString(t, "timestamp")
		date := ""
		if len(dateStr) >= 10 {
			date = dateStr[:10]
		}

		rows = append(rows, TableRow{
			Columns: []string{
				date,
				displayType,
				amountStr,
				details,
				txID,
			},
			Colors: map[int]*color.Color{2: amountColor},
		})
	}

	Table([]string{"Date", "Type", "Amount", "Details", "ID"}, rows)

	if getBool(data, "has_more") {
		Tip("Use --limit and --offset for pagination.")
	}
}

// FormatLimits formats spending limits
func FormatLimits(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	// Summary message
	if msg, ok := data["message"].(string); ok && msg != "" {
		InfoMsg(msg)
		fmt.Println()
	}

	// --- Hard Limits ---
	Section("Hard Limits")
	if hardCap, ok := data["hard_cap"].(float64); ok && hardCap > 0 {
		KeyValueMoney("Max per transaction", hardCap)
	} else {
		KeyValue("Max per transaction", "No limit")
	}
	if hardCapBudget, ok := data["hard_cap_budget"].(float64); ok && hardCapBudget > 0 {
		period := "weekly"
		if p, ok := data["hard_cap_budget_period"].(string); ok {
			period = p
		}
		KeyValue("Spending ceiling", fmt.Sprintf("$%.2f / %s", hardCapBudget, period))
	} else {
		KeyValue("Spending ceiling", "No limit")
	}

	// --- Auto-Approve ---
	Section("Auto-Approve")
	if autoMax, ok := data["auto_approve_max"].(float64); ok && autoMax > 0 {
		KeyValueMoney("Up to", autoMax)
		if budget, ok := data["budget"].(float64); ok && budget > 0 {
			period := "daily"
			if p, ok := data["budget_period"].(string); ok {
				period = p
			}
			KeyValue("Budget", fmt.Sprintf("$%.2f / %s", budget, period))
		}
	} else {
		Dim.Println("  All transactions require approval")
	}

	// --- Budget Usage ---
	autoApproveMax, _ := data["auto_approve_max"].(float64)
	if autoApproveMax > 0 {
		Section("Auto-Approve Budget Usage")
		if spent, ok := data["spent_this_period"].(float64); ok {
			period := "daily"
			if p, ok := data["budget_period"].(string); ok {
				period = p
			}
			KeyValue(fmt.Sprintf("Spent this %s period", period), fmt.Sprintf("$%.2f", spent))
		}
		if remaining, ok := data["remaining_budget"].(float64); ok {
			KeyValueMoney("Auto-approve remaining", remaining)
		}
	} else {
		Section("Budget")
		Dim.Println("  No auto-approve budget configured — all transactions require owner approval.")
	}
	if balance, ok := data["balance"].(float64); ok {
		KeyValueMoney("Wallet balance", balance)
	}

	// --- Withdrawals ---
	Section("Withdrawals")
	if allowed, ok := data["allow_withdrawal_requests"].(bool); ok && allowed {
		KeyValue("Status", "Allowed (always requires approval)")
		if maxW, ok := data["max_withdrawal"].(float64); ok && maxW > 0 {
			KeyValueMoney("Max withdrawal", maxW)
		}
	} else {
		KeyValue("Status", "Disabled")
	}

	// --- Paylinks ---
	Section("Paylinks")
	if allowed, ok := data["allow_paylinks"].(bool); ok && allowed {
		KeyValue("Status", "Enabled")
		if maxP, ok := data["max_paylink_amount"].(float64); ok && maxP > 0 {
			KeyValueMoney("Max paylink amount", maxP)
		}
	} else {
		KeyValue("Status", "Disabled")
	}

	// --- Firewall ---
	if fw, ok := data["firewall_enabled"].(bool); ok && fw {
		Section("Firewall")
		KeyValue("Mode", "Whitelist only - transactions limited to trusted recipients")
	}

	// --- Portal link ---
	if portal, ok := data["guard_rails_portal"].(string); ok {
		fmt.Println()
		KeyValueURL("Manage guard rails", portal)
	}
}

// FormatPendingApprovals formats pending approvals.
// API returns: { pending: [{ approval_id, type, amount, recipient, note, triggered_by, created_at, expires_at }], count }
func FormatPendingApprovals(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	pending := getSlice(data, "pending")
	count := getInt(data, "count")

	Section(fmt.Sprintf("Pending Approvals (%d)", count))

	if count == 0 || len(pending) == 0 {
		SuccessMsg("No pending approvals!")
		return
	}

	for _, p := range pending {
		approval, ok := p.(map[string]interface{})
		if !ok {
			continue
		}

		fmt.Println()
		KeyValue("ID", getString(approval, "approval_id"))
		KeyValue("Type", getString(approval, "type"))
		KeyValueMoney("Amount", getFloat(approval, "amount_usdc", "amount"))

		recipient := getString(approval, "recipient")
		if recipient != "" {
			KeyValue("To", ensureAtPrefix(recipient))
		}

		// Show note if present
		note := getString(approval, "note")
		if note != "" {
			KeyValue("Note", note)
		}

		reason := getString(approval, "triggered_by")
		if reason != "" {
			KeyValue("Triggered By", reason)
		}

		// Show created/expires times
		createdAt := getString(approval, "created_at")
		if createdAt != "" && len(createdAt) >= 10 {
			KeyValue("Created", createdAt[:10])
		}
		expiresAt := getString(approval, "expires_at")
		if expiresAt != "" {
			KeyValue("Expires", formatExpiryRelative(expiresAt))
		}

		approvalURL := getString(approval, "approval_url")
		if approvalURL != "" {
			KeyValueURL("Approval URL", approvalURL)
		}
	}

	fmt.Println()
	Tip("Ask your human owner to approve pending actions in their portal.")
}

// FormatEvents formats the events list response.
func FormatEvents(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	events := getSlice(data, "events")
	unreadCount := getInt(data, "unread_count")

	Section(fmt.Sprintf("Events (%d unread)", unreadCount))

	if len(events) == 0 {
		SuccessMsg("No events. You are up to date!")
		Tip("Events appear when your human approves payments, funds arrive, etc.")
		return
	}

	for _, e := range events {
		evt, ok := e.(map[string]interface{})
		if !ok {
			continue
		}

		severity := getString(evt, "severity")
		title := getString(evt, "title")
		message := getString(evt, "message")
		eventType := getString(evt, "type")
		isRead := getBool(evt, "is_read")
		createdAt := getString(evt, "created_at")

		// Severity indicator
		switch severity {
		case "action_required":
			Warning.Print("  ● ")
		case "warning":
			Warning.Print("  ▲ ")
		default:
			Info.Print("  ○ ")
		}

		// Title with read status
		if isRead {
			Dim.Printf("%s", title)
		} else {
			Bold.Printf("%s", title)
		}

		// Timestamp
		dateStr := ""
		if len(createdAt) >= 16 {
			dateStr = createdAt[:16]
		}
		Dim.Printf("  [%s]", dateStr)
		fmt.Println()

		// Message body
		if message != "" {
			Dim.Printf("    %s\n", message)
		}

		// Event type tag
		Dim.Printf("    type: %s\n", eventType)

		fmt.Println()
	}

	if unreadCount > 0 {
		Tip("Use 'botwallet events --mark-read' to acknowledge events.")
	}
}

// FormatMarkRead formats the mark-read response.
func FormatMarkRead(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	count := getInt(data, "marked_read")
	if count > 0 {
		SuccessMsg("%d event(s) marked as read.", count)
	} else {
		InfoMsg("No unread events to mark.")
	}
}

// FormatDepositAddress formats deposit address info
func FormatDepositAddress(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	Section("Deposit Information")
	KeyValueHighlight("Deposit Address", getString(data, "deposit_address"))
	KeyValueURL("Funding URL", getString(data, "funding_url"))
	KeyValueMoney("Current Balance", getFloat(data, "balance"))

	instructions := getString(data, "instructions")
	if instructions != "" {
		fmt.Println()
		InfoMsg(instructions)
	}

	Tip("Share the funding URL with your human owner for easy deposits.")
}

// FormatWithdraw formats the initial withdrawal request response (Step 1).
// API returns: { status: 'awaiting_approval', withdrawal_id, approval_id, approval_url, amount, ... }
func FormatWithdraw(data map[string]interface{}) {
	withdrawalID := getString(data, "withdrawal_id")
	approvalID := getString(data, "approval_id")

	if !humanOutput {
		data["ready_to_confirm"] = false
		if approvalID != "" {
			data["agent_hint"] = "Save approval_id to persistent memory. Check status periodically until approved, then run confirm_command."
		}
		JSON(data)
		return
	}

	WarningMsg("Withdrawal requires owner approval")

	Section("Withdrawal Request")
	KeyValue("Withdrawal ID", withdrawalID)
	KeyValue("Approval ID", approvalID)
	KeyValueMoney("Amount", getFloat(data, "amount_usdc", "amount"))
	KeyValueMoney("Network Fee", getFloat(data, "network_fee_usdc", "network_fee"))
	KeyValueMoney("You'll Receive", getFloat(data, "you_receive_usdc", "you_receive"))
	KeyValue("To Address", getString(data, "to_address"))

	fmt.Println()
	InfoMsg("All withdrawals require owner approval.")

	approvalURL := getString(data, "approval_url")
	if approvalURL != "" {
		KeyValueURL("Approval URL", approvalURL)
	}

	Section("Next Steps")
	fmt.Println()
	Bold.Println("  Three steps required:")
	fmt.Println()
	fmt.Print("  1. ")
	Warning.Print("Ask your human")
	fmt.Print(" to approve at:\n")
	if approvalURL != "" {
		fmt.Printf("     %s\n", approvalURL)
	}
	fmt.Println()
	fmt.Print("  2. ")
	Warning.Print("Check status")
	fmt.Print(" with:\n")
	if approvalID != "" {
		Highlight.Printf("     botwallet approval status %s\n", approvalID)
	}
	fmt.Println()
	fmt.Print("  3. ")
	Warning.Print("After approved")
	fmt.Print(", run:\n")
	Highlight.Printf("     botwallet withdraw confirm %s\n", withdrawalID)
	fmt.Println()
	Dim.Println("  ❌ Withdrawal does NOT auto-execute after approval!")
	Tip("Save the approval_id to memory. Check periodically until the human approves, then run the confirm command.")
}

// FormatWithdrawSuccess formats a successful withdrawal (after FROST signing).
func FormatWithdrawSuccess(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	SuccessMsg("Withdrawal submitted to Solana!")

	Section("Withdrawal Details")
	if txID := getString(data, "transaction_id"); txID != "" {
		KeyValue("Transaction ID", txID)
	}
	KeyValueMoney("Amount", getFloat(data, "amount_usdc", "amount"))
	KeyValueMoney("Fee", getFloat(data, "fee_usdc", "fee"))
	KeyValue("To Address", getString(data, "to_address"))

	if solanaSig := getString(data, "solana_signature"); solanaSig != "" {
		fmt.Println()
		Section("Solana Transaction")
		KeyValue("Signature", truncateAddress(solanaSig))
		if explorerURL := getString(data, "explorer_url"); explorerURL != "" {
			KeyValueURL("View on Explorer", explorerURL)
		}
		if network := getString(data, "network"); network != "" {
			KeyValue("Network", network)
		}
	}

	if newBalance := getFloat(data, "new_balance_usdc", "new_balance"); newBalance > 0 {
		fmt.Println()
		Label.Print("  New Balance: ")
		Money.Printf("$%.2f", newBalance)
		Dim.Println(" (estimate)")
	}
}

// =============================================================================
// Approval Formatters
// =============================================================================

// FormatApprovalStatus formats the status of a single approval.
// Designed for agents polling for human decisions.
func FormatApprovalStatus(data map[string]interface{}) {
	status := getString(data, "status")
	approvalType := getString(data, "type")
	actionable := getBool(data, "actionable")

	if !humanOutput {
		JSON(data)
		return
	}

	// Type-aware label
	typeLabel := "Payment"
	switch approvalType {
	case "x402_payment":
		typeLabel = "API Payment"
	case "withdrawal":
		typeLabel = "Withdrawal"
	}

	switch status {
	case "pending":
		WarningMsg("%s approval is still pending", typeLabel)
		Section("Approval Details")
		KeyValue("ID", getString(data, "approval_id"))
		KeyValue("Type", typeLabel)
		KeyValueMoney("Amount", getFloat(data, "amount_usdc", "amount"))
		if recipient := getString(data, "recipient"); recipient != "" {
			if approvalType == "x402_payment" || len(recipient) > 30 {
				KeyValue("Recipient", truncateAddress(recipient))
			} else {
				KeyValue("To", ensureAtPrefix(recipient))
			}
		}
		if note := getString(data, "note"); note != "" {
			KeyValue("Note", note)
		}
		expiresAt := getString(data, "expires_at")
		if expiresAt != "" {
			KeyValue("Expires", formatExpiryRelative(expiresAt))
		}
		fmt.Println()
		Dim.Println("  Waiting for human owner to approve or reject.")
		Dim.Println("  Check again later with: botwallet approval status <id>")

	case "approved":
		SuccessMsg("%s approved by owner!", typeLabel)
		Section("Approval Details")
		KeyValue("ID", getString(data, "approval_id"))
		KeyValue("Type", typeLabel)
		KeyValueMoney("Amount", getFloat(data, "amount_usdc", "amount"))
		if recipient := getString(data, "recipient"); recipient != "" {
			if approvalType == "x402_payment" || len(recipient) > 30 {
				KeyValue("Recipient", truncateAddress(recipient))
			} else {
				KeyValue("To", ensureAtPrefix(recipient))
			}
		}
		if resolvedAt := getString(data, "resolved_at"); resolvedAt != "" && len(resolvedAt) >= 16 {
			KeyValue("Approved at", resolvedAt[:16])
		}
		if confirmCmd := getString(data, "confirm_command"); confirmCmd != "" {
			fmt.Println()
			Section("Next Step")
			InfoMsg("Run: %s", confirmCmd)
		}

	case "rejected":
		ErrorMsg("%s rejected by owner", typeLabel)
		Section("Approval Details")
		KeyValue("ID", getString(data, "approval_id"))
		KeyValue("Type", typeLabel)
		KeyValueMoney("Amount", getFloat(data, "amount_usdc", "amount"))
		if recipient := getString(data, "recipient"); recipient != "" {
			if approvalType == "x402_payment" || len(recipient) > 30 {
				KeyValue("Recipient", truncateAddress(recipient))
			} else {
				KeyValue("To", ensureAtPrefix(recipient))
			}
		}
		if reason := getString(data, "rejection_reason"); reason != "" {
			KeyValue("Reason", reason)
		}
		fmt.Println()
		Dim.Println("  The owner declined this action. It cannot proceed.")

	case "expired":
		WarningMsg("%s approval expired", typeLabel)
		Section("Approval Details")
		KeyValue("ID", getString(data, "approval_id"))
		KeyValue("Type", typeLabel)
		KeyValue("Expired at", getString(data, "expires_at"))
		fmt.Println()
		Dim.Println("  The approval window has closed. Re-initiate the action if still needed.")

	default:
		InfoMsg("Approval status: %s", status)
		KeyValue("ID", getString(data, "approval_id"))
		if actionable {
			if nextStep := getString(data, "next_step"); nextStep != "" {
				fmt.Println()
				InfoMsg(nextStep)
			}
		}
	}
}

// =============================================================================
// x402 Formatters
// =============================================================================

// FormatX402Fetch formats the Step 1 probe result from the server.
// Shows price, status, fetch_id, and next steps — mirrors FormatPayInitiated.
func FormatX402Fetch(data map[string]interface{}) {
	if !humanOutput {
		status := getString(data, "status")

		switch status {
		case "pre_approved":
			data["ready_to_confirm"] = true
		case "awaiting_approval":
			data["ready_to_confirm"] = false
			data["agent_hint"] = "Save approval_id to persistent memory. Check status periodically until approved, then run confirm_command."
		case "rejected", "blocked":
			data["ready_to_confirm"] = false
		}

		JSON(data)
		return
	}

	status := getString(data, "status")
	fetchID := getString(data, "fetch_id")
	url := getString(data, "url")

	switch status {
	case "pre_approved":
		SuccessMsg("x402 API probed - PRE-APPROVED ✓")
	case "awaiting_approval":
		WarningMsg("x402 API probed - AWAITING APPROVAL")
	case "rejected":
		ErrorMsg("x402 payment BLOCKED by guard rail")
	default:
		InfoMsg("x402 API probed (status: %s)", status)
	}

	Section("Price Details")
	if url != "" {
		KeyValue("URL", url)
	}
	if fetchID != "" {
		KeyValue("Fetch ID", fetchID)
	}
	KeyValue("Status", formatStatus(status))
	fmt.Println()

	payTo := getString(data, "pay_to")
	if payTo != "" {
		KeyValue("Pay To", truncateAddress(payTo))
	}

	amount := getFloat(data, "amount_usdc")
	if amount > 0 {
		KeyValueMoney("Price", amount)
	}
	fee := getFloat(data, "fee_usdc")
	if fee > 0 {
		KeyValueMoney("Fee", fee)
	}
	total := getFloat(data, "total_usdc")
	if total > 0 {
		KeyValueMoney("Total", total)
	}

	balanceAfter := getFloat(data, "balance_after_usdc")
	if balanceAfter > 0 {
		KeyValueMoney("Balance After", balanceAfter)
	}

	expiresAt := getString(data, "expires_at")
	if expiresAt != "" {
		KeyValue("Expires", formatExpiryRelative(expiresAt))
	}

	fmt.Println()

	switch status {
	case "pre_approved":
		Section("Next Step")
		InfoMsg("Ready to fetch! Run:")
		fmt.Printf("  botwallet x402 fetch confirm %s\n", fetchID)
	case "awaiting_approval":
		approvalID := getString(data, "approval_id")
		Section("Awaiting Human Approval")
		if guardRail := getMap(data, "guard_rail"); guardRail != nil {
			if grName := getString(guardRail, "name"); grName != "" {
				KeyValue("Triggered by", grName)
			}
		}
		if approvalID != "" {
			KeyValue("Approval ID", approvalID)
		}
		fmt.Println()
		Bold.Println("  Three steps required:")
		fmt.Println()
		fmt.Print("  1. ")
		Warning.Print("Ask your human")
		fmt.Print(" to approve at:\n")
		if approvalURL := getString(data, "approval_url"); approvalURL != "" {
			fmt.Printf("     %s\n", approvalURL)
		}
		fmt.Println()
		fmt.Print("  2. ")
		Warning.Print("Check status")
		fmt.Print(" with:\n")
		if approvalID != "" {
			Highlight.Printf("     botwallet approval status %s\n", approvalID)
		}
		fmt.Println()
		fmt.Print("  3. ")
		Warning.Print("After approved")
		fmt.Print(", run:\n")
		Highlight.Printf("     botwallet x402 fetch confirm %s\n", fetchID)
		fmt.Println()
		Dim.Println("  ❌ Payment does NOT auto-execute after approval!")
		Tip("Save the approval_id to memory. Check periodically until the human approves, then run the confirm command.")
	case "rejected":
		Section("Blocked by Guard Rail")
		if guardRail := getMap(data, "guard_rail"); guardRail != nil {
			if grName := getString(guardRail, "name"); grName != "" {
				KeyValue("Guard Rail", grName)
			}
		}
		if msg := getString(data, "message"); msg != "" {
			fmt.Println()
			InfoMsg(msg)
		}
	}
}

// FormatX402FetchConfirm formats a successful x402 fetch with payment.
func FormatX402FetchConfirm(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	SuccessMsg("x402 API data retrieved!")

	url := getString(data, "url")
	if url != "" {
		KeyValue("URL", url)
	}

	amountPaid := getFloat(data, "amount_paid")
	if amountPaid > 0 {
		KeyValueMoney("Paid", amountPaid)
	}

	newBalance := getFloat(data, "new_balance")
	if newBalance > 0 {
		fmt.Println()
		Label.Print("  New Balance: ")
		Money.Printf("$%.2f", newBalance)
		Dim.Println(" (estimate)")
	}

	Section("Response")
	// Print the response data
	if respText := getString(data, "response_text"); respText != "" {
		fmt.Println(respText)
	} else if resp := getMap(data, "response"); resp != nil {
		JSON(resp)
	}
}

// FormatX402Discover formats the output of the x402 discover command.
func FormatX402Discover(items []x402.DiscoveredResource, totalOnServer int, query string, solanaOnly bool, limit, offset int) {
	if !humanOutput {
		results := make([]map[string]interface{}, 0, len(items))
		for _, item := range items {
			price, network := x402.ResourceBestPrice(&item)
			priceUSDC, _ := x402.ResourceBestPriceUSDC(&item)
			entry := map[string]interface{}{
				"url":               item.Resource,
				"description":       x402.ResourceDescription(&item),
				"price":             price,
				"price_usdc":        priceUSDC,
				"network":           network,
				"solana_compatible": x402.HasSolanaOption(&item),
				"last_updated":      item.LastUpdated,
				"next_step":         "botwallet x402 fetch " + item.Resource,
			}
			if item.Metadata != nil {
				entry["metadata"] = item.Metadata
			}
			if len(item.Accepts) > 1 {
				var networks []string
				for _, opt := range item.Accepts {
					networks = append(networks, opt.Network)
				}
				entry["networks"] = networks
			}
			results = append(results, entry)
		}

		out := map[string]interface{}{
			"results":     results,
			"total":       totalOnServer,
			"showing":     len(results),
			"solana_only": solanaOnly,
		}
		if query != "" {
			out["query"] = query
		}
		out["tip"] = "Use 'botwallet x402 fetch <url>' to probe any API"
		if (offset + limit) < totalOnServer {
			out["next_page"] = fmt.Sprintf("botwallet x402 discover --bazaar --offset %d", offset+limit)
		}
		JSON(out)
		return
	}

	// Human output
	if len(items) == 0 {
		WarningMsg("No APIs found")
		if solanaOnly {
			Tip("Try --all to include non-Solana networks")
		}
		if query != "" {
			Tip("Try a broader search or omit the query to list all")
		}
		return
	}

	Section("x402 Bazaar — Discovered APIs")

	if query != "" {
		KeyValue("Search", query)
	}
	if solanaOnly {
		KeyValue("Filter", "Solana-compatible only (use --all for all networks)")
	}
	fmt.Printf("  Showing %d of %d total\n\n", len(items), totalOnServer)

	headers := []string{"#", "URL", "Description", "Price (USDC)", "Network"}
	rows := make([]TableRow, 0, len(items))
	for i, item := range items {
		priceUSDC, network := x402.ResourceBestPriceUSDC(&item)
		desc := x402.ResourceDescription(&item)

		// Truncate long URLs/descriptions for terminal readability
		urlDisplay := item.Resource
		if len(urlDisplay) > 50 {
			urlDisplay = urlDisplay[:47] + "..."
		}
		if len(desc) > 35 {
			desc = desc[:32] + "..."
		}

		priceDisplay := "—"
		if priceUSDC > 0 {
			priceDisplay = fmt.Sprintf("$%.4f", priceUSDC)
		}

		row := TableRow{
			Columns: []string{
				fmt.Sprintf("%d", i+1+offset),
				urlDisplay,
				desc,
				priceDisplay,
				network,
			},
		}
		if x402.HasSolanaOption(&item) {
			row.Colors = map[int]*color.Color{4: Success}
		}
		rows = append(rows, row)
	}
	Table(headers, rows)

	fmt.Println()
	Tip("Probe an API: botwallet x402 fetch <url>")
	if (offset + limit) < totalOnServer {
		Tip("Next page: botwallet x402 discover --bazaar --offset %d", offset+limit)
	}
}

// FormatX402Catalog formats the output of the curated catalog list.
func FormatX402Catalog(entries []x402.CatalogEntry, query string) {
	if !humanOutput {
		results := make([]map[string]interface{}, 0, len(entries))
		for _, e := range entries {
			entry := map[string]interface{}{
				"slug":        e.Slug,
				"name":        e.Name,
				"description": e.Description,
				"url":         e.URL,
				"method":      e.Method,
				"price_usdc":  e.PriceUSDC,
				"network":     e.Network,
				"category":    e.Category,
				"next_step":   x402FetchHint(e.Method, e.URL),
			}
			if e.Metadata != nil {
				entry["metadata"] = e.Metadata
			}
			results = append(results, entry)
		}
		out := map[string]interface{}{
			"results": results,
			"total":   len(results),
		}
		if query != "" {
			out["query"] = query
		}
		out["tip"] = "Use 'botwallet x402 fetch <url>' to probe any API"
		JSON(out)
		return
	}

	if len(entries) == 0 {
		WarningMsg("No APIs found in catalog")
		if query != "" {
			Tip("Try a broader search or omit the query to list all")
		}
		Tip("Use --bazaar to search the full x402 Bazaar (all networks)")
		return
	}

	Section("x402 Catalog — Verified Solana APIs")

	if query != "" {
		KeyValue("Search", query)
	}
	fmt.Printf("  %d APIs available\n\n", len(entries))

	headers := []string{"#", "Name", "Description", "Price (USDC)", "Category"}
	rows := make([]TableRow, 0, len(entries))
	for i, e := range entries {
		desc := e.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}

		row := TableRow{
			Columns: []string{
				fmt.Sprintf("%d", i+1),
				e.Name,
				desc,
				fmt.Sprintf("$%.4f", e.PriceUSDC),
				e.Category,
			},
			Colors: map[int]*color.Color{3: Success},
		}
		rows = append(rows, row)
	}
	Table(headers, rows)

	fmt.Println()
	for _, e := range entries {
		fmt.Printf("  %s%s%s\n", color.New(color.Bold).Sprint(e.Name), "  →  ", color.New(color.FgCyan).Sprint(e.URL))
	}

	fmt.Println()
	Tip("Probe an API: botwallet x402 fetch <url>")
	Tip("Search the full Bazaar: botwallet x402 discover --bazaar")
}

func x402FetchHint(method, url string) string {
	if method == "" || strings.EqualFold(method, "GET") {
		return "botwallet x402 fetch " + url
	}
	return "botwallet x402 fetch --method " + strings.ToUpper(method) + " " + url
}
