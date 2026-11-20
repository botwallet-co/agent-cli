package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/output"
)

// The wallet subcommands that only read and change config.json.

var walletListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all stored wallets",
	Long: `List all wallets stored in your local configuration.

Shows each wallet's local name, server username, and indicates
which one is the default (used when no --wallet flag is specified).

Use 'botwallet wallet use <name>' to switch the default wallet.`,
	Example: "  botwallet wallet list",
	Run: func(cmd *cobra.Command, args []string) {
		wallets, err := config.ListWallets()
		if err != nil {
			exitError("CONFIG_ERROR", err.Error(), "Check "+config.ConfigPath(), nil)
			return
		}

		if len(wallets) == 0 {
			exitError("NO_WALLETS", "No wallets found", "Run 'botwallet wallet create' to create a wallet", nil)
			return
		}

		list := make([]output.WalletListEntry, 0, len(wallets))
		for _, w := range wallets {
			list = append(list, output.WalletListEntry{
				LocalName:   w.LocalName,
				Username:    w.Entry.Username,
				DisplayName: w.Entry.DisplayName,
				PublicKey:   w.Entry.PublicKey,
				IsDefault:   w.IsDefault,
				SeedFile:    w.Entry.SeedFile,
			})
		}

		output.FormatWalletList(list)
	},
}

var walletUseCmd = &cobra.Command{
	Use:   "use <wallet-name>",
	Short: "Switch default wallet",
	Long: `Switch the default wallet used for all commands.

The default wallet is used when no --wallet flag is specified.
Use 'botwallet wallet list' to see all available wallets.`,
	Example: `  botwallet wallet use my-research-wallet
  botwallet wallet use test-wallet`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		localName := args[0]

		// Check if wallet exists
		wallet, err := config.GetWallet(localName)
		var notFound *config.WalletNotFoundError
		switch {
		case errors.As(err, &notFound):
			howToFix := "Run 'botwallet wallet list' to see available wallets"
			if len(notFound.Available) > 0 {
				howToFix = fmt.Sprintf("Available wallets: %v", notFound.Available)
			}

			exitError("WALLET_NOT_FOUND",
				fmt.Sprintf("Wallet '%s' not found", localName),
				howToFix, nil)
			return
		case err != nil:
			exitWalletError(err)
			return
		}

		// Already the default? Compared by local name: two local wallets
		// can hold the same wallet (one export imported twice).
		_, current, err := config.GetDefaultWallet()
		alreadyDefault := err == nil && current == localName

		// Set as default
		if err := config.SetDefaultWallet(localName); err != nil {
			exitError("CONFIG_ERROR", err.Error(), "", nil)
			return
		}

		output.FormatWalletUse(localName, wallet.DisplayName, wallet.Username, wallet.PublicKey, alreadyDefault)
	},
}
