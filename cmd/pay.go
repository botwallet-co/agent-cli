package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/output"
)

var (
	payNote           string
	payReference      string
	payPaylinkID      string
	payIdempotencyKey string
)

var payCmd = &cobra.Command{
	Use:   "pay [recipient] [amount]",
	Short: "Send a payment (two-step flow)",
	Long: `Create a payment intent. This does NOT send funds immediately.

STEP 1: botwallet pay @recipient 10.00
        Creates a payment intent, returns transaction_id

STEP 2: botwallet pay confirm <transaction_id>
        FROST threshold signs the transaction and submits to Solana

Subcommands:
  confirm   FROST sign and submit a payment (Step 2)
  preview   Check if you can afford a payment before creating it
  list      List your pending and completed payments
  cancel    Cancel a pending payment

You can pay:
- A merchant by username: botwallet pay @botverse 10.00
- Another agent by username: botwallet pay @claude-research 5.00
- A Solana address: botwallet pay 7xK...abc 5.00
- A payment link: botwallet pay --paylink pl_abc123`,
	Example: `  # Two-step payment flow:
  botwallet pay @merchant 10.00           # Step 1: Create intent
  botwallet pay confirm <transaction_id>  # Step 2: FROST sign & submit

  # With options:
  botwallet pay @merchant 10.00 --note "API payment"
  botwallet pay --paylink pl_abc123

  # Manage payments:
  botwallet pay list
  botwallet pay preview @merchant 10.00
  botwallet pay cancel <transaction_id>`,
	Args: cobra.RangeArgs(0, 2),
	Run:  runPayCreate,
}

func runPayCreate(cmd *cobra.Command, args []string) {
	if !requireAPIKey() {
		return
	}

	client := getClient()
	var result map[string]interface{}
	var err error

	// Pay by paylink ID
	if payPaylinkID != "" {
		result, err = client.PayRequest(payPaylinkID, payIdempotencyKey)
	} else {
		// Pay by recipient + amount
		if len(args) < 2 {
			output.ValidationError("Missing required arguments", "Usage: botwallet pay <recipient> <amount> OR botwallet pay --paylink <id>")
			return
		}

		amountArg := args[1]

		// Strip @ prefix if user typed @username (allows both @user and user)
		to := stripAtPrefix(args[0])
		amountCents, ok := parseAmountArg(amountArg, "10.00")
		if !ok {
			return
		}

		// The server pays anything shaped like an address as an address, so
		// one that does not decode to 32 bytes must stop here.
		if looksLikeSolanaAddress(to) && !isValidSolanaAddress(to) {
			output.ValidationError("Invalid Solana address format", invalidAddressHowToFix)
			return
		}

		result, err = client.Pay(to, centsToDollars(amountCents), payNote, payReference, payIdempotencyKey)
	}

	if err != nil {
		handleAPIError(err)
		return
	}

	// Format based on status
	output.FormatPayInitiated(result)
}

func init() {
	payCmd.Flags().StringVar(&payNote, "note", "", "Note visible to recipient")
	payCmd.Flags().StringVar(&payNote, "memo", "", "Note visible to recipient (alias for --note)")
	payCmd.Flags().StringVar(&payReference, "reference", "", "Your internal reference ID")
	payCmd.Flags().StringVar(&payPaylinkID, "paylink", "", "Pay a payment link by ID")
	payCmd.Flags().StringVar(&payIdempotencyKey, "idempotency-key", "", "Idempotency key to prevent duplicate payments")
	payCmd.Flags().MarkHidden("memo")

	// Intercept Cobra's flag parse errors to give a clear message when a
	// negative number (e.g. -5.00) is mistakenly interpreted as a flag.
	// SetFlagErrorFunc is Cobra's designated API for this — it only fires
	// for the specific error, unlike FParseErrWhitelist which would suppress
	// ALL unknown flags.
	payCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		msg := err.Error()
		// Detect pattern: "unknown shorthand flag: 'X' in -VALUE"
		if strings.Contains(msg, "unknown shorthand flag") {
			if idx := strings.Index(msg, " in "); idx >= 0 {
				value := strings.TrimSpace(msg[idx+4:])
				if _, parseErr := strconv.ParseFloat(value, 64); parseErr == nil {
					// PersistentPreRun hasn't run yet because flag parsing
					// failed before --human could be parsed. Scan os.Args
					// directly to detect if the user passed --human.
					for _, arg := range os.Args {
						if arg == "--human" {
							output.SetHumanOutput(true)
							break
						}
					}
					output.ValidationError(
						"Amount must be a positive number",
						"Got '"+value+"'. Use a positive number, e.g., botwallet pay @recipient 5.00",
					)
					return nil // ValidationError calls os.Exit(1), never reaches here
				}
			}
		}
		return err // All other flag errors pass through normally
	})
}

var payConfirmCmd = &cobra.Command{
	Use:   "confirm <transaction_id>",
	Short: "Execute a payment (Step 2 of 2)",
	Long: `Sign and submit a previously initiated payment.

This command:
1. Retrieves the transaction from the server
2. Shows the transaction details (amount, recipient, fee)
3. Performs FROST threshold signing (agent + server cooperate)
4. Server submits to Solana blockchain

Your key share (S1) is used locally to produce a partial signature.
The server uses its share (S2) to produce the other half.
Neither party can sign alone — both must cooperate.

Only works for transactions with status:
- PRE-APPROVED (guard rails passed)
- APPROVED (human owner approved)

Transactions expire after 48 hours.`,
	Example: `  botwallet pay confirm abc12345-6789-...
  
  # Full flow:
  botwallet pay @merchant 10.00   # Step 1: Initiate
  botwallet pay confirm <id>      # Step 2: Execute`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		transactionId := args[0]
		target := paymentTarget(transactionId)

		// Load Key 1 first: if it is missing or belongs to another wallet,
		// stop before the server starts processing the payment.
		signer, err := loadSigningWallet()
		if err != nil {
			exitSigningError(err, target)
			return
		}

		client := getClient()

		if output.IsHumanOutput() {
			output.InfoMsg("Retrieving transaction %s...", transactionId)
		}

		confirmResult, err := client.ConfirmPayment(transactionId)
		if err != nil {
			exitConfirmError(err, target)
			return
		}

		messageB64, _ := confirmResult["message"].(string)
		if messageB64 == "" {
			output.APIError("SIGNING_ERROR", "Server did not return a message to sign",
				"The transaction may have expired. Check with 'botwallet pay list'", nil)
			return
		}

		if output.IsHumanOutput() {
			toDisplay, _ := confirmResult["to"].(string)
			if toDisplay == "" {
				if addr, ok := confirmResult["to_address"].(string); ok && len(addr) > 8 {
					toDisplay = addr[:8] + "..."
				}
			}
			amountUSDC, _ := confirmResult["amount_usdc"].(float64)
			feeUSDC, _ := confirmResult["fee_usdc"].(float64)
			totalUSDC, _ := confirmResult["total_usdc"].(float64)
			network, _ := confirmResult["network"].(string)

			feeLine := fmt.Sprintf("$%.2f USDC", feeUSDC)
			if bd, ok := confirmResult["fee_breakdown"].(map[string]interface{}); ok {
				if sf, ok := bd["account_setup_fee_usdc"].(float64); ok && sf > 0 {
					feeLine += fmt.Sprintf(" (incl. $%.2f account setup)", sf)
				}
			}
			summary := fmt.Sprintf("To:     @%s\nAmount: $%.2f USDC\nFee:    %s\nTotal:  $%.2f USDC", toDisplay, amountUSDC, feeLine, totalUSDC)
			if network != "" {
				summary += fmt.Sprintf("\nNetwork: %s", network)
			}
			output.Box("Confirm Payment", summary)
			fmt.Println()
		}

		intent, err := paymentIntent(confirmResult)
		if err != nil {
			exitSigningError(err, target)
			return
		}

		submitResult, err := frostSignAndSubmit(client, transactionId, messageB64, signer, intent)
		if err != nil {
			exitSigningError(err, target)
			return
		}

		output.FormatPaySuccess(submitResult)
	},
}

var payPreviewCmd = &cobra.Command{
	Use:   "preview <recipient> <amount>",
	Short: "Check if you can afford a payment",
	Long: `Pre-flight check to see if a payment would succeed.

Checks:
- Recipient exists and is active
- Sufficient balance (including fees)
- Within spending limits
- No guard rails blocking the payment

This helps you avoid failed payment attempts and understand
exactly what will happen before committing to a payment.`,
	Example: `  botwallet pay preview @botverse 10.00
  botwallet pay preview @claude-research 5.50`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		// Strip @ prefix if user typed @username (allows both @user and user)
		to := stripAtPrefix(args[0])
		amountCents, ok := parseAmountArg(args[1], "10.00")
		if !ok {
			return
		}

		client := getClient()

		result, err := client.CanIAfford(to, centsToDollars(amountCents))
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatCanIAfford(result)
	},
}

var (
	payListStatus string
	payListID     string
	payListLimit  int
	payListOffset int
)

var payListCmd = &cobra.Command{
	Use:   "list",
	Short: "List payment transactions",
	Long: `List your payment transactions and their status.

By default, shows actionable payments (things you need to act on):
- PRE-APPROVED: Ready to confirm
- AWAITING APPROVAL: Waiting for owner
- APPROVED: Owner approved, ready to confirm
- PENDING: Being processed on-chain

Use --status to filter:
- actionable (default): Transactions you can act on
- all: All transactions
- pre_approved, awaiting_approval, approved: One of the actionable states
- completed: Successfully executed
- failed: Failed transactions
- rejected: Blocked by guard rails or the owner
- expired: Expired or cancelled transactions
- pending: Alias for actionable`,
	Example: `  botwallet pay list                        # Actionable transactions
  botwallet pay list --status all           # All history
  botwallet pay list --status completed     # Completed only
  botwallet pay list --id <transaction_id>  # Specific transaction`,
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		status, ok := payStatusFilter(payListStatus)
		if !ok {
			output.ValidationError("Unknown status: "+payListStatus,
				"Use one of: actionable, all, pre_approved, awaiting_approval, approved, completed, failed, rejected, expired")
			return
		}

		client := getClient()

		result, err := client.ListPayments(payListID, status, payListLimit, payListOffset)
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatPaymentsList(result)
	},
}

func init() {
	payListCmd.Flags().StringVar(&payListStatus, "status", "actionable", "Filter: actionable (default), all, pre_approved, awaiting_approval, approved, completed, failed, rejected, expired")
	payListCmd.Flags().StringVar(&payListID, "id", "", "Get specific transaction by ID")
	payListCmd.Flags().IntVar(&payListLimit, "limit", 20, "Maximum number of results")
	payListCmd.Flags().IntVar(&payListOffset, "offset", 0, "Offset for pagination")
}

// payStatusFilter checks a --status value for pay list. The server takes
// actionable (pending is its alias), all, or one payment status, and
// answers an unknown status with an internal error. Cancelling a payment
// marks it expired, so cancelled lists expired payments.
func payStatusFilter(status string) (string, bool) {
	switch s := strings.ToLower(strings.TrimSpace(status)); s {
	case "", "actionable", "pending", "all",
		"pre_approved", "awaiting_approval", "approved",
		"completed", "failed", "rejected", "expired", "reversed":
		return s, true
	case "cancelled", "canceled":
		return "expired", true
	default:
		return "", false
	}
}

var payCancelCmd = &cobra.Command{
	Use:   "cancel <transaction_id>",
	Short: "Cancel a pending payment",
	Long: `Cancel a payment that hasn't been executed yet.

Only works for transactions with status:
- PRE-APPROVED (ready but not yet confirmed)
- AWAITING APPROVAL (waiting for owner)
- APPROVED (owner approved but not yet confirmed)

Completed, failed, or expired transactions cannot be cancelled.`,
	Example: `  botwallet pay cancel abc12345-6789-...
  
  # Check what's pending first:
  botwallet pay list`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		transactionId := args[0]
		client := getClient()

		result, err := client.CancelPayment(transactionId)
		if err != nil {
			handleAPIError(err)
			return
		}

		if !output.IsHumanOutput() {
			output.JSON(result)
			return
		}

		output.SuccessMsg("Payment cancelled!")
		if id, ok := result["transaction_id"].(string); ok {
			output.KeyValue("Transaction ID", id)
		}
		if prev, ok := result["previous_status"].(string); ok {
			output.KeyValue("Was", prev)
		}
		amount, _ := result["amount_usdc"].(float64)
		if amount == 0 {
			amount, _ = result["amount"].(float64)
		}
		if amount > 0 {
			output.KeyValueMoney("Amount", amount)
		}
	},
}

func init() {
	payCmd.AddCommand(payConfirmCmd)
	payCmd.AddCommand(payPreviewCmd)
	payCmd.AddCommand(payListCmd)
	payCmd.AddCommand(payCancelCmd)
}
