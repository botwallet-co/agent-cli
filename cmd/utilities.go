package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/output"
)

// =============================================================================
// Ping Command
// =============================================================================

var pingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Test connectivity to the Botwallet API",
	Long: `Test connectivity to the Botwallet API.

Use this to verify the API is reachable and check the current version.
No authentication required.`,
	Example: "  botwallet ping",
	Run: func(cmd *cobra.Command, args []string) {
		client := newClient(currentWallet().APIKey)

		result, err := client.Ping()
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatPing(result)
	},
}

// =============================================================================
// Lookup Command
// =============================================================================

var lookupCmd = &cobra.Command{
	Use:   "lookup <username>",
	Short: "Check if a recipient exists",
	Long: `Check if a recipient exists before sending payment.

Use this to verify a recipient's username and type (merchant or agent)
before making a payment.`,
	Example: `  botwallet lookup @botverse
  botwallet lookup @claude-research`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client := authedClient()

		// Strip @ prefix if user typed @username (allows both @user and user)
		username := stripAtPrefix(args[0])

		result, err := client.Lookup(username)
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatLookup(result)
	},
}

// =============================================================================
// Version Command
// =============================================================================

var versionCmd = &cobra.Command{
	Use:         "version",
	Short:       "Print version information (also: botwallet --version)",
	Annotations: map[string]string{localFilesAnnotation: "none"},
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(versionInfo())
	},
}

// =============================================================================
// Balance Shortcut
// =============================================================================

// balanceCmd is a hidden shortcut for 'wallet balance', which agents often
// try first as 'botwallet balance'.
var balanceCmd = &cobra.Command{
	Use:    "balance",
	Short:  "Same as 'wallet balance'",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		walletBalanceCmd.Run(cmd, args)
	},
}
