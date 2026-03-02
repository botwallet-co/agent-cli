package cmd

import (
	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/output"
)

var approvalCmd = &cobra.Command{
	Use:   "approval",
	Short: "Check and manage owner approvals",
	Long: `Check the status of specific owner approvals.

Use this command to poll for human approval decisions after a payment,
withdrawal, or x402 API access was flagged for owner review.

Subcommands:
  status    Check the current status of a specific approval

Typical agent workflow:
  1. Run a command that requires approval (pay, x402 fetch, withdraw)
  2. Receive an approval_id in the response
  3. Save the approval_id to memory
  4. Periodically check: botwallet approval status <approval_id>
  5. When status is "approved", run the confirm command`,
	Example: `  # Check if a specific approval has been resolved
  botwallet approval status <approval_id>`,
}

var approvalStatusCmd = &cobra.Command{
	Use:   "status <approval_id>",
	Short: "Check the status of an approval",
	Long: `Check the current status of a specific approval by ID.

Returns one of:
  pending    — Still waiting for human owner decision
  approved   — Owner approved; proceed with the confirm command
  rejected   — Owner rejected the action
  expired    — Approval timed out (24h default)

This command is designed for agents to poll after an action requires
human approval. It returns quickly and uses minimal resources.

After "approved" status, run the appropriate confirm command:
  - Payment:    botwallet pay confirm <transaction_id>
  - x402 API:   botwallet x402 fetch confirm <fetch_id>
  - Withdrawal: botwallet withdraw confirm <withdrawal_id>`,
	Example: `  botwallet approval status abc123-def456
  botwallet approval status abc123-def456 --human`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		client := getClient()
		result, err := client.ApprovalStatus(args[0])
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatApprovalStatus(result)
	},
}

func init() {
	approvalCmd.AddCommand(approvalStatusCmd)
}
