package cmd

import (
	"fmt"
	"strconv"
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
  • Invoice breakdown: Add itemized line items with --breakdown
  • Owner privacy: Hide your email with --revealOwner=false

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
  
  # With invoice breakdown
  botwallet paylink create 20.00 --desc "Services" --breakdown '2x API @ $5.00
  1x Setup @ $10.00'
  
  # Check status
  botwallet paylink get pl_abc123`,
}

var (
	paylinkCreateDesc        string
	paylinkCreateReference   string
	paylinkCreateExpiresIn   string
	paylinkCreateRevealOwner bool
	paylinkCreateBreakdown   string
)

var paylinkCreateCmd = &cobra.Command{
	Use:   "create <amount>",
	Short: "Create a payment link to receive money",
	Long: `Create a payment link so others can pay you.

This is how agents EARN money! Create a paylink, share the URL,
and receive funds when someone pays.

The payment URL can be shared with:
  • Other agents (they pay via: botwallet pay --paylink <id>)
  • Humans (they pay via the web interface)

INVOICE BREAKDOWN (optional --breakdown flag):
  Add --breakdown to include an itemized invoice. Each line is one item.
  Items must add up to the total amount.

  FORMAT (pick one per line):
    "2x Item Name @ $5.00"      ← quantity × unit price
    "Item Name - $10.00"        ← flat price, quantity = 1

  RULES:
    • Wrap the whole breakdown in single quotes
    • One item per line (newline-separated)
    • Prices use $ and decimals: $5.00, $10.00
    • Total of all items must equal the <amount> argument`,
	Example: `  # Simple paylink (no breakdown)
  botwallet paylink create 10.00 --desc "Research report"

  # Invoice with itemized breakdown ($10 + $10 = $20)
  botwallet paylink create 20.00 --desc "Dev services" --breakdown '2x API calls @ $5.00
  1x Setup fee - $10.00'

  # Email it as an invoice after creating
  botwallet paylink send <request_id> --to client@example.com --message "Invoice attached"

  # With reference ID and expiry
  botwallet paylink create 50.00 --desc "Consulting" --reference "INV-001" --expires "7d"`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		amount, err := strconv.ParseFloat(args[0], 64)
		if err != nil {
			output.ValidationError("Invalid amount: "+args[0], "Amount should be a number, e.g., 10.00")
			return
		}

		if amount <= 0 {
			output.ValidationError("Amount must be greater than 0", "Provide a positive number")
			return
		}

		if paylinkCreateDesc == "" {
			output.ValidationError("--desc flag is required", "Describe what you're charging for, e.g., --desc \"Research report\"")
			return
		}

		// Parse and validate breakdown if provided
		var lineItems []api.LineItem
		if paylinkCreateBreakdown != "" {
			items, breakdownTotal, err := ParseBreakdown(paylinkCreateBreakdown)
			if err != nil {
				output.ValidationError("Invalid breakdown format", err.Error())
				fmt.Println("\n" + FormatBreakdownExamples())
				return
			}

			// Convert to api.LineItem type
			for _, item := range items {
				lineItems = append(lineItems, api.LineItem{
					Description:    item.Description,
					Quantity:       item.Quantity,
					UnitPriceCents: item.UnitPriceCents,
					TotalCents:     item.TotalCents,
				})
			}

			// Validate breakdown total matches payment amount
			if !floatsEqual(breakdownTotal, amount) {
				diff := amount - breakdownTotal
				var advice string
				if diff > 0 {
					advice = fmt.Sprintf("Add $%.2f more to your breakdown, or reduce payment to $%.2f", diff, breakdownTotal)
				} else {
					advice = fmt.Sprintf("Remove $%.2f from your breakdown, or increase payment to $%.2f", -diff, breakdownTotal)
				}
				output.ValidationError(
					"Breakdown total doesn't match payment amount",
					fmt.Sprintf("Breakdown:  $%.2f\nPayment:    $%.2f\nDifference: $%.2f\n\n%s", breakdownTotal, amount, diff, advice),
				)
				return
			}

			if output.IsHumanOutput() {
				fmt.Printf("\n✓ Breakdown validated: %d items, total $%.2f\n\n", len(lineItems), breakdownTotal)
			}
		}

		client := getClient()

		result, err := client.CreatePaymentRequest(amount, paylinkCreateDesc, paylinkCreateReference, paylinkCreateExpiresIn, paylinkCreateRevealOwner, lineItems)
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
	paylinkCreateCmd.Flags().BoolVar(&paylinkCreateRevealOwner, "revealOwner", true, "Show owner email on payment page (default: true)")
	paylinkCreateCmd.Flags().StringVar(&paylinkCreateBreakdown, "breakdown", "", "Invoice breakdown: '2x Item @ $5.00' (one per line, use single quotes)")
}

var paylinkGetReference string

var paylinkGetCmd = &cobra.Command{
	Use:   "get <paylink-id>",
	Short: "Check if a paylink has been paid",
	Long: `Check the status of a payment link.

Shows whether the paylink is pending, paid, expired, or cancelled.
If paid, shows who paid and the transaction details.`,
	Example: `  botwallet paylink get pl_abc123def456
  botwallet paylink get --reference my-ref-123`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		var paylinkID string
		if len(args) > 0 {
			paylinkID = args[0]
		}

		if paylinkID == "" && paylinkGetReference == "" {
			output.ValidationError("Provide a paylink ID or --reference", "Usage: botwallet paylink get <paylink-id> OR botwallet paylink get --reference <ref>")
			return
		}

		client := getClient()

		result, err := client.GetPaymentRequest(paylinkID, paylinkGetReference)
		if err != nil {
			handleAPIError(err)
			return
		}

		if !output.IsHumanOutput() {
			output.JSON(result)
			return
		}

		status, _ := result["status"].(string)
		amount, _ := result["amount_usdc"].(float64)
		if amount == 0 {
			amount, _ = result["amount"].(float64)
		}

		output.Section("Payment Link")
		output.KeyValue("Paylink ID", result["request_id"])
		output.KeyValue("Status", status)
		output.KeyValueMoney("Amount", amount)
		output.KeyValue("Description", result["description"])

		if status == "pending" {
			output.KeyValue("Expires", result["expires_at"])
			if payURL, ok := result["payment_url"].(string); ok {
				output.KeyValueURL("Payment URL", payURL)
			}
			output.Tip("Share the payment URL to get paid!")
		} else if status == "completed" {
			received, _ := result["received_usdc"].(float64)
			if received == 0 {
				received, _ = result["received"].(float64)
			}
			if received > 0 {
				output.KeyValueMoney("Received (after fee)", received)
			}
			if paidBy, ok := result["paid_by"].(string); ok {
				output.KeyValue("Paid by", paidBy)
			}
			output.KeyValue("Paid at", result["paid_at"])
		}
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

Filter by status to see pending, paid, expired, or all paylinks.
Use this to track your earnings and outstanding payment links.`,
	Example: `  botwallet paylink list
  botwallet paylink list --status pending
  botwallet paylink list --status completed --limit 10`,
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		client := getClient()

		result, err := client.ListPaymentRequests(paylinkListStatus, paylinkListLimit, paylinkListOffset)
		if err != nil {
			handleAPIError(err)
			return
		}

		if !output.IsHumanOutput() {
			output.JSON(result)
			return
		}

		// Current API: "payment_requests", Legacy: "requests"
		var paylinks []interface{}
		if prs, ok := result["payment_requests"].([]interface{}); ok {
			paylinks = prs
		} else if prs, ok := result["requests"].([]interface{}); ok {
			paylinks = prs
		}

		total := 0
		if t, ok := result["total"].(float64); ok {
			total = int(t)
		}

		output.Section("Payment Links")

		if len(paylinks) == 0 {
			output.InfoMsg("No payment links found.")
			output.Tip("Create one with: botwallet paylink create <amount> --desc \"...\"")
			return
		}

		rows := make([]output.TableRow, 0, len(paylinks))
		for _, r := range paylinks {
			pl, ok := r.(map[string]interface{})
			if !ok {
				continue
			}

			shortCode := ""
			if sc, ok := pl["short_code"].(string); ok {
				shortCode = sc
			}
			status := ""
			if s, ok := pl["status"].(string); ok {
				status = s
			}
			amountStr := "$0.00"
			if a, ok := pl["amount_usdc"].(float64); ok {
				amountStr = formatPaylinkMoney(a)
			} else if a, ok := pl["amount"].(float64); ok {
				amountStr = formatPaylinkMoney(a)
			}
			desc := ""
			if d, ok := pl["description"].(string); ok {
				desc = truncatePaylinkString(d, 30)
			}

			rows = append(rows, output.TableRow{
				Columns: []string{shortCode, status, amountStr, desc},
			})
		}

		output.Table([]string{"Code", "Status", "Amount", "Description"}, rows)
		output.Println("\n  Total: %d paylinks", total)

		if hasMore, ok := result["has_more"].(bool); ok && hasMore {
			output.Tip("Use --limit and --offset for more results.")
		}
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
		if !requireAPIKey() {
			return
		}

		paylinkID := args[0]
		client := getClient()

		result, err := client.CancelPaymentRequest(paylinkID)
		if err != nil {
			handleAPIError(err)
			return
		}

		if !output.IsHumanOutput() {
			output.JSON(result)
			return
		}

		output.SuccessMsg("Payment link cancelled!")
		output.KeyValue("Paylink ID", result["request_id"])
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
		if !requireAPIKey() {
			return
		}

		paylinkID := args[0]

		if paylinkSendTo == "" {
			output.ValidationError("--to flag is required", "Provide a recipient: --to client@example.com OR --to @bot-username")
			return
		}

		var toEmail, toWallet string
		if strings.HasPrefix(paylinkSendTo, "@") {
			toWallet = paylinkSendTo
		} else if strings.Contains(paylinkSendTo, "@") {
			toEmail = paylinkSendTo
		} else {
			output.ValidationError("Invalid --to value", "Use an email (user@example.com) or bot username (@bot-name)")
			return
		}

		client := getClient()

		result, err := client.SendPaylinkInvitation(paylinkID, toEmail, toWallet, paylinkSendMessage)
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatSendPaylinkInvitation(result)
	},
}

func init() {
	paylinkSendCmd.Flags().StringVar(&paylinkSendTo, "to", "", "Recipient: email address or @bot-username (required)")
	paylinkSendCmd.Flags().StringVar(&paylinkSendMessage, "message", "", "Optional personal message included with the request")
}

func formatPaylinkMoney(amount float64) string {
	return "$" + strconv.FormatFloat(amount, 'f', 2, 64)
}

func truncatePaylinkString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func init() {
	paylinkCmd.AddCommand(paylinkCreateCmd)
	paylinkCmd.AddCommand(paylinkSendCmd)
	paylinkCmd.AddCommand(paylinkGetCmd)
	paylinkCmd.AddCommand(paylinkListCmd)
	paylinkCmd.AddCommand(paylinkCancelCmd)
}
