package cmd

import (
	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/output"
)

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
		client := authedClient()

		result, err := client.MyLimits()
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatLimits(result)
	},
}
