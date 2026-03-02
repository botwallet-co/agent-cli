// =============================================================================
// Botwallet CLI - Fund Commands (Request Funds from Owner)
// =============================================================================
// fund ask, fund list
// These are FUND REQUESTS - requests to your human owner for more funds.
// =============================================================================

package cmd

import (
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/output"
)

// =============================================================================
// Fund Parent Command
// =============================================================================

var fundCmd = &cobra.Command{
	Use:   "fund [amount] [--reason ...]",
	Short: "Request funds from your owner",
	Long: `Request additional funds from your human owner.

Fund requests are sent to YOUR OWNER ONLY - the human who claimed your wallet.
This is different from payment requests, which anyone can pay.

Use this when:
- You need more funds to continue operations
- Your balance is running low
- You need to pay for external services

Your owner sees these in their Human Portal and can approve/deny them.

You can call this directly or use subcommands:
  botwallet fund 50.00 --reason "..."   (shortcut for fund ask)
  botwallet fund ask 50.00 --reason "..."
  botwallet fund list`,
	Example: `  botwallet fund 50.00 --reason "API costs"
  botwallet fund ask 50.00 --reason "API costs"
  botwallet fund list`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			cmd.Help()
			return
		}
		fundAskCmd.Run(cmd, args)
	},
}

// =============================================================================
// fund ask
// =============================================================================

var fundAskReason string

var fundAskCmd = &cobra.Command{
	Use:   "ask <amount>",
	Short: "Request funds from your human owner",
	Long: `Request funds from your human owner.

The request goes to your owner's Human Portal where they can approve or deny it.
Once approved, the funds are deposited directly into your wallet.

Always provide a reason so your owner knows why you need the funds!`,
	Example: `  botwallet fund ask 50.00 --reason "API costs running low"
  botwallet fund ask 100.00 --reason "Need to purchase database credits"`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
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

		if fundAskReason == "" {
			output.ValidationError("--reason flag is required", "Explain why you need the funds, e.g., --reason \"API costs\"")
			return
		}

		client := getClient()

		result, err := client.RequestFunds(amount, fundAskReason)
		if err != nil {
			handleAPIError(err)
			return
		}

		// JSON output (default for bots)
		if !output.IsHumanOutput() {
			output.JSON(result)
			return
		}

		// Human-readable output
		output.SuccessMsg("Fund request submitted!")
		output.Section("Request Details")
		output.KeyValue("Request ID", result["request_id"])
		respAmount, _ := result["amount_usdc"].(float64)
		if respAmount == 0 {
			respAmount, _ = result["amount"].(float64)
		}
		output.KeyValueMoney("Amount", respAmount)
		output.KeyValue("Reason", result["reason"])
		if sentTo, ok := result["sent_to"].(string); ok && sentTo != "" {
			output.KeyValue("Notified", sentTo)
		}
		if payURL, ok := result["payment_url"].(string); ok && payURL != "" {
			output.KeyValue("Funding Link", payURL)
		}

		output.Tip("Your owner will review this in their Human Portal.")
	},
}

func init() {
	fundAskCmd.Flags().StringVar(&fundAskReason, "reason", "", "Reason for requesting funds (required)")
	fundAskCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		msg := err.Error()
		if strings.Contains(msg, "unknown shorthand flag") || strings.Contains(msg, "unknown flag") {
			for _, a := range os.Args {
				if len(a) > 1 && a[0] == '-' && a[1] >= '0' && a[1] <= '9' {
					output.ValidationError("Amount must be a positive number", "Provide a positive amount, e.g., botwallet fund ask 50.00 --reason \"...\"")
					os.Exit(1)
				}
			}
		}
		return err
	})
}

// =============================================================================
// fund list
// =============================================================================

var (
	fundListStatus string
	fundListLimit  int
	fundListOffset int
)

var fundListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your fund requests",
	Long: `List your fund request history.

Shows all requests you've made to your owner, including pending,
approved, and denied requests.`,
	Example: `  botwallet fund list
  botwallet fund list --status pending
  botwallet fund list --limit 10`,
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		client := getClient()

		result, err := client.ListFundRequests(fundListStatus, fundListLimit, fundListOffset)
		if err != nil {
			handleAPIError(err)
			return
		}

		// JSON output (default for bots)
		if !output.IsHumanOutput() {
			output.JSON(result)
			return
		}

		// Human-readable output
		var requests []interface{}
		if r, ok := result["requests"].([]interface{}); ok {
			requests = r
		}
		total := 0
		if t, ok := result["total"].(float64); ok {
			total = int(t)
		}

		output.Section("Fund Requests")

		if len(requests) == 0 {
			output.InfoMsg("No fund requests found.")
			output.Tip("Request funds with: botwallet fund ask <amount> --reason \"...\"")
			return
		}

		rows := make([]output.TableRow, 0, len(requests))
		for _, r := range requests {
			req, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			reqID, _ := req["request_id"].(string)
			status, _ := req["status"].(string)
			amount, _ := req["amount_usdc"].(float64)
			if amount == 0 {
				amount, _ = req["amount"].(float64)
			}
			reason, _ := req["reason"].(string)
			rows = append(rows, output.TableRow{
				Columns: []string{
					truncateFundID(reqID),
					status,
					formatFundMoney(amount),
					truncateFundString(reason, 30),
				},
			})
		}

		output.Table([]string{"ID", "Status", "Amount", "Reason"}, rows)
		output.Println("\n  Total: %d requests", total)

		if hasMore, ok := result["has_more"].(bool); ok && hasMore {
			output.Tip("Use --limit and --offset for more results.")
		}
	},
}

func init() {
	fundListCmd.Flags().StringVar(&fundListStatus, "status", "", "Filter by status: pending, approved, denied")
	fundListCmd.Flags().IntVar(&fundListLimit, "limit", 20, "Maximum results to return")
	fundListCmd.Flags().IntVar(&fundListOffset, "offset", 0, "Offset for pagination")
}

// =============================================================================
// Helper Functions
// =============================================================================

func formatFundMoney(amount float64) string {
	return "$" + strconv.FormatFloat(amount, 'f', 2, 64)
}

func truncateFundString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func truncateFundID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:8] + "..."
}

// =============================================================================
// Register subcommands
// =============================================================================

func init() {
	fundCmd.AddCommand(fundAskCmd)
	fundCmd.AddCommand(fundListCmd)

	// Register --reason on the parent too so `fund 50.00 --reason "..."` works
	fundCmd.Flags().StringVar(&fundAskReason, "reason", "", "Reason for requesting funds (required)")
	fundCmd.SetFlagErrorFunc(fundAskCmd.FlagErrorFunc())
}
