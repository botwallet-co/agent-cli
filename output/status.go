package output

import (
	"fmt"
	"strings"
	"time"
)

// =============================================================================
// Status Labels, Expiry Times and Next Steps
// =============================================================================

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

// formatWithdrawStatus is formatStatus for a withdrawal: pending means it
// is being signed, and the owner can deny it. Other statuses are shown as
// the API wrote them.
func formatWithdrawStatus(status string) string {
	switch status {
	case "pending":
		return "SIGNING..."
	case "denied":
		return "DENIED ✗"
	case "awaiting_approval", "approved", "completed", "failed", "expired":
		return formatStatus(status)
	default:
		return status
	}
}

// formatClaimExpiry shows a claim-code expiry as an absolute UTC time plus the
// time left, e.g. "2026-09-29 14:03 UTC (in 47h)". Values that can't be parsed
// are returned unchanged.
func formatClaimExpiry(expiresAt string) string {
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return expiresAt
	}
	when := t.UTC().Format("2006-01-02 15:04 UTC")
	left := t.Sub(now())
	switch {
	case left <= 0:
		return when + " (expired)"
	case left < time.Hour:
		return fmt.Sprintf("%s (in %dm)", when, int(left.Minutes()))
	default:
		return fmt.Sprintf("%s (in %dh)", when, int(left.Hours()))
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

	current := now()
	if t.Before(current) {
		return "expired"
	}

	diff := t.Sub(current)
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

// initiatedHeader prints the first line for a payment intent (pay, x402
// fetch) in status: started says what happened ("Payment initiated"),
// blocked what the guard rail stopped ("Payment").
func initiatedHeader(status, started, blocked string) {
	switch status {
	case "pre_approved":
		SuccessMsg("%s - PRE-APPROVED ✓", started)
	case "awaiting_approval":
		WarningMsg("%s - AWAITING APPROVAL", started)
	case "rejected":
		ErrorMsg("%s BLOCKED by guard rail", blocked)
	default:
		InfoMsg("%s (status: %s)", started, status)
	}
}

// printApprovalSteps prints what to do while the owner has to approve:
// ask the human, check the approval, then run confirmCommand. what names
// the action ("Payment", "Withdrawal"), which never runs by itself.
func printApprovalSteps(approvalURL, approvalID, confirmCommand, what string) {
	fmt.Println()
	styleBold.Println("  Three steps required:")
	fmt.Println()
	fmt.Print("  1. ")
	styleWarning.Print("Ask your human")
	fmt.Print(" to approve at:\n")
	if approvalURL != "" {
		fmt.Printf("     %s\n", approvalURL)
	}
	fmt.Println()
	fmt.Print("  2. ")
	styleWarning.Print("Check status")
	fmt.Print(" with:\n")
	if approvalID != "" {
		styleHighlight.Printf("     botwallet approval status %s\n", approvalID)
	}
	fmt.Println()
	fmt.Print("  3. ")
	styleWarning.Print("After approved")
	fmt.Print(", run:\n")
	styleHighlight.Printf("     %s\n", confirmCommand)
	fmt.Println()
	styleDim.Printf("  ❌ %s does NOT auto-execute after approval!\n", what)
	Tip("Save the approval_id to memory. Check periodically until the human approves, then run the confirm command.")
}

// printSolanaTx prints the Solana transaction section: the signature and,
// when known, the explorer link and network.
func printSolanaTx(signature, explorerURL, network string) {
	Section("Solana Transaction")
	KeyValue("Signature", truncateAddress(signature))
	if explorerURL != "" {
		KeyValueURL("View on Explorer", explorerURL)
	}
	if network != "" {
		KeyValue("Network", network)
	}
}

// printNewBalanceEstimate prints the balance after a payment, which is the
// server's estimate until the transaction settles.
func printNewBalanceEstimate(balance float64) {
	styleLabel.Print("  New Balance: ")
	styleMoney.Printf("$%.2f", balance)
	styleDim.Println(" (estimate)")
}

// printLowBalance warns that the balance is low, with where to add funds
// if fundingURL is known.
func printLowBalance(message, fundingURL string) {
	fmt.Println()
	WarningMsg("%s", message)
	if fundingURL != "" {
		KeyValueURL("Add funds at", fundingURL)
	}
}
