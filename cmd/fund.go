// =============================================================================
// Botwallet CLI - Fund Commands (Request Funds from Owner)
// =============================================================================
// fund ask, fund list
// These are FUND REQUESTS - requests to your human owner for more funds.
// =============================================================================

package cmd

import (
	"os"
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

Your owner sees these in their dashboard and can fund them (pay the
included payment link) or dismiss them.

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
			_ = cmd.Help()
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

The request goes to your owner's dashboard. Your owner can pay the request's
payment link (it is then marked funded and the USDC arrives in your wallet)
or dismiss it.

Always provide a reason so your owner knows why you need the funds!`,
	Example: `  botwallet fund ask 50.00 --reason "API costs running low"
  botwallet fund ask 100.00 --reason "Need to purchase database credits"`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client := authedClient()

		amountCents, ok := parseAmountArg(args[0], "50.00")
		if !ok {
			return
		}

		if fundAskReason == "" {
			exitValidationError("--reason flag is required", "Explain why you need the funds, e.g., --reason \"API costs\"")
			return
		}

		result, err := client.RequestFunds(centsToDollars(amountCents), fundAskReason)
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatFundRequest(result)
	},
}

func init() {
	fundAskCmd.Flags().StringVar(&fundAskReason, "reason", "", "Reason for requesting funds (required)")
	fundAskCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		msg := err.Error()
		if strings.Contains(msg, "unknown shorthand flag") || strings.Contains(msg, "unknown flag") {
			for _, a := range os.Args {
				if len(a) > 1 && a[0] == '-' && a[1] >= '0' && a[1] <= '9' {
					exitValidationError("Amount must be a positive number", "Provide a positive amount, e.g., botwallet fund ask 50.00 --reason \"...\"")
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
funded, and dismissed requests.`,
	Example: `  botwallet fund list
  botwallet fund list --status pending
  botwallet fund list --limit 10`,
	Run: func(cmd *cobra.Command, args []string) {
		client := authedClient()

		status, ok := fundStatusFilter(fundListStatus)
		if !ok {
			exitValidationError("Unknown status: "+fundListStatus,
				"Use one of: pending, funded, dismissed, all")
			return
		}

		result, err := client.ListFundRequests(status, fundListLimit, fundListOffset)
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatFundRequests(result)
	},
}

func init() {
	fundListCmd.Flags().StringVar(&fundListStatus, "status", "", "Filter by status: pending, funded, dismissed, all (default: all)")
	fundListCmd.Flags().IntVar(&fundListLimit, "limit", 20, "Maximum results to return")
	fundListCmd.Flags().IntVar(&fundListOffset, "offset", 0, "Offset for pagination")
}

// =============================================================================
// Helper Functions
// =============================================================================

// fundStatusFilter checks a --status value for fund list. A fund request is
// pending until the owner pays its link (funded) or dismisses it. Older
// help listed approved and denied, so those still work, as funded and
// dismissed. The server compares the value as is, so an unknown one would
// silently list nothing.
func fundStatusFilter(status string) (string, bool) {
	switch s := strings.ToLower(strings.TrimSpace(status)); s {
	case "", "all", "pending", "funded", "dismissed":
		return s, true
	case "approved":
		return "funded", true
	case "denied", "rejected":
		return "dismissed", true
	default:
		return "", false
	}
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
