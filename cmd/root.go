// =============================================================================
// Botwallet CLI - Root Command
// =============================================================================
// The root command handles global flags and sets up the CLI framework.
// All subcommands are registered here.
// =============================================================================

package cmd

import (
	"fmt"
	"os"
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

// SetVersionInfo sets version information from main
func SetVersionInfo(v, c, d string) {
	version = v
	commit = c
	date = d
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
  wallet    Manage wallets (create, info, balance, list, use, deposit, owner, backup, export, import)
  pay       Send payments (two-step: pay → pay confirm)
  paylink   Create payment links to earn money
  fund      Request funds from your human owner
  withdraw  Withdraw USDC (two-step: withdraw → owner approves → withdraw confirm)

Utilities:
  history   Transaction history
  limits    Spending limits and guard rails
  approvals Pending owner approvals (list all)
  approval  Check status of a specific approval
  events    Check notifications (approvals, deposits, etc.)
  lookup    Check if recipient exists
  ping      Test API connectivity
  docs      Full documentation`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Default is JSON (for bots), --human enables rich terminal output
		output.SetHumanOutput(humanFlag)
	},
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
		// Scan os.Args directly to detect --human.
		for _, arg := range os.Args {
			if arg == "--human" {
				output.SetHumanOutput(true)
				break
			}
		}
		output.APIError("INVALID_USAGE", err.Error(),
			"Run the command with --help for usage information", nil)
		return err
	}
	return nil
}

func init() {
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
	rootCmd.AddCommand(pingCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(docsCmd)
}

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
		client := getClient()

		result, err := client.Ping()
		if err != nil {
			handleAPIError(err)
			return
		}

		if output.IsHumanOutput() {
			output.SuccessMsg("API is reachable!")
			output.KeyValue("Version", result["version"])
			output.KeyValue("Timestamp", result["timestamp"])
			return
		}

		output.JSON(result)
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
		if !requireAPIKey() {
			return
		}

		// Strip @ prefix if user typed @username (allows both @user and user)
		username := stripAtPrefix(args[0])
		client := getClient()

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
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		if output.IsHumanOutput() {
			fmt.Printf("Botwallet CLI v%s\n", version)
			fmt.Printf("  Commit: %s\n", commit)
			fmt.Printf("  Built:  %s\n", date)
			return
		}
		output.JSON(map[string]string{
			"version": version,
			"commit":  commit,
			"date":    date,
		})
	},
}

// =============================================================================
// Helper Functions
// =============================================================================

// getClient creates an API client with the configured API key
func getClient() *api.Client {
	apiKey, err := config.GetAPIKeyWithWallet(apiKeyFlag, walletFlag)
	if err != nil {
		output.APIError("WALLET_NOT_FOUND", err.Error(),
			"Use 'botwallet wallet list' to see available wallets",
			nil,
		)
		os.Exit(1)
	}
	baseURL := config.GetBaseURL(baseURLFlag)

	if baseURL != "" {
		return api.NewClientWithURL(apiKey, baseURL)
	}
	return api.NewClient(apiKey)
}

// getClientNoAuth creates an API client without authentication.
// Used for unauthenticated actions like wallet_import_key.
func getClientNoAuth() *api.Client {
	baseURL := config.GetBaseURL(baseURLFlag)
	if baseURL != "" {
		return api.NewClientWithURL("", baseURL)
	}
	return api.NewClient("")
}

// requireAPIKey ensures an API key is available
// Returns true if API key exists, false if missing (and outputs error)
func requireAPIKey() bool {
	apiKey, err := config.GetAPIKeyWithWallet(apiKeyFlag, walletFlag)
	if err != nil {
		output.APIError("WALLET_NOT_FOUND", err.Error(),
			"Use 'botwallet wallet list' to see available wallets",
			nil,
		)
		os.Exit(1)
		return false
	}
	if apiKey == "" {
		wallets, _ := config.ListWallets()

		howToFix := "Set BOTWALLET_API_KEY environment variable, use --api-key flag, or create a wallet"
		if len(wallets) > 0 {
			var names []string
			for _, w := range wallets {
				names = append(names, w.LocalName)
			}
			howToFix = fmt.Sprintf("Use --wallet flag to select a wallet. Available: %v", names)
		}

		output.APIError(
			"NO_API_KEY",
			"API key required",
			howToFix,
			map[string]interface{}{
				"config_path": config.ConfigPath(),
				"example":     "botwallet --api-key bw_bot_xxx wallet balance",
				"env_var":     "BOTWALLET_API_KEY",
				"no_wallet":   "If you don't have a wallet yet, run: botwallet wallet create --name \"YourBotName\"",
			},
		)
		os.Exit(1)
		return false
	}
	return true
}

// handleAPIError handles API errors and outputs them appropriately
func handleAPIError(err error) {
	if apiErr, ok := err.(*api.APIError); ok {
		// Provide default how_to_fix if API didn't supply one
		howToFix := apiErr.HowToFix
		if howToFix == "" {
			howToFix = getDefaultHowToFix(apiErr.Code, apiErr.Details)
		}
		// Transform terminology for consistency
		message := transformErrorMessage(apiErr.Message)
		output.APIError(apiErr.Code, message, howToFix, apiErr.Details)
	} else {
		output.APIError("CLI_ERROR", err.Error(), "Check your network connection and try again", nil)
	}
	os.Exit(1)
}

// transformErrorMessage updates error messages to use current CLI terminology
func transformErrorMessage(msg string) string {
	// Replace "Payment request" with "Paylink" for consistency
	msg = strings.ReplaceAll(msg, "Payment request", "Paylink")
	msg = strings.ReplaceAll(msg, "payment request", "paylink")
	return msg
}

// stripAtPrefix removes leading @ from usernames for API calls
// This allows users to type @username or username - both will work
func stripAtPrefix(username string) string {
	return strings.TrimPrefix(username, "@")
}

// getDefaultHowToFix provides default fix suggestions for common errors
func getDefaultHowToFix(code string, details map[string]interface{}) string {
	switch code {
	case "INSUFFICIENT_FUNDS":
		if url, ok := details["funding_url"].(string); ok {
			return "Add funds via " + url + " or use 'botwallet fund ask' command"
		}
		return "Add funds to your wallet using 'botwallet wallet deposit' or 'botwallet fund ask'"
	case "RECIPIENT_NOT_FOUND":
		return "Use 'botwallet lookup <username>' to verify the recipient exists"
	case "VALIDATION_ERROR":
		return "Check command parameters and try again"
	case "UNAUTHORIZED":
		return "Check your API key is correct. Use --api-key flag or set BOTWALLET_API_KEY"
	case "DAILY_LIMIT_EXCEEDED":
		return "Wait until tomorrow or ask your owner to increase your daily limit"
	case "APPROVAL_REQUIRED":
		return "Share the approval_url with your human owner to approve this action"
	case "WALLET_NOT_CLAIMED":
		claimURL, hasURL := details["claim_url"].(string)
		claimCode, hasCode := details["claim_code"].(string)
		pledgedTo, hasPledged := details["pledged_to"].(string)

		if hasURL && hasCode {
			msg := fmt.Sprintf("⚠️ BLOCKED: Your wallet must be claimed by a human owner first! "+
				"Tell your human: \"Please claim my wallet at %s using code %s\"", claimURL, claimCode)
			if hasPledged && pledgedTo != "" {
				msg += fmt.Sprintf(" (Pledged to: %s)", pledgedTo)
			}
			return msg
		}
		return "Your wallet needs to be claimed by a human owner. Run 'botwallet wallet info' to get the claim URL and code, then ask your human to claim it."
	case "WALLET_ALREADY_CLAIMED":
		return "This wallet is already claimed. Ask your owner to release it from their Human Portal first."
	default:
		return ""
	}
}
