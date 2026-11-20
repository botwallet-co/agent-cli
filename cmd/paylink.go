package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/output"
)

var paylinkCmd = &cobra.Command{
	Use:   "paylink",
	Short: "Create payment links to get paid (earn money)",
	Long: `Create and manage payment links to receive money.

A paylink is a shareable payment URL - you create it, share the link,
and anyone (agents or humans) can pay you. This is how agents EARN money!

How it works:
  1. You create a paylink for $25 for "Research report"
  2. You get a payment URL to share
  3. Anyone with the link can pay
  4. You receive the funds when paid

Optional Features:
  • Itemized invoice: Add line items with --item (repeatable)
  • Owner privacy: Hide your email with --reveal-owner=false

Subcommands:
  create    Create a new payment link
  send      Send a paylink to an email or bot's inbox
  get       Check if a paylink has been paid
  list      List all your paylinks
  cancel    Cancel a pending paylink`,
	Example: `  # Simple paylink
  botwallet paylink create 25.00 --desc "Research report"
  
  # Send to another bot's inbox
  botwallet paylink send <request_id> --to @data-bot
  
  # Or email it to a human
  botwallet paylink send <request_id> --to client@example.com
  
  # With itemized invoice (total auto-calculated)
  botwallet paylink create --desc "Services" --item "API Calls, 5.00, 2" --item "Setup Fee, 10.00"
  
  # Check status
  botwallet paylink get pl_abc123`,
}

var (
	paylinkCreateDesc        string
	paylinkCreateReference   string
	paylinkCreateExpiresIn   string
	paylinkCreateRevealOwner bool
	paylinkCreateItems       []string
)

var paylinkCreateCmd = &cobra.Command{
	Use:   "create [amount]",
	Short: "Create a payment link to receive money",
	Long: `Create a payment link so others can pay you.

This is how agents EARN money! Create a paylink, share the URL,
and receive funds when someone pays.

The payment URL can be shared with:
  • Other agents (they pay via: botwallet pay --paylink <id>)
  • Humans (they pay via the web interface)

ITEMIZED INVOICE (optional --item flag, repeatable):
  Add --item for each line item. The total is calculated automatically.

  FORMAT:  --item "description, price[, quantity]"

  • Price: use numbers like 5.00 or 5 (no $ sign needed)
  • Quantity is optional (defaults to 1)
  • Descriptions with commas are fine

  When using --item, the amount argument is optional — the CLI
  calculates the total from your items.`,
	Example: `  # Simple paylink
  botwallet paylink create 10.00 --desc "Research report"

  # Itemized invoice (total auto-calculated: $20)
  botwallet paylink create --desc "Dev services" --item "API Calls, 5.00, 2" --item "Setup Fee, 10.00"

  # Email it as an invoice after creating
  botwallet paylink send <request_id> --to client@example.com --message "Invoice attached"

  # With reference ID and expiry
  botwallet paylink create 50.00 --desc "Consulting" --reference "INV-001" --expires "7d"`,
	Args: cobra.RangeArgs(0, 1),
	Run: func(cmd *cobra.Command, args []string) {
		client := authedClient()

		var amountCents int64
		hasAmount := len(args) > 0

		if hasAmount {
			var ok bool
			if amountCents, ok = parseAmountArg(args[0], "10.00"); !ok {
				return
			}
		}

		if paylinkCreateDesc == "" {
			exitValidationError("--desc flag is required", "Describe what you're charging for, e.g., --desc \"Research report\"")
			return
		}

		var lineItems []api.LineItem
		if len(paylinkCreateItems) > 0 {
			items, itemTotalCents, err := parseItems(paylinkCreateItems)
			if err != nil {
				exitValidationError("Invalid --item", err.Error())
				return
			}
			lineItems = items

			if hasAmount && amountCents != itemTotalCents {
				diff := amountCents - itemTotalCents
				exitValidationError(
					"Amount doesn't match item total",
					fmt.Sprintf("Amount:     $%.2f\nItem total: $%.2f\nDifference: $%.2f\n\nEither remove the amount argument (let items set the total)\nor fix your --item values to match.",
						centsToDollars(amountCents), centsToDollars(itemTotalCents), centsToDollars(diff)),
				)
				return
			}

			amountCents = itemTotalCents
			output.PaylinkItemsTotal(len(lineItems), centsToDollars(amountCents))
		} else if !hasAmount {
			exitValidationError(
				"Amount is required",
				"Provide an amount or use --item flags:\n  botwallet paylink create 10.00 --desc \"Service\"\n  botwallet paylink create --desc \"Service\" --item \"Item, 10.00\"",
			)
			return
		}

		result, err := client.CreatePaymentRequest(api.PaymentRequest{
			Amount:      centsToDollars(amountCents),
			Description: paylinkCreateDesc,
			Reference:   paylinkCreateReference,
			ExpiresIn:   paylinkCreateExpiresIn,
			RevealOwner: paylinkCreateRevealOwner,
			LineItems:   lineItems,
		})
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatCreatePaymentRequest(result)
	},
}

func init() {
	paylinkCreateCmd.Flags().StringVar(&paylinkCreateDesc, "desc", "", "Description/note shown on payment page (required)")
	paylinkCreateCmd.Flags().StringVar(&paylinkCreateReference, "reference", "", "Your internal reference ID")
	paylinkCreateCmd.Flags().StringVar(&paylinkCreateExpiresIn, "expires", "24h", "Expiration time: 1h, 24h, 7d")
	paylinkCreateCmd.Flags().BoolVar(&paylinkCreateRevealOwner, "reveal-owner", true, "Show owner email on payment page")
	// The flag's first spelling, still accepted so existing scripts keep working
	paylinkCreateCmd.Flags().BoolVar(&paylinkCreateRevealOwner, "revealOwner", true, "Same as --reveal-owner")
	_ = paylinkCreateCmd.Flags().MarkHidden("revealOwner")
	paylinkCreateCmd.Flags().StringArrayVar(&paylinkCreateItems, "item", nil, "Line item: \"description, price[, qty]\" (repeatable)")
}

var paylinkGetReference string

var paylinkGetCmd = &cobra.Command{
	Use:   "get <paylink-id>",
	Short: "Check if a paylink has been paid",
	Long: `Check the status of a payment link.

Shows whether the paylink is pending, completed (paid), expired, or
cancelled. If it was paid, shows who paid and the transaction details.`,
	Example: `  botwallet paylink get pl_abc123def456
  botwallet paylink get --reference my-ref-123`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client := authedClient()

		var paylinkID string
		if len(args) > 0 {
			paylinkID = args[0]
		}

		if paylinkID == "" && paylinkGetReference == "" {
			exitValidationError("Provide a paylink ID or --reference", "Usage: botwallet paylink get <paylink-id> OR botwallet paylink get --reference <ref>")
			return
		}

		result, err := client.GetPaymentRequest(paylinkID, paylinkGetReference)
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatPaylink(result)
	},
}

func init() {
	paylinkGetCmd.Flags().StringVar(&paylinkGetReference, "reference", "", "Look up by your reference ID instead")
}

var (
	paylinkListStatus string
	paylinkListLimit  int
	paylinkListOffset int
)

var paylinkListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your payment links",
	Long: `List all your payment links.

Without --status it lists them all. Filter with --status pending, completed
(paid), expired or cancelled.
Use this to track your earnings and outstanding payment links.`,
	Example: `  botwallet paylink list
  botwallet paylink list --status pending
  botwallet paylink list --status completed --limit 10`,
	Run: func(cmd *cobra.Command, args []string) {
		client := authedClient()

		result, err := client.ListPaymentRequests(paylinkListStatus, paylinkListLimit, paylinkListOffset)
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatPaylinks(result)
	},
}

func init() {
	paylinkListCmd.Flags().StringVar(&paylinkListStatus, "status", "", "Filter by status: pending, completed, expired, cancelled")
	paylinkListCmd.Flags().IntVar(&paylinkListLimit, "limit", 20, "Maximum results to return")
	paylinkListCmd.Flags().IntVar(&paylinkListOffset, "offset", 0, "Offset for pagination")
}

var paylinkCancelCmd = &cobra.Command{
	Use:   "cancel <paylink-id>",
	Short: "Cancel a pending payment link",
	Long: `Cancel a pending payment link.

Only pending paylinks can be cancelled. Paid or expired paylinks
cannot be cancelled.`,
	Example: "  botwallet paylink cancel pl_abc123def456",
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client := authedClient()

		paylinkID := args[0]

		result, err := client.CancelPaymentRequest(paylinkID)
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatPaylinkCancelled(result)
	},
}

var (
	paylinkSendTo      string
	paylinkSendMessage string
)

var paylinkSendCmd = &cobra.Command{
	Use:   "send <paylink-id>",
	Short: "Send a paylink to an email or bot",
	Long: `Send a paylink to a recipient as a payment request.

The --to flag accepts two types of recipients:
  • Email address  → sends a branded email with a "Pay Now" button
  • @bot-username  → delivers to the bot's event inbox

Use this after creating a paylink to request payment from a specific person or bot.`,
	Example: `  # Send to a human via email
  botwallet paylink send <request_id> --to client@example.com

  # Send to another bot's inbox
  botwallet paylink send <request_id> --to @data-bot

  # With a personal message
  botwallet paylink send <request_id> --to @data-bot --message "Payment for research data"`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client := authedClient()

		paylinkID := args[0]

		if paylinkSendTo == "" {
			exitValidationError("--to flag is required", "Provide a recipient: --to client@example.com OR --to @bot-username")
			return
		}

		var toEmail, toWallet string
		if strings.HasPrefix(paylinkSendTo, "@") {
			toWallet = paylinkSendTo
		} else if strings.Contains(paylinkSendTo, "@") {
			toEmail = paylinkSendTo
		} else {
			exitValidationError("Invalid --to value", "Use an email (user@example.com) or bot username (@bot-name)")
			return
		}

		result, err := client.SendPaylinkInvitation(paylinkID, toEmail, toWallet, paylinkSendMessage)
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatSendPaylinkInvitation(paylinkID, result)
	},
}

func init() {
	paylinkSendCmd.Flags().StringVar(&paylinkSendTo, "to", "", "Recipient: email address or @bot-username (required)")
	paylinkSendCmd.Flags().StringVar(&paylinkSendMessage, "message", "", "Optional personal message included with the request")
}

func init() {
	paylinkCmd.AddCommand(paylinkCreateCmd)
	paylinkCmd.AddCommand(paylinkSendCmd)
	paylinkCmd.AddCommand(paylinkGetCmd)
	paylinkCmd.AddCommand(paylinkListCmd)
	paylinkCmd.AddCommand(paylinkCancelCmd)
}
