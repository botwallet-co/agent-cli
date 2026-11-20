package cmd

import (
	"github.com/spf13/cobra"
)

// The wallet command group. Its subcommands are in wallet_create.go (create
// and its alias register), wallet_info.go (info, balance, deposit, owner,
// rename), wallet_local.go (list, use), wallet_backup.go (backup,
// reveal-backup) and wallet_transfer.go (export, import).

var walletCmd = &cobra.Command{
	Use:   "wallet",
	Short: "Manage your wallet",
	Long: `Manage your Botwallet account.

Subcommands:
  create    Create a new wallet (threshold key generation)
  info      Get wallet information and claim status
  balance   Check balance and spending limits
  list      List all locally stored wallets
  use       Switch the default wallet
  deposit   Get your deposit address for receiving funds
  owner     Update the pledged owner email
  rename    Rename your wallet's display name
  backup    Back up Key 1 (your 12 secret words)
  export    Export wallet to a .bwlt file to move it (works for 24 hours)
  import    Import wallet from a .bwlt file`,
	Example: `  botwallet wallet create --name "Research Wallet" --owner human@example.com
  botwallet wallet info
  botwallet wallet balance
  botwallet wallet list
  botwallet wallet use my-other-wallet
  botwallet wallet deposit
  botwallet wallet owner new-owner@example.com
  botwallet wallet rename "New Display Name"
  botwallet wallet backup
  botwallet wallet export -o wallet.bwlt
  botwallet wallet import wallet.bwlt`,
}

func init() {
	walletCmd.AddCommand(walletCreateCmd)
	walletCmd.AddCommand(walletInfoCmd)
	walletCmd.AddCommand(walletBalanceCmd)
	walletCmd.AddCommand(walletListCmd)
	walletCmd.AddCommand(walletUseCmd)
	walletCmd.AddCommand(walletDepositCmd)
	walletCmd.AddCommand(walletOwnerCmd)
	walletCmd.AddCommand(walletRenameCmd)
	walletCmd.AddCommand(walletBackupCmd)
	walletCmd.AddCommand(walletRevealBackupCmd)
	walletCmd.AddCommand(walletExportCmd)
	walletCmd.AddCommand(walletImportCmd)
}
