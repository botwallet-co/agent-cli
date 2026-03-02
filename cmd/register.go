package cmd

import (
	"github.com/spf13/cobra"
)

// registerCmd is a top-level alias for 'wallet create'
// This matches the "register" terminology used in web onboarding.
var registerCmd = &cobra.Command{
	Use:   "register",
	Short: "Create a new Botwallet (same as 'wallet create')",
	Long: `Create a new Botwallet for your AI agent.

This is an alias for 'wallet create' — both commands do exactly the same thing.
Use whichever matches your workflow.

This command:
1. Performs threshold key generation (your key share stays on your machine)
2. Saves your key share to ~/.botwallet/seeds/<name>.seed
3. Registers with Botwallet (server holds its own separate key share)
4. Saves the API key to ~/.botwallet/config.json

Your wallet is secured with 2-of-2 threshold signing — neither the agent
nor the server can sign transactions alone.

The wallet will be in "unclaimed" status until a human owner claims it.

TIP: Name it so your human recognizes it — e.g., "Orion's Wallet" for a
general wallet, or "API Allowance Wallet" for a specific purpose.`,
	Example: `  botwallet register --name "Research Wallet"
  botwallet register --name "Research Wallet" --owner human@example.com
  
  # Equivalent to:
  botwallet wallet create --name "Research Wallet"`,
	Run: runWalletCreate, // Same function as wallet create
}

func init() {
	// Same flags as wallet create
	registerCmd.Flags().StringVarP(&walletCreateName, "name", "n", "", "Name for your wallet (required)")
	registerCmd.Flags().StringVarP(&walletCreateAgentModel, "model", "m", "", "Agent model (e.g., 'gpt-4', 'claude-3')")
	registerCmd.Flags().StringVar(&walletCreateOwner, "owner", "", "Owner's email (wallet appears in their portal)")
	registerCmd.MarkFlagRequired("name")
}
