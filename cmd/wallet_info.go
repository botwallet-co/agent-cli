package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/output"
)

// The wallet subcommands that read or change the wallet on the server.

var walletInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Get wallet information",
	Long: `Get detailed information about your wallet.

Shows wallet ID, username, status, balance, and deposit information.
Use this to check your wallet's claim status and overall state.`,
	Example: "  botwallet wallet info",
	Run: func(cmd *cobra.Command, args []string) {
		client := authedClient()

		result, err := client.Info()
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatInfo(result)
	},
}

var walletBalanceCmd = &cobra.Command{
	Use:   "balance",
	Short: "Check your balance and spending limits",
	Long: `Check your current balance and daily spending limits.

Shows:
- Current available balance
- Daily spending limit (if set)
- Amount spent today
- Remaining spending allowance

Use this before making payments to ensure you have sufficient funds.`,
	Example: "  botwallet wallet balance",
	Run: func(cmd *cobra.Command, args []string) {
		client := authedClient()

		result, err := client.Balance()
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatBalance(result)
	},
}

var walletDepositCmd = &cobra.Command{
	Use:   "deposit",
	Short: "Get your deposit address for receiving funds",
	Long: `Get your Solana deposit address to receive USDC.

Your deposit address is a Solana wallet address where you can receive
USDC deposits. The funds will be credited to your balance within
1-2 minutes of confirmation.

You can also share the funding URL with your human owner for an
easier deposit experience via the web interface.`,
	Example: "  botwallet wallet deposit",
	Run: func(cmd *cobra.Command, args []string) {
		client := authedClient()

		result, err := client.GetDepositAddress()
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatDepositAddress(result)
	},
}

var walletOwnerCmd = &cobra.Command{
	Use:   "owner <email>",
	Short: "Update the pledged owner email",
	Long: `Change the email address this wallet is pledged to.

This only works for UNCLAIMED wallets. If your wallet is already claimed,
you need to ask your current owner to release it from their Human Portal first.

The new owner will see this wallet in their portal when they log in.`,
	Example: `  botwallet wallet owner new-owner@example.com
  botwallet wallet owner boss@company.com`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client := authedClient()

		ownerEmail := args[0]

		result, err := client.UpdateOwner(ownerEmail)
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatOwnerUpdated(result)
	},
}

var walletRenameCmd = &cobra.Command{
	Use:   "rename <new-name>",
	Short: "Rename your wallet's display name",
	Long: `Change the display name of your wallet.

This updates the human-readable name only. Your unique username (@slug)
never changes — it's used in fund URLs, paylinks, and recipient lookup.

Works for both claimed and unclaimed wallets.`,
	Example: `  botwallet wallet rename "Research Budget"
  botwallet wallet rename "Production API Wallet"`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		wallet, client := authedWallet()

		newName := strings.TrimSpace(args[0])
		if len(newName) < 2 || len(newName) > 50 {
			exitValidationError(
				"Name must be 2-50 characters",
				"botwallet wallet rename \"My New Wallet Name\"",
			)
			return
		}

		result, err := client.UpdateName(newName)
		if err != nil {
			handleAPIError(err)
			return
		}

		// Server succeeded — update the same wallet's local config (best-effort)
		if wallet.Entry != nil {
			if cfgErr := config.UpdateWalletDisplayName(wallet.LocalName, newName); cfgErr != nil {
				if output.IsHumanOutput() {
					output.WarningMsg("Server updated, but local config update failed: %v", cfgErr)
				}
			}
		}

		output.FormatWalletRenamed(result)
	},
}
