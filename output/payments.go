package output

import (
	"fmt"
	"strings"
)

// =============================================================================
// Payment Formatters
// =============================================================================

// feeBreakdownSublines shows indented ATA fee details when present in API response.
// Only shows sub-lines when account_setup_fee > 0.
func feeBreakdownSublines(data map[string]interface{}) {
	if !humanOutput {
		return
	}
	breakdown, ok := data["fee_breakdown"].(map[string]interface{})
	if !ok {
		return
	}
	setupFee := 0.0
	if v, ok := breakdown["account_setup_fee_usdc"].(float64); ok {
		setupFee = v
	}
	if setupFee <= 0 {
		return
	}
	platformFee := 0.0
	if v, ok := breakdown["platform_fee_usdc"].(float64); ok {
		platformFee = v
	}
	styleDim.Printf("    Platform fee:    $%.2f\n", platformFee)
	styleDim.Printf("    Account setup:   $%.2f  (one-time for new recipient)\n", setupFee)
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
		KeyValue("To", ensureAtPrefix(toName))
	}
	if toAddr, ok := data["to_address"].(string); ok && toAddr != "" {
		KeyValue("Address", truncateAddress(toAddr))
	}
	KeyValueMoney("Amount", amount)

	fee := getFloat(data, "fee_usdc", "fee")
	if fee > 0 {
		KeyValueMoney("Fee", fee)
		feeBreakdownSublines(data)
	}

	// Total (amount + fee)
	KeyValueMoney("Total", amount+fee)
	fmt.Println()

	if newBalance := getFloat(data, "new_balance_usdc", "new_balance"); newBalance > 0 {
		printNewBalanceEstimate(newBalance)
	}

	// Transaction ID
	if txID, ok := data["transaction_id"].(string); ok {
		KeyValue("Transaction ID", txID)
	}

	if solanaSig := getString(data, "solana_signature"); solanaSig != "" {
		fmt.Println()
		printSolanaTx(solanaSig, getString(data, "explorer_url"), getString(data, "network"))
	}

	if getBool(data, "low_balance") {
		printLowBalance("Balance is getting low!", getString(data, "funding_url"))
	}
}

// FormatPayInitiated formats a payment initiation response (two-step flow).
// API returns: { status, transaction_id, reference, amount, fee, total, to, to_address, message, guard_rail, approval_url, ... }
func FormatPayInitiated(data map[string]interface{}) {
	// For JSON output (default for bots), add ready_to_confirm and agent_hint
	if !humanOutput {
		JSON(initiatedForAgent("payment", data))
		return
	}

	status := getString(data, "status")
	txID := getString(data, "transaction_id")

	// Status-specific header
	initiatedHeader(status, "Payment initiated", "Payment")

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
		feeBreakdownSublines(data)
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
			styleDim.Printf("  %s\n", msg)
		}

		printApprovalSteps(getString(data, "approval_url"), approvalID, "botwallet pay confirm "+txID, "Payment")
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
				styleDim.Printf("  %s\n", grDesc)
			}
		}
		if msg := getString(data, "message"); msg != "" {
			fmt.Println()
			InfoMsg("%s", msg)
		}
		if howToFix := getString(data, "how_to_fix"); howToFix != "" {
			fmt.Println()
			styleInfo.Print("   → ")
			fmt.Println(howToFix)
		}
	}
}

// PaymentConfirmSummary shows, before Key 1 signs, the payment that pay
// confirm is about to send (human mode only). confirm is the server's
// confirm response.
func PaymentConfirmSummary(confirm map[string]interface{}) {
	to := getString(confirm, "to")
	if to == "" {
		if addr := getString(confirm, "to_address"); len(addr) > 8 {
			to = addr[:8] + "..."
		}
	}
	confirmSummary("Confirm Payment", confirm, true, [2]string{"To", "@" + to})
}

// confirmSummary prints the box a confirm command shows before Key 1
// signs: rows, then the amount, fee (with any one-time account setup fee),
// total and, with withNetwork, the network of confirm.
func confirmSummary(title string, confirm map[string]interface{}, withNetwork bool, rows ...[2]string) {
	if !humanOutput {
		return
	}

	feeLine := fmt.Sprintf("$%.2f USDC", getFloat(confirm, "fee_usdc"))
	if setupFee := getFloat(getMap(confirm, "fee_breakdown"), "account_setup_fee_usdc"); setupFee > 0 {
		feeLine += fmt.Sprintf(" (incl. $%.2f account setup)", setupFee)
	}
	rows = append(rows,
		[2]string{"Amount", fmt.Sprintf("$%.2f USDC", getFloat(confirm, "amount_usdc"))},
		[2]string{"Fee", feeLine},
		[2]string{"Total", fmt.Sprintf("$%.2f USDC", getFloat(confirm, "total_usdc"))})
	if network := getString(confirm, "network"); withNetwork && network != "" {
		rows = append(rows, [2]string{"Network", network})
	}

	lines := make([]string, len(rows))
	for i, row := range rows {
		lines[i] = fmt.Sprintf("%-7s %s", row[0]+":", row[1])
	}
	Box(title, strings.Join(lines, "\n"))
	fmt.Println()
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
	fmt.Println("  " + strings.Repeat("-", 94))

	for _, p := range payments {
		payment, ok := p.(map[string]interface{})
		if !ok {
			continue
		}

		status := formatStatusShort(getString(payment, "status"))

		// Full ID: pay confirm, pay cancel and pay list --id need all of it
		txID := getString(payment, "transaction_id")

		amountStr := formatUSD(getFloat(payment, "amount_usdc", "amount"))

		// Recipient — API already returns "@username", use ensureAtPrefix for safety
		to := truncate(ensureAtPrefix(getString(payment, "to")), 18)

		// Show created date instead of expires (more useful for completed payments)
		dateStr := datePrefix(getString(payment, "created_at"), 10)

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
					firstTxID = getString(payment, "transaction_id")
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
		printSolanaTx(sig, "", "")
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

// FormatPaymentCancelled formats a cancelled payment (pay cancel).
func FormatPaymentCancelled(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	SuccessMsg("Payment cancelled!")
	if id, ok := data["transaction_id"].(string); ok {
		KeyValue("Transaction ID", id)
	}
	if prev, ok := data["previous_status"].(string); ok {
		KeyValue("Was", prev)
	}
	if amount := getNonZeroFloat(data, "amount_usdc", "amount"); amount > 0 {
		KeyValueMoney("Amount", amount)
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
			styleDim.Println("💡 Tips:")
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
		feeBreakdownSublines(data)
		KeyValueMoney("Total", getFloat(data, "total_usdc", "total"))
		KeyValueMoney("Balance After", getFloat(data, "balance_after_usdc", "balance_after"))

		Tip("Use 'botwallet pay @%s %.2f' to complete the payment.", getString(data, "to"), getFloat(data, "amount_usdc", "amount"))
	} else {
		reason := getString(data, "reason")
		ErrorMsg("Cannot afford this payment: %s", reason)

		if msg := getString(data, "message"); msg != "" {
			fmt.Println()
			InfoMsg("%s", msg)
		}

		if fundingURL := getString(data, "funding_url"); fundingURL != "" {
			KeyValueURL("Add funds at", fundingURL)
		}
	}
}

// FormatSentUnconfirmed formats a payment or withdrawal that was sent to
// Solana and is waiting for the network to confirm it. It is not an error:
// it may still go through, so the output says how to check it instead of
// sending it again. what names it, e.g. "payment".
func FormatSentUnconfirmed(what string, data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	InfoMsg("%s", getString(data, "message"))

	Section(strings.ToUpper(what[:1]) + what[1:] + " Details")
	KeyValue("Transaction ID", getString(data, "transaction_id"))
	KeyValue("Status", "SENT, CONFIRMING")

	if solanaSig := getString(data, "solana_signature"); solanaSig != "" {
		printSolanaTx(solanaSig, getString(data, "explorer_url"), "")
	}

	fmt.Println()
	WarningMsg("It may still go through. Do not send it again.")
	if check := getString(data, "check_command"); check != "" {
		Tip("Check it in about a minute: %s", check)
	}
}
