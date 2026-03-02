package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/output"
)

var withdrawIdempotencyKey string
var withdrawReason string

var withdrawCmd = &cobra.Command{
	Use:   "withdraw [amount] [address]",
	Short: "Withdraw USDC to a Solana address (two-step flow)",
	Long: `Withdraw USDC from your wallet to an external Solana address.

STEP 1: botwallet withdraw 50.00 <address> --reason "..."
        Creates a withdrawal request, returns withdrawal_id
        All withdrawals require owner approval.

STEP 2: After your owner approves, run:
        botwallet withdraw confirm <withdrawal_id>
        Signs the transaction locally (FROST) and submits to Solana.

Subcommands:
  confirm   Sign and submit an approved withdrawal (Step 2)
  get       Check the status of a withdrawal`,
	Example: `  # Two-step withdrawal flow:
  botwallet withdraw 50.00 7xKXtR9... --reason "Pay hosting"  # Step 1
  botwallet withdraw confirm <withdrawal_id>                   # Step 2

  # Check status:
  botwallet withdraw get <withdrawal_id>`,
	Args: cobra.RangeArgs(0, 2),
	Run:  runWithdrawDefault,
}

func runWithdrawDefault(cmd *cobra.Command, args []string) {
	if len(args) < 2 {
		cmd.Help()
		return
	}

	if !requireAPIKey() {
		return
	}

	amount, err := strconv.ParseFloat(args[0], 64)
	if err != nil {
		output.ValidationError("Invalid amount: "+args[0], "Amount should be a number, e.g., 50.00")
		return
	}

	if amount <= 0 {
		output.ValidationError("Amount must be greater than 0", "Provide a positive number")
		return
	}

	toAddress := args[1]

	if !isValidSolanaAddress(toAddress) {
		output.ValidationError("Invalid Solana address format", "Solana addresses are 32-44 characters of base58 (letters/digits, no 0/O/I/l)")
		return
	}

	if strings.TrimSpace(withdrawReason) == "" {
		output.ValidationError("--reason is required for withdrawals", "Example: botwallet withdraw 50.00 <address> --reason \"Pay hosting\"")
		return
	}

	client := getClient()

	result, err := client.Withdraw(amount, toAddress, withdrawReason, withdrawIdempotencyKey)
	if err != nil {
		handleAPIError(err)
		return
	}

	output.FormatWithdraw(result)
}

func init() {
	withdrawCmd.Flags().StringVar(&withdrawIdempotencyKey, "idempotency-key", "", "Idempotency key to prevent duplicate withdrawals")
	withdrawCmd.Flags().StringVar(&withdrawReason, "reason", "", "Reason for the withdrawal (required)")

	withdrawCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		msg := err.Error()
		if strings.Contains(msg, "unknown shorthand flag") {
			if idx := strings.Index(msg, " in "); idx >= 0 {
				value := strings.TrimSpace(msg[idx+4:])
				if _, parseErr := strconv.ParseFloat(value, 64); parseErr == nil {
					for _, arg := range os.Args {
						if arg == "--human" {
							output.SetHumanOutput(true)
							break
						}
					}
					output.ValidationError(
						"Amount must be a positive number",
						"Got '"+value+"'. Use a positive number, e.g., botwallet withdraw 50.00 <address> --reason \"...\"",
					)
					return nil
				}
			}
		}
		return err
	})
}

var withdrawConfirmCmd = &cobra.Command{
	Use:   "confirm <withdrawal_id>",
	Short: "Execute an approved withdrawal (Step 2 of 2)",
	Long: `Sign and submit a previously approved withdrawal.

This command:
1. Retrieves the approved withdrawal from the server
2. Builds the Solana withdrawal transaction
3. Performs FROST threshold signing (agent + server cooperate)
4. Server submits to Solana blockchain

Only works for withdrawals with status APPROVED (owner has approved).
Withdrawals expire after 48 hours.`,
	Example: `  botwallet withdraw confirm abc12345-6789-...

  # Full flow:
  botwallet withdraw 50.00 <address> --reason "..."  # Step 1
  # ... owner approves on dashboard ...
  botwallet withdraw confirm <withdrawal_id>          # Step 2`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		withdrawalID := args[0]
		client := getClient()

		if output.IsHumanOutput() {
			output.InfoMsg("Retrieving withdrawal %s...", withdrawalID)
		}

		confirmResult, err := client.ConfirmWithdrawal(withdrawalID)
		if err != nil {
			handleAPIError(err)
			return
		}

		messageB64, _ := confirmResult["message"].(string)
		if messageB64 == "" {
			output.APIError("SIGNING_ERROR", "Server did not return a message to sign",
				"The withdrawal may have expired or not yet been approved. Check with 'botwallet withdraw get'", nil)
			return
		}

		if output.IsHumanOutput() {
			toAddr, _ := confirmResult["to_address"].(string)
			amount, _ := confirmResult["amount_usdc"].(float64)
			fee, _ := confirmResult["fee_usdc"].(float64)
			total, _ := confirmResult["total_usdc"].(float64)
			network, _ := confirmResult["network"].(string)

			addrDisplay := toAddr
			if len(addrDisplay) > 16 {
				addrDisplay = addrDisplay[:8] + "..." + addrDisplay[len(addrDisplay)-8:]
			}

			summary := fmt.Sprintf("To:     %s\nAmount: $%.2f USDC\nFee:    $%.2f USDC\nTotal:  $%.2f USDC", addrDisplay, amount, fee, total)
			if network != "" {
				summary += fmt.Sprintf("\nNetwork: %s", network)
			}
			output.Box("Confirm Withdrawal", summary)
			fmt.Println()
		}

		submitResult, err := frostSignAndSubmit(client, withdrawalID, messageB64, walletFlag)
		if err != nil {
			output.APIError("SIGNING_ERROR", err.Error(),
				"Check your wallet configuration and try again", nil)
			return
		}

		output.FormatWithdrawSuccess(submitResult)
	},
}

var withdrawGetCmd = &cobra.Command{
	Use:   "get <withdrawal-id>",
	Short: "Check status of a withdrawal",
	Long: `Check the status of a withdrawal.

Shows whether the withdrawal is:
- awaiting_approval: Waiting for owner approval
- approved: Owner approved — ready to confirm
- pending: FROST signing in progress
- completed: Successfully sent (includes Solana transaction ID)
- failed: Something went wrong`,
	Example: `  botwallet withdraw get <withdrawal_id>`,
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		withdrawalID := args[0]
		client := getClient()

		result, err := client.GetWithdrawal(withdrawalID)
		if err != nil {
			handleAPIError(err)
			return
		}

		if !output.IsHumanOutput() {
			output.JSON(result)
			return
		}

		status, _ := result["status"].(string)
		wID, _ := result["withdrawal_id"].(string)
		amount, _ := result["amount_usdc"].(float64)
		networkFee, _ := result["network_fee_usdc"].(float64)
		youReceived, _ := result["you_receive_usdc"].(float64)
		toAddress, _ := result["to_address"].(string)
		createdAt, _ := result["created_at"].(string)

		output.Section("Withdrawal Status")
		output.KeyValue("Withdrawal ID", wID)
		output.KeyValue("Status", formatWithdrawStatus(status))
		output.KeyValueMoney("Amount", amount)
		output.KeyValueMoney("Network Fee", networkFee)
		output.KeyValueMoney("You Received", youReceived)
		output.KeyValue("To Address", toAddress)
		output.KeyValue("Created", createdAt)

		switch status {
		case "completed":
			output.Section("Blockchain Details")
			if completedAt, ok := result["completed_at"].(string); ok {
				output.KeyValue("Completed", completedAt)
			}
			if solanaTx, ok := result["solana_tx"].(string); ok {
				output.KeyValueHighlight("Solana TX", solanaTx)
				output.Tip("View on Solscan: https://solscan.io/tx/%s", solanaTx)
			}
		case "approved":
			fmt.Println()
			output.SuccessMsg("Owner approved! Ready to execute.")
			fmt.Println()
			output.Section("Next Step")
			output.InfoMsg("Run to execute:")
			fmt.Printf("  botwallet withdraw confirm %s\n", wID)
		case "awaiting_approval":
			fmt.Println()
			output.WarningMsg("Waiting for owner approval.")
			if approvalURL, ok := result["approval_url"].(string); ok {
				output.KeyValueURL("Approval URL", approvalURL)
			}
			output.Tip("After approval: botwallet withdraw confirm %s", wID)
		case "pending":
			output.InfoMsg("FROST signing in progress...")
		case "failed":
			output.ErrorMsg("Withdrawal failed.")
			if reason, ok := result["failure_reason"].(string); ok {
				output.KeyValue("Reason", reason)
			}
		case "denied":
			output.ErrorMsg("Withdrawal denied by owner.")
		case "expired":
			output.WarningMsg("Withdrawal expired. Create a new request.")
		}
	},
}

// isValidSolanaAddress checks length and base58 character set.
func isValidSolanaAddress(addr string) bool {
	if len(addr) < 32 || len(addr) > 44 {
		return false
	}
	const base58Chars = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	for _, c := range addr {
		if !strings.ContainsRune(base58Chars, c) {
			return false
		}
	}
	return true
}

func formatWithdrawStatus(status string) string {
	switch status {
	case "awaiting_approval":
		return "AWAITING APPROVAL"
	case "approved":
		return "APPROVED ✓"
	case "pending":
		return "SIGNING..."
	case "completed":
		return "COMPLETED ✓"
	case "failed":
		return "FAILED ✗"
	case "denied":
		return "DENIED ✗"
	case "expired":
		return "EXPIRED"
	default:
		return status
	}
}

func init() {
	withdrawCmd.AddCommand(withdrawConfirmCmd)
	withdrawCmd.AddCommand(withdrawGetCmd)
}
