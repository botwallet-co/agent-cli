package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/output"
)

var (
	historyType   string
	historyLimit  int
	historyOffset int
)

var historyCmd = &cobra.Command{
	Use:     "history",
	Aliases: []string{"transactions"},
	Short:   "View transaction history",
	Long: `View your complete transaction history.

Shows all transactions including:
- Deposits and payments received (money in)
- Payments, x402 payments and withdrawals (money out)
- Fees, refunds and adjustments (credits/debits)

Filters:
  --type in     Money in: deposits, payments received, refunds
  --type out    Money out: payments, x402 payments, withdrawals, fees
  --type all    Everything, including adjustments (default)

Each transaction's "type" field says which kind it is.

Note: 'transactions' is an alias for 'history'.`,
	Example: `  botwallet history
  botwallet history --type in
  botwallet history --type out --limit 50`,
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		// The server filters by direction only. Any other value used to come
		// back as the full, unfiltered list.
		txType := strings.ToLower(strings.TrimSpace(historyType))
		switch txType {
		case "", "all", "in", "out":
		default:
			output.ValidationError("Unknown --type: "+historyType,
				"Use in, out or all. To find one kind (payment, deposit, withdrawal, ...), list all and read each transaction's type field")
			return
		}

		client := getClient()

		result, err := client.Transactions(txType, historyLimit, historyOffset)
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatTransactions(result)
	},
}

func init() {
	historyCmd.Flags().StringVar(&historyType, "type", "", "Filter: in (money in), out (money out), all (default: all)")
	historyCmd.Flags().IntVar(&historyLimit, "limit", 20, "Maximum results to return")
	historyCmd.Flags().IntVar(&historyOffset, "offset", 0, "Offset for pagination")
}

var limitsCmd = &cobra.Command{
	Use:   "limits",
	Short: "View your spending limits and guard rails",
	Long: `View your wallet's guard rails (spending limits and restrictions).

Guard rails are set by your human owner and control:
- Maximum amount per transaction
- Daily spending limit
- Which merchants/bots you can pay
- Whether you can create payment requests
- Withdrawal permissions and limits

Use this to understand what actions you can and cannot take.`,
	Example: "  botwallet limits",
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		client := getClient()

		result, err := client.MyLimits()
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatLimits(result)
	},
}

var approvalsCmd = &cobra.Command{
	Use:   "approvals",
	Short: "View pending owner approvals",
	Long: `View actions waiting for your human owner's approval.

Actions that may require owner approval:
- Payments above your per-transaction approval threshold
- All withdrawals (owner must approve before you can confirm)

Share the approval URLs with your owner to get these approved.`,
	Example: "  botwallet approvals",
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		client := getClient()

		result, err := client.PendingApprovals()
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatPendingApprovals(result)
	},
}
