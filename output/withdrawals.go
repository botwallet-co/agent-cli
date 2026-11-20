package output

import "fmt"

// =============================================================================
// Withdrawal Formatters
// =============================================================================

// WithdrawalFeeLines prints what a withdrawal costs (human mode only) and
// returns the total fee. Withdrawals are never free: the platform fee applies
// to every withdrawal, plus a one-time account setup fee when the destination
// address has never held USDC. totalKeys name the response field that holds
// the combined fee, which differs between endpoints. If the response carries
// no fee data at all, nothing is printed rather than a misleading "$0.00".
func WithdrawalFeeLines(data map[string]interface{}, totalKeys ...string) float64 {
	total := getFloat(data, totalKeys...)
	breakdown := getMap(data, "fee_breakdown")
	if breakdown == nil {
		if total > 0 {
			KeyValueMoney("Fees", total)
		}
		return total
	}
	platformFee := getFloat(breakdown, "platform_fee_usdc")
	setupFee := getFloat(breakdown, "account_setup_fee_usdc")
	if total == 0 {
		total = platformFee + setupFee
	}
	KeyValueMoney("Platform fee", platformFee)
	if setupFee > 0 {
		KeyValueMoney("Account setup (one-time, new address)", setupFee)
		KeyValueMoney("Total fees", total)
	}
	return total
}

// FormatWithdraw formats the initial withdrawal request response (Step 1).
// API returns: { status: 'awaiting_approval', withdrawal_id, approval_id, approval_url, amount, ... }
func FormatWithdraw(data map[string]interface{}) {
	if !humanOutput {
		JSON(withdrawalForAgent(data))
		return
	}

	withdrawalID := getString(data, "withdrawal_id")
	approvalID := getString(data, "approval_id")

	WarningMsg("Withdrawal requires owner approval")

	Section("Withdrawal Request")
	KeyValue("Withdrawal ID", withdrawalID)
	KeyValue("Approval ID", approvalID)
	amount := getFloat(data, "amount_usdc", "amount")
	KeyValueMoney("Amount", amount)
	if fees := WithdrawalFeeLines(data, "network_fee_usdc", "network_fee"); fees > 0 {
		KeyValueMoney("Total deducted", amount+fees)
	}
	KeyValueMoney("You'll Receive", getFloat(data, "you_receive_usdc", "you_receive"))
	KeyValue("To Address", getString(data, "to_address"))

	fmt.Println()
	InfoMsg("All withdrawals require owner approval.")

	approvalURL := getString(data, "approval_url")
	if approvalURL != "" {
		KeyValueURL("Approval URL", approvalURL)
	}

	Section("Next Steps")
	printApprovalSteps(approvalURL, approvalID, "botwallet withdraw confirm "+withdrawalID, "Withdrawal")
}

// WithdrawalConfirmSummary shows, before Key 1 signs, the withdrawal that
// withdraw confirm is about to send (human mode only).
func WithdrawalConfirmSummary(confirm map[string]interface{}) {
	confirmSummary("Confirm Withdrawal", confirm, true, [2]string{"To", truncateAddress(getString(confirm, "to_address"))})
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
	WithdrawalFeeLines(data, "fee_usdc", "fee")
	KeyValue("To Address", getString(data, "to_address"))

	if solanaSig := getString(data, "solana_signature"); solanaSig != "" {
		fmt.Println()
		printSolanaTx(solanaSig, getString(data, "explorer_url"), getString(data, "network"))
	}

	if newBalance := getFloat(data, "new_balance_usdc", "new_balance"); newBalance > 0 {
		fmt.Println()
		printNewBalanceEstimate(newBalance)
	}
}

// FormatWithdrawal formats one withdrawal's status (withdraw get).
func FormatWithdrawal(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	status := getString(data, "status")
	withdrawalID := getString(data, "withdrawal_id")

	Section("Withdrawal Status")
	KeyValue("Withdrawal ID", withdrawalID)
	KeyValue("Status", formatWithdrawStatus(status))
	KeyValueMoney("Amount", getFloat(data, "amount_usdc"))
	WithdrawalFeeLines(data, "network_fee_usdc")
	KeyValueMoney("You Received", getFloat(data, "you_receive_usdc"))
	KeyValue("To Address", getString(data, "to_address"))
	KeyValue("Created", getString(data, "created_at"))

	switch status {
	case "completed":
		Section("Blockchain Details")
		if completedAt, ok := data["completed_at"].(string); ok {
			KeyValue("Completed", completedAt)
		}
		if solanaTx, ok := data["solana_tx"].(string); ok {
			KeyValueHighlight("Solana TX", solanaTx)
			Tip("View on Solscan: https://solscan.io/tx/%s", solanaTx)
		}
	case "approved":
		fmt.Println()
		SuccessMsg("Owner approved! Ready to execute.")
		fmt.Println()
		Section("Next Step")
		InfoMsg("Run to execute:")
		fmt.Printf("  botwallet withdraw confirm %s\n", withdrawalID)
	case "awaiting_approval":
		fmt.Println()
		WarningMsg("Waiting for owner approval.")
		if approvalURL, ok := data["approval_url"].(string); ok {
			KeyValueURL("Approval URL", approvalURL)
		}
		Tip("After approval: botwallet withdraw confirm %s", withdrawalID)
	case "pending":
		InfoMsg("FROST signing in progress...")
	case "failed":
		ErrorMsg("Withdrawal failed.")
		if reason, ok := data["failure_reason"].(string); ok {
			KeyValue("Reason", reason)
		}
	case "denied":
		ErrorMsg("Withdrawal denied by owner.")
	case "expired":
		WarningMsg("Withdrawal expired. Create a new request.")
	}
}
