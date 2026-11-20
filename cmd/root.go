// =============================================================================
// Botwallet CLI - Root Command
// =============================================================================
// The root command handles global flags and sets up the CLI framework.
// All subcommands are registered here.
// =============================================================================

// Package cmd implements the botwallet commands with Cobra. A command
// resolves the wallet it acts as (session.go), checks its arguments, calls
// the Botwallet API (package api) and hands the result to package output,
// which prints JSON or, with --human, text for people. Every failure ends
// in exitError (errors.go), with exit code 1.
//
// Besides one file per command group, the package has utilities.go (ping,
// lookup, version and the balance shortcut) and the Key 1 signing flow of
// the confirm commands: frost_sign.go, signing_intent.go and
// signing_errors.go.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/output"
)

// Version info (set from main.go)
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// Global flags
var (
	apiKeyFlag  string
	baseURLFlag string
	walletFlag  string // Select which local wallet to use
	humanFlag   bool   // Enable human-readable output (default is JSON for bots)
)

// SetVersionInfo sets version information from main, and the version the
// API client sends in its User-Agent.
func SetVersionInfo(v, c, d string) {
	v = buildVersion(v, debug.ReadBuildInfo)
	version = v
	commit = c
	date = d
	api.SetVersion(v)
	// botwallet --version prints what 'botwallet version' prints.
	rootCmd.Version = v
	rootCmd.SetVersionTemplate("{{botwalletVersion}}")
}

// buildVersion returns the version to report. Release builds set v with
// -ldflags. 'go install ...@version' sets no ldflags (v is "dev"), so it
// falls back to the module version Go records in the binary, without the
// leading "v" so it reads like a release version (0.1.0-beta.12).
func buildVersion(v string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if v != "" && v != "dev" {
		return v
	}
	if info, ok := readBuildInfo(); ok && info != nil {
		if mv := info.Main.Version; mv != "" && mv != "(devel)" {
			return strings.TrimPrefix(mv, "v")
		}
	}
	return "dev"
}

// versionInfo is the output of 'botwallet version' and --version: JSON, or
// text with --human. --version is answered before PersistentPreRun runs,
// so it reads the --human flag directly.
func versionInfo() string {
	if humanFlag {
		return fmt.Sprintf("Botwallet CLI v%s\n  Commit: %s\n  Built:  %s\n", version, commit, date)
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]string{"version": version, "commit": commit, "date": date})
	return b.String()
}

// rootCmd is the base command
var rootCmd = &cobra.Command{
	Use:   "botwallet",
	Short: "Botwallet CLI - Payment infrastructure for AI agents",
	Long: `Botwallet CLI - Payment infrastructure for AI agents

Designed for autonomous AI agents. Outputs JSON by default for easy parsing.
Use --human flag for human-readable output with colors and tips.

Quick Start:
  1. botwallet register --name "Orion's Wallet" --owner human@example.com
  2. Tell your human to claim the wallet (claim_url + claim_code shown after step 1)
  3. botwallet wallet balance                  Check balance
  4. botwallet pay @merchant 10.00             Create payment intent
  5. botwallet pay confirm <id>                FROST sign & submit

Commands:
  register  Create a new wallet (alias: wallet create)
  wallet    Manage wallets (create, info, balance, list, use, deposit, owner, rename, backup, export, import)
  pay       Send payments (two-step: pay → pay confirm)
  paylink   Create payment links to earn money
  fund      Request funds from your human owner
  withdraw  Withdraw USDC (two-step: withdraw → owner approves → withdraw confirm)
  x402      Pay for x402 APIs (two-step: x402 fetch → x402 fetch confirm)

Utilities:
  history   Transaction history
  limits    Spending limits and guard rails
  approvals Pending owner approvals (list all)
  approval  Check status of a specific approval
  events    Check notifications (approvals, deposits, etc.)
  lookup    Check if recipient exists
  ping      Test API connectivity
  version   Print version information
  docs      Full documentation`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		output.SetHumanOutput(humanFlag)
		requireConfigDir(cmd)
	},
}

// localFilesAnnotation set to "none" marks a command that never reads or
// writes the local wallet files, so it runs without a folder for them.
const localFilesAnnotation = "botwallet.local-files"

// requireConfigDir stops early when there is no safe folder for wallet keys
// (no HOME and no BOTWALLET_HOME), instead of failing halfway through a
// command. Commands that never touch local files are exempt.
func requireConfigDir(cmd *cobra.Command) {
	if !usesLocalFiles(cmd) {
		return
	}
	if _, err := config.ResolveConfigDir(); err != nil {
		exitError("NO_HOME_DIR", err.Error(), noHomeDirHowToFix, nil)
	}
}

// usesLocalFiles reports whether cmd may read or write the local wallet
// files. Commands marked with localFilesAnnotation do not, and neither do
// Cobra's own top-level commands: help, completion (and its bash, zsh, ...
// subcommands) and the hidden __complete that shell completion runs.
func usesLocalFiles(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[localFilesAnnotation] == "none" {
			return false
		}
		if c.HasParent() && !c.Parent().HasParent() {
			switch c.Name() {
			case "help", "completion", cobra.ShellCompRequestCmd:
				return false
			}
		}
	}
	return true
}

// Execute runs the CLI
func Execute() error {
	// Silence Cobra's default error/usage output so we control formatting.
	// In JSON mode (default), Cobra's plaintext errors would break parsing.
	// In human mode, we still provide clear error messages with --help hints.
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	err := rootCmd.Execute()
	if err != nil {
		// Cobra-level error (arg/flag parsing failed before PersistentPreRun).
		// PersistentPreRun hasn't run, so humanFlag may not be applied yet.
		applyHumanFlagFromArgs()
		exitError("INVALID_USAGE", err.Error(),
			"Run the command with --help for usage information", nil)
		return err
	}
	return nil
}

// applyHumanFlagFromArgs turns on human output if --human is on the
// command line. It is for errors Cobra reports while parsing arguments,
// before PersistentPreRun has applied the parsed flag.
func applyHumanFlagFromArgs() {
	for _, arg := range os.Args {
		if arg == "--human" {
			output.SetHumanOutput(true)
			return
		}
	}
}

func init() {
	cobra.AddTemplateFunc("botwalletVersion", versionInfo)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&apiKeyFlag, "api-key", "", "API key (or set BOTWALLET_API_KEY)")
	rootCmd.PersistentFlags().StringVar(&walletFlag, "wallet", "", "Use specific local wallet (see 'botwallet wallet list')")
	rootCmd.PersistentFlags().StringVar(&baseURLFlag, "api-url", "", "API base URL (for development)")
	rootCmd.PersistentFlags().BoolVar(&humanFlag, "human", false, "Human-readable output with colors and formatting")

	// =========================================================================
	// TOP-LEVEL COMMANDS
	// =========================================================================

	// Register alias (matches web onboarding terminology)
	rootCmd.AddCommand(registerCmd)

	// =========================================================================
	// COMMAND GROUPS
	// =========================================================================

	// Wallet management group
	rootCmd.AddCommand(walletCmd)

	// Payment group (two-step flow)
	rootCmd.AddCommand(payCmd)

	// Payment links (earning)
	rootCmd.AddCommand(paylinkCmd)

	// Fund requests (from owner)
	rootCmd.AddCommand(fundCmd)

	// Withdrawals
	rootCmd.AddCommand(withdrawCmd)

	// x402 paid API access
	rootCmd.AddCommand(x402Cmd)

	// =========================================================================
	// TOP-LEVEL UTILITIES
	// =========================================================================

	// History & status
	rootCmd.AddCommand(historyCmd)
	rootCmd.AddCommand(limitsCmd)
	rootCmd.AddCommand(approvalsCmd)
	rootCmd.AddCommand(approvalCmd)
	rootCmd.AddCommand(eventsCmd)

	// Utilities
	rootCmd.AddCommand(lookupCmd)
	rootCmd.AddCommand(balanceCmd)
	rootCmd.AddCommand(pingCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(docsCmd)
}
