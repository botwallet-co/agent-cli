package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/output"
)

// Backing up Key 1 takes two commands: wallet backup shows a warning and a
// one-time code, and wallet reveal-backup --code <code> prints the 12 words.

var walletBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Start the backup process for your wallet's Key 1 (12 secret words)",
	Long: `Start the Key 1 backup process.

This shows a warning and generates a one-time confirmation code.
You must then run 'wallet reveal-backup --code <code>' within ` + config.BackupNonceTTLText() + `
to reveal the 12 secret words (Key 1).

This two-step process prevents accidental exposure of sensitive key material.`,
	Example: "  botwallet wallet backup",
	Run: func(cmd *cobra.Command, args []string) {
		// The wallet this command acts as (same one the API key belongs to)
		w := currentWallet()
		if err := w.RequireLocal(); err != nil {
			exitWalletError(err)
			return
		}
		localName := w.LocalName

		// Generate one-time code
		code := config.GenerateBackupCode()

		// Save nonce
		if err := config.WriteBackupNonce(code, localName); err != nil {
			exitError("CONFIG_ERROR", fmt.Sprintf("Failed to create backup request: %v", err),
				"Check file permissions on "+config.ConfigDir(), nil)
			return
		}

		output.FormatBackupStarted(code, config.BackupNonceTTLText(), int(config.BackupNonceTTL.Seconds()))
	},
}

var revealBackupCode string

var walletRevealBackupCmd = &cobra.Command{
	Use:    "reveal-backup",
	Short:  "Reveal Key 1 — 12 secret words (requires confirmation code)",
	Hidden: true, // Intentionally unlisted — discovered only via 'wallet backup' output
	Run: func(cmd *cobra.Command, args []string) {
		if revealBackupCode == "" {
			exitValidationError("--code flag is required",
				"Run 'botwallet wallet backup' first to get a confirmation code")
			return
		}

		// Validate nonce
		localName, err := config.ValidateBackupNonce(revealBackupCode)
		if err != nil {
			exitError("BACKUP_ERROR", err.Error(),
				"Run 'botwallet wallet backup' to get a new confirmation code", nil)
			return
		}

		// Load the 12 secret words (Key 1)
		mnemonic, err := config.LoadWalletSeed(localName)
		if err != nil {
			exitError("CONFIG_ERROR", fmt.Sprintf("Failed to load Key 1: %v", err),
				"Check that the seed file exists in "+config.SeedsDir(), nil)
			return
		}

		output.FormatKey1Revealed(localName, mnemonic)
	},
}

func init() {
	walletRevealBackupCmd.Flags().StringVar(&revealBackupCode, "code", "", "Confirmation code from 'wallet backup'")
}
