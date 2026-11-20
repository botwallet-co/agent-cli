package output

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

// =============================================================================
// History, Limits, Approvals and Events
// =============================================================================

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
			amountColor = styleDebit
		} else {
			amountStr = fmt.Sprintf("+$%.2f", amount)
			amountColor = styleMoney
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
		details = truncate(details, 20)

		// Short transaction ID
		txID := getString(t, "id")
		if len(txID) > 8 {
			txID = txID[:8]
		}

		rows = append(rows, TableRow{
			Columns: []string{
				datePrefix(getString(t, "timestamp"), 10),
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
		InfoMsg("%s", msg)
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
		styleDim.Println("  All transactions require approval")
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
		styleDim.Println("  No auto-approve budget configured — all transactions require owner approval.")
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
		if created := datePrefix(getString(approval, "created_at"), 10); created != "" {
			KeyValue("Created", created)
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

		// Severity indicator
		switch severity {
		case "action_required":
			styleWarning.Print("  ● ")
		case "warning":
			styleWarning.Print("  ▲ ")
		default:
			styleInfo.Print("  ○ ")
		}

		// Title with read status
		if isRead {
			styleDim.Printf("%s", title)
		} else {
			styleBold.Printf("%s", title)
		}

		// Timestamp
		styleDim.Printf("  [%s]", datePrefix(getString(evt, "created_at"), 16))
		fmt.Println()

		// Message body
		if message != "" {
			styleDim.Printf("    %s\n", message)
		}

		// Event type tag and id (for --mark-read --ids)
		styleDim.Printf("    type: %s\n", eventType)
		if id := getString(evt, "id"); id != "" {
			styleDim.Printf("    id:   %s\n", id)
		}

		fmt.Println()
	}

	if unreadCount > 0 {
		Tip("After acting on events, mark them read: botwallet events --mark-read --ids <id>,<id>")
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

// FormatApprovalStatus formats the status of a single approval.
// Designed for agents polling for human decisions.
func FormatApprovalStatus(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	status := getString(data, "status")
	approvalType := getString(data, "type")
	actionable := getBool(data, "actionable")

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
		approvalID := getString(data, "approval_id")
		WarningMsg("%s approval is still pending", typeLabel)
		Section("Approval Details")
		KeyValue("ID", approvalID)
		KeyValue("Type", typeLabel)
		KeyValueMoney("Amount", getFloat(data, "amount_usdc", "amount"))
		approvalRecipient(approvalType, getString(data, "recipient"))
		if note := getString(data, "note"); note != "" {
			KeyValue("Note", note)
		}
		expiresAt := getString(data, "expires_at")
		if expiresAt != "" {
			KeyValue("Expires", formatExpiryRelative(expiresAt))
		}
		fmt.Println()
		styleDim.Println("  Waiting for human owner to approve or reject.")
		checkCmd := "botwallet approval status <approval_id>"
		if approvalID != "" {
			checkCmd = "botwallet approval status " + approvalID
		}
		styleDim.Printf("  Check again later with: %s\n", checkCmd)

	case "approved":
		SuccessMsg("%s approved by owner!", typeLabel)
		Section("Approval Details")
		KeyValue("ID", getString(data, "approval_id"))
		KeyValue("Type", typeLabel)
		KeyValueMoney("Amount", getFloat(data, "amount_usdc", "amount"))
		approvalRecipient(approvalType, getString(data, "recipient"))
		if resolvedAt := datePrefix(getString(data, "resolved_at"), 16); resolvedAt != "" {
			KeyValue("Approved at", resolvedAt)
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
		approvalRecipient(approvalType, getString(data, "recipient"))
		if reason := getString(data, "rejection_reason"); reason != "" {
			KeyValue("Reason", reason)
		}
		fmt.Println()
		styleDim.Println("  The owner declined this action. It cannot proceed.")

	case "expired":
		WarningMsg("%s approval expired", typeLabel)
		Section("Approval Details")
		KeyValue("ID", getString(data, "approval_id"))
		KeyValue("Type", typeLabel)
		KeyValue("Expired at", getString(data, "expires_at"))
		fmt.Println()
		styleDim.Println("  The approval window has closed. Re-initiate the action if still needed.")

	default:
		InfoMsg("Approval status: %s", status)
		KeyValue("ID", getString(data, "approval_id"))
		if actionable {
			if nextStep := getString(data, "next_step"); nextStep != "" {
				fmt.Println()
				InfoMsg("%s", nextStep)
			}
		}
	}
}

// approvalRecipient prints who an approval pays: an address (x402 APIs and
// withdrawals) shortened, a username with its @.
func approvalRecipient(approvalType, recipient string) {
	if recipient == "" {
		return
	}
	if approvalType == "x402_payment" || len(recipient) > 30 {
		KeyValue("Recipient", truncateAddress(recipient))
	} else {
		KeyValue("To", ensureAtPrefix(recipient))
	}
}
