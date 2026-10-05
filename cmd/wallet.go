package cmd

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/output"
	"github.com/botwallet-co/agent-cli/solana/frost"
)

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

var (
	walletCreateName       string
	walletCreateAgentModel string
	walletCreateOwner      string
	walletCreateDesc       string
	walletCreateMeta       []string
)

var walletCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new Botwallet",
	Long: `Create a new Botwallet for your AI agent.

This command:
1. Performs threshold key generation (your key share never leaves your machine)
2. Saves your key share to ~/.botwallet/seeds/<name>.seed
3. Registers the wallet with Botwallet (server holds its own key share)
4. Saves the API key to ~/.botwallet/config.json

Your wallet is secured with 2-of-2 threshold signing:
- The agent holds one key share (S1) locally
- The server holds the other key share (S2)
- Neither party can sign alone — both must cooperate

The wallet starts "unclaimed" until a human owner claims it via a claim link.
Use --owner to pledge the wallet to a specific email — they'll see it in their portal.

Naming (--name is required):
  This is your wallet — just pick a name and proceed. No need to ask your
  human; it keeps the experience seamless. Use your own name, a creative name,
  or describe the wallet's purpose.`,
	Example: `  botwallet wallet create --name "Assistant's Wallet"
  botwallet wallet create --name "Research Budget" --owner human@example.com
  botwallet wallet create --name "x402 APIs Allowance"`,
	Args: walletCreateArgs,
	Run:  runWalletCreate,
}

// runWalletCreate is the shared implementation for 'wallet create' and 'register'.
//
// FROST DKG Protocol (2 rounds):
//
//	Round 1: CLI calls dkg_init → server generates S2, returns A2 (server's public share)
//	Round 2: CLI generates S1, computes group key A = A1+A2, calls dkg_complete with A1 and A
//	Server verifies A == A1+A2, creates the wallet, returns API key
//
// SECURITY: S1 (bot's key share) is generated locally, saved to disk, and NEVER
// sent to the server. Only public key shares (A1, A2) and the group key (A) cross the wire.
// The CLI output NEVER displays S1 or any secret material.
//
// S1 is written to disk BEFORE dkg_complete, and is never deleted once the
// wallet may exist on the server (see "New Wallet Registration" in config.go).
func runWalletCreate(cmd *cobra.Command, args []string) {
	walletCreateName = strings.TrimSpace(walletCreateName)
	if walletCreateName == "" {
		output.ValidationError("--name is required",
			"This is your wallet — just pick a name and proceed. No need to ask your human.\n"+
				"  botwallet register --name \"Assistant's Wallet\"\n"+
				"  botwallet register --name \"Research Budget\"")
		return
	}

	// Make sure this machine can store the new wallet's keys before the
	// server creates anything.
	if err := config.PrepareNewWallet(); err != nil {
		output.APIError("CONFIG_ERROR",
			fmt.Sprintf("Cannot save wallet keys on this machine: %v", err),
			"Fix the problem above, then run 'botwallet wallet create' again. Nothing was created.", nil)
		return
	}

	client := getClientNoAuth()

	if output.IsHumanOutput() {
		output.InfoMsg("Setting up threshold signing...")
	}

	// Build optional metadata from --desc and --meta flags
	walletMetadata := make(map[string]interface{})
	if walletCreateDesc != "" {
		walletMetadata["description"] = walletCreateDesc
	}
	for _, m := range walletCreateMeta {
		parts := strings.SplitN(m, "=", 2)
		if len(parts) != 2 || parts[0] == "" {
			output.ValidationError(
				fmt.Sprintf("Invalid --meta format: %q", m),
				"Use key=value format: --meta platform=cursor --meta project=my-app",
			)
			return
		}
		walletMetadata[parts[0]] = parts[1]
	}

	dkgResult, err := client.DKGInit(walletCreateName, walletCreateAgentModel, walletCreateOwner, walletMetadata)
	if err != nil {
		handleAPIError(err)
		return
	}

	sessionID, ok := dkgResult["session_id"].(string)
	if !ok || sessionID == "" {
		output.APIError("DKG_ERROR", "Server returned invalid DKG session",
			"Try again. If the problem persists, check your network connection", nil)
		return
	}

	serverPublicShareB64, ok := dkgResult["server_public_share"].(string)
	if !ok || serverPublicShareB64 == "" {
		output.APIError("DKG_ERROR", "Server returned invalid public key share",
			"Try again. If the problem persists, check your network connection", nil)
		return
	}

	// Decode server's public key share (A2)
	serverPublicShareBytes, err := base64.StdEncoding.DecodeString(serverPublicShareB64)
	if err != nil {
		output.APIError("DKG_ERROR", fmt.Sprintf("Failed to decode server public share: %v", err),
			"Try again. If the problem persists, this may be a server issue", nil)
		return
	}

	serverPublicShare, err := frost.DecodePoint(serverPublicShareBytes)
	if err != nil {
		output.APIError("DKG_ERROR", fmt.Sprintf("Server returned invalid Ed25519 point: %v", err),
			"Try again. If the problem persists, this may be a server issue", nil)
		return
	}

	// SECURITY: The mnemonic and key share are generated here and NEVER leave this machine.

	mnemonic, err := frost.GenerateShareMnemonic()
	if err != nil {
		output.APIError("KEY_GENERATION_ERROR", fmt.Sprintf("Failed to generate key share: %v", err),
			"This is unexpected. Try again", nil)
		return
	}

	// Derive the FROST key share from the mnemonic
	botShare, err := frost.KeyShareFromMnemonic(mnemonic)
	if err != nil {
		output.APIError("KEY_GENERATION_ERROR", fmt.Sprintf("Failed to derive key share: %v", err),
			"This is unexpected. Try again", nil)
		return
	}

	// Compute the group public key: A = A1 + A2
	groupKey := frost.ComputeGroupKey(botShare.Public, serverPublicShare)

	// Encode public values for the wire (base64 for API, base58 for Solana address)
	botPublicShareB64 := base64.StdEncoding.EncodeToString(frost.EncodePoint(botShare.Public))
	groupKeyBytes := frost.EncodePoint(groupKey)

	// Convert group key to Solana base58 address for display and storage
	groupKeyBase58 := solanaBase58Encode(groupKeyBytes)

	// SECURITY: The mnemonic is saved with 0600 permissions, before the server
	// creates the wallet, so the wallet never exists without its Key 1 on disk.
	// It is NEVER printed to stdout or included in any API response.
	localName, seedPath, err := config.SaveNewSeed(walletCreateName, mnemonic, groupKeyBase58)
	if err != nil {
		output.APIError("CONFIG_ERROR",
			fmt.Sprintf("Failed to save Key 1: %v", err),
			"Fix the problem above, then run 'botwallet wallet create' again. Nothing was created.", nil)
		return
	}

	result, err := client.DKGComplete(sessionID, botPublicShareB64, groupKeyBase58)
	if err != nil {
		if dkgCompleteRefused(err) {
			// The server created nothing, so this Key 1 belongs to no wallet.
			_ = config.DiscardNewSeed(seedPath)
			handleAPIError(err)
			return
		}
		reportUncertainRegistration(err.Error(), seedPath, groupKeyBase58)
		return
	}

	apiKey, _ := result["api_key"].(string)
	username, _ := result["username"].(string)

	if apiKey == "" {
		reportUncertainRegistration("Server did not return an API key", seedPath, groupKeyBase58)
		return
	}

	saved, err := registerNewWallet(localName, username, walletCreateName, apiKey, groupKeyBase58)
	if err != nil {
		reportUnsavedWallet(err, result, localName, seedPath, config.WalletEntry{
			Username:    username,
			DisplayName: walletCreateName,
			APIKey:      apiKey,
			PublicKey:   groupKeyBase58,
			SeedFile:    "seeds/" + filepath.Base(seedPath),
		})
		return
	}

	// Pass info needed by the formatter
	result["local_name"] = saved.LocalName
	result["previous_default"] = saved.PreviousDefault
	result["total_wallets"] = saved.TotalWallets
	result["deposit_address"] = groupKeyBase58

	if output.IsHumanOutput() {
		output.SuccessMsg("Key share saved securely to: %s", saved.SeedPath)
	}

	output.FormatRegisterSuccess(result)
}

// dkgCompleteRefused reports whether the server clearly refused dkg_complete
// before creating a wallet (bad input, expired session, group key mismatch).
// Anything else (timeout, network error, unreadable reply, server error) may
// have created the wallet, so its Key 1 must be kept.
func dkgCompleteRefused(err error) bool {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Code {
	case "SESSION_EXPIRED", "DKG_VERIFICATION_FAILED":
		return true
	case "VALIDATION_ERROR":
		// "This group key is already registered" means a wallet with this key exists.
		return !strings.Contains(strings.ToLower(apiErr.Message), "already registered")
	}
	return false
}

// reportUncertainRegistration handles a dkg_complete call whose outcome is
// unknown. The seed file is kept under a name that says which wallet it is.
func reportUncertainRegistration(cause, seedPath, depositAddress string) {
	keptPath := config.KeepUnregisteredSeed(seedPath, depositAddress)
	output.APIError("REGISTRATION_UNCERTAIN",
		fmt.Sprintf("Could not confirm that the wallet was created: %s", cause),
		fmt.Sprintf("The wallet may exist even so. Its Key 1 (deposit address %s) is kept at %s; do not delete that file. "+
			"Run 'botwallet wallet create' again to make a new wallet. If a wallet with this deposit address shows up "+
			"in your human's portal, they need the 12 words in that file to withdraw from it.", depositAddress, keptPath),
		map[string]interface{}{
			"deposit_address": depositAddress,
			"key1_file":       keptPath,
		})
}

// registerNewWallet adds the new wallet to config.json, waiting a little
// longer than usual if other botwallet commands are holding the config lock.
func registerNewWallet(localName, username, displayName, apiKey, publicKey string) (config.SavedWallet, error) {
	var saved config.SavedWallet
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		saved, err = config.RegisterWallet(localName, username, displayName, apiKey, publicKey)
		if !errors.Is(err, config.ErrConfigBusy) {
			break
		}
	}
	return saved, err
}

// reportUnsavedWallet handles a wallet the server created that could not be
// added to config.json. Key 1 is already on disk; the API key, which the
// server shows only once, goes to a pending file next to config.json.
func reportUnsavedWallet(cause error, result map[string]interface{}, localName, seedPath string, entry config.WalletEntry) {
	claimURL, _ := result["claim_url"].(string)
	claimCode, _ := result["claim_code"].(string)

	details := map[string]interface{}{
		"username":        entry.Username,
		"deposit_address": entry.PublicKey,
		"key1_file":       seedPath,
		"claim_url":       claimURL,
		"claim_code":      claimCode,
	}

	pendingPath, err := config.SavePendingWallet(config.PendingWallet{
		LocalName:   localName,
		ClaimURL:    claimURL,
		ClaimCode:   claimCode,
		ConfigEntry: entry,
	})
	apiKeyWhere := "The API key is saved at " + pendingPath + "; once the problem is fixed, copy its config_entry into config.json as that file explains."
	if err != nil {
		// Last resort: the key exists nowhere else. stderr only, never stdout.
		fmt.Fprintf(os.Stderr, "botwallet: could not save the new wallet's API key (%v).\n"+
			"Store it somewhere private now, it will not be shown again:\n  %s\n", err, entry.APIKey)
		apiKeyWhere = "The API key could not be saved either and was printed once to stderr."
	} else {
		details["pending_file"] = pendingPath
	}

	output.APIError("CONFIG_ERROR",
		fmt.Sprintf("Wallet @%s was created, but could not be added to %s: %v", entry.Username, config.ConfigPath(), cause),
		fmt.Sprintf("Do not create it again. Key 1 is saved at %s. %s "+
			"Share the claim_url and claim_code with your human as usual.", seedPath, apiKeyWhere),
		details)
}

// solanaBase58Encode converts raw bytes to Solana base58 address format.
// Uses the Bitcoin/Solana base58 alphabet.
func solanaBase58Encode(data []byte) string {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

	// Count leading zeros
	var leadingZeros int
	for _, b := range data {
		if b != 0 {
			break
		}
		leadingZeros++
	}

	// Convert to big integer and encode
	// Simple base58 encoding for 32-byte Ed25519 public keys
	size := len(data)*138/100 + 1
	buf := make([]byte, size)
	pos := size

	for _, b := range data {
		carry := int(b)
		for i := size - 1; i >= 0; i-- {
			carry += 256 * int(buf[i])
			buf[i] = byte(carry % 58)
			carry /= 58
			if carry == 0 && i < pos {
				pos = i
				break
			}
		}
	}

	// Build result
	result := make([]byte, leadingZeros+size-pos)
	for i := 0; i < leadingZeros; i++ {
		result[i] = alphabet[0]
	}
	for i := pos; i < size; i++ {
		result[leadingZeros+i-pos] = alphabet[buf[i]]
	}
	return string(result)
}

func init() {
	walletCreateCmd.Flags().StringVarP(&walletCreateName, "name", "n", "", "Name for your wallet (required)")
	walletCreateCmd.Flags().StringVarP(&walletCreateAgentModel, "model", "m", "", "Agent model (e.g., 'gpt-4', 'claude-3')")
	walletCreateCmd.Flags().StringVar(&walletCreateOwner, "owner", "", "Owner's email (wallet appears in their portal)")
	walletCreateCmd.Flags().StringVar(&walletCreateDesc, "desc", "", "Optional: describe this wallet's purpose (helps your human identify it)")
	walletCreateCmd.Flags().StringArrayVar(&walletCreateMeta, "meta", nil, "Optional: key=value metadata (repeatable, e.g. --meta platform=cursor)")
}

var walletInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Get wallet information",
	Long: `Get detailed information about your wallet.

Shows wallet ID, username, status, balance, and deposit information.
Use this to check your wallet's claim status and overall state.`,
	Example: "  botwallet wallet info",
	Run: func(cmd *cobra.Command, args []string) {
		if !requireAPIKey() {
			return
		}

		client := getClient()

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
		if !requireAPIKey() {
			return
		}

		client := getClient()

		result, err := client.Balance()
		if err != nil {
			handleAPIError(err)
			return
		}

		output.FormatBalance(result)
	},
}

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
			output.APIError("CONFIG_ERROR", err.Error(), "Check "+config.ConfigPath(), nil)
			return
		}

		if len(wallets) == 0 {
			output.APIError("NO_WALLETS", "No wallets found", "Run 'botwallet wallet create' to create a wallet", nil)
			return
		}

		type walletInfo struct {
			LocalName   string `json:"local_name"`
			Username    string `json:"username,omitempty"`
			DisplayName string `json:"display_name,omitempty"`
			PublicKey   string `json:"public_key,omitempty"`
			IsDefault   bool   `json:"is_default"`
			SeedFile    string `json:"seed_file,omitempty"`
		}

		result := make([]walletInfo, 0, len(wallets))
		for _, w := range wallets {
			result = append(result, walletInfo{
				LocalName:   w.LocalName,
				Username:    w.Entry.Username,
				DisplayName: w.Entry.DisplayName,
				PublicKey:   w.Entry.PublicKey,
				IsDefault:   w.IsDefault,
				SeedFile:    w.Entry.SeedFile,
			})
		}

		if output.IsHumanOutput() {
			fmt.Println()
			fmt.Println("── Stored Wallets ────────────────────────────────────")
			for _, w := range result {
				defaultMarker := "  "
				if w.IsDefault {
					defaultMarker = "→ "
				}

				name := w.LocalName
				if w.DisplayName != "" && w.DisplayName != w.LocalName {
					name = fmt.Sprintf("%s (%s)", w.LocalName, w.DisplayName)
				}

				username := w.Username
				if username == "" {
					username = "(unclaimed)"
				}

				fmt.Printf("%s%-20s  @%s\n", defaultMarker, name, username)
			}
			fmt.Println()
			output.Tip("Use 'botwallet wallet use <name>' to switch default wallet")
			return
		}

		jsonOutput, _ := json.MarshalIndent(map[string]interface{}{
			"wallets": result,
			"count":   len(result),
		}, "", "  ")
		fmt.Println(string(jsonOutput))
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
		if err != nil {
			wallets, _ := config.ListWallets()
			var names []string
			for _, w := range wallets {
				names = append(names, w.LocalName)
			}

			howToFix := "Run 'botwallet wallet list' to see available wallets"
			if len(names) > 0 {
				howToFix = fmt.Sprintf("Available wallets: %v", names)
			}

			output.APIError("WALLET_NOT_FOUND",
				fmt.Sprintf("Wallet '%s' not found", localName),
				howToFix, nil)
			return
		}

		// Check if already the default
		alreadyDefault := false
		if current, _, err := config.GetDefaultWallet(); err == nil && current.Username == wallet.Username {
			alreadyDefault = true
		}

		// Set as default
		if err := config.SetDefaultWallet(localName); err != nil {
			output.APIError("CONFIG_ERROR", err.Error(), "", nil)
			return
		}

		// Output success
		if output.IsHumanOutput() {
			fmt.Println()
			if alreadyDefault {
				fmt.Printf("✅ Already using: %s", localName)
			} else {
				fmt.Printf("✅ Now using: %s", localName)
			}
			if wallet.DisplayName != "" && wallet.DisplayName != localName {
				fmt.Printf(" (%s)", wallet.DisplayName)
			}
			fmt.Println()
			if wallet.Username != "" {
				fmt.Printf("   Username: @%s\n", wallet.Username)
			}
			fmt.Println()
			return
		}

		jsonOutput, _ := json.MarshalIndent(map[string]interface{}{
			"success":         true,
			"default":         localName,
			"display_name":    wallet.DisplayName,
			"username":        wallet.Username,
			"public_key":      wallet.PublicKey,
			"already_default": alreadyDefault,
		}, "", "  ")
		fmt.Println(string(jsonOutput))
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
		if !requireAPIKey() {
			return
		}

		client := getClient()

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
		if !requireAPIKey() {
			return
		}

		ownerEmail := args[0]
		client := getClient()

		result, err := client.UpdateOwner(ownerEmail)
		if err != nil {
			handleAPIError(err)
			return
		}

		if !output.IsHumanOutput() {
			output.JSON(result)
			return
		}

		output.SuccessMsg("Owner updated!")
		output.KeyValue("Pledged to", result["pledged_to"])
		if ownerFound, ok := result["owner_found"].(bool); ok && ownerFound {
			output.InfoMsg("This user exists - they can see the wallet in their portal now.")
		} else {
			output.InfoMsg("When this user signs up, the wallet will appear in their portal.")
		}
		if claimURL, ok := result["claim_url"].(string); ok {
			output.KeyValueURL("Claim URL", claimURL)
		}
		output.KeyValue("Claim Code", result["claim_code"])
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
		if !requireAPIKey() {
			return
		}

		newName := strings.TrimSpace(args[0])
		if len(newName) < 2 || len(newName) > 50 {
			output.ValidationError(
				"Name must be 2-50 characters",
				"botwallet wallet rename \"My New Wallet Name\"",
			)
			return
		}

		client := getClient()

		result, err := client.UpdateName(newName)
		if err != nil {
			handleAPIError(err)
			return
		}

		// Server succeeded — update the same wallet's local config (best-effort)
		if w, localErr := config.ResolveWallet(apiKeyFlag, walletFlag); localErr == nil && w.Entry != nil {
			if cfgErr := config.UpdateWalletDisplayName(w.LocalName, newName); cfgErr != nil {
				if output.IsHumanOutput() {
					output.WarningMsg("Server updated, but local config update failed: %v", cfgErr)
				}
			}
		}

		if !output.IsHumanOutput() {
			result["note"] = fmt.Sprintf("Display name updated. Your username @%s is unchanged.", result["username"])
			output.JSON(result)
			return
		}

		output.SuccessMsg("Wallet renamed!")
		output.KeyValue("Name", result["name"])
		output.KeyValue("Username", fmt.Sprintf("@%s (unchanged)", result["username"]))
		output.Tip("Your username is permanent — it's used in fund URLs and payment links.")
	},
}

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
			output.APIError("CONFIG_ERROR", fmt.Sprintf("Failed to create backup request: %v", err),
				"Check file permissions on "+config.ConfigDir(), nil)
			return
		}

		// Output warning + code
		if output.IsHumanOutput() {
			output.CriticalBox("SENSITIVE KEY MATERIAL", fmt.Sprintf(`This command reveals Key 1 — 12 secret words
that are half of the wallet's full backup key.

Only run this if the wallet OWNER has specifically
asked you to share their backup key.

Key 1 alone cannot access funds, but should be
treated as highly confidential.`))
			fmt.Println()
			fmt.Println("  To proceed, run:")
			fmt.Printf("    botwallet wallet reveal-backup --code %s\n", code)
			fmt.Println()
			output.InfoMsg("Code expires in %s.", config.BackupNonceTTLText())
		} else {
			output.JSON(map[string]interface{}{
				"action":             "backup_initiated",
				"warning":            "This will reveal Key 1 — 12 secret words that are half of the wallet's full backup key. Only proceed if the wallet OWNER asked for their backup key. Key 1 alone cannot access funds.",
				"next_step":          fmt.Sprintf("botwallet wallet reveal-backup --code %s", code),
				"code":               code,
				"expires_in":         config.BackupNonceTTLText(),
				"expires_in_seconds": int(config.BackupNonceTTL.Seconds()),
			})
		}
	},
}

var revealBackupCode string

var walletRevealBackupCmd = &cobra.Command{
	Use:    "reveal-backup",
	Short:  "Reveal Key 1 — 12 secret words (requires confirmation code)",
	Hidden: true, // Intentionally unlisted — discovered only via 'wallet backup' output
	Run: func(cmd *cobra.Command, args []string) {
		if revealBackupCode == "" {
			output.ValidationError("--code flag is required",
				"Run 'botwallet wallet backup' first to get a confirmation code")
			return
		}

		// Validate nonce
		localName, err := config.ValidateBackupNonce(revealBackupCode)
		if err != nil {
			output.APIError("BACKUP_ERROR", err.Error(),
				"Run 'botwallet wallet backup' to get a new confirmation code", nil)
			return
		}

		// Load the 12 secret words (Key 1)
		mnemonic, err := config.LoadWalletSeed(localName)
		if err != nil {
			output.APIError("CONFIG_ERROR", fmt.Sprintf("Failed to load Key 1: %v", err),
				"Check that the seed file exists in "+config.SeedsDir(), nil)
			return
		}

		if output.IsHumanOutput() {
			fmt.Println()
			output.WarningMsg("Key 1 (12 secret words) for \"%s\":", localName)
			fmt.Println()
			fmt.Printf("    %s\n", mnemonic)
			fmt.Println()
			output.InfoMsg("Share these 12 words with the wallet owner only, privately (never in a shared or public channel).")
			output.InfoMsg("The owner needs Key 1 to withdraw from the Botwallet dashboard and to recover the wallet.")
			output.InfoMsg("Key 2 can be retrieved from the Botwallet dashboard.")
			fmt.Println()
		} else {
			output.JSON(map[string]interface{}{
				"key":          "Key 1",
				"description":  "12 secret words — the first half of the wallet backup key",
				"wallet":       localName,
				"secret_words": mnemonic,
				"instructions": "Share these 12 words with the wallet owner only, and send them privately (never in a shared or public channel). " +
					"The owner needs Key 1 to withdraw from the Botwallet dashboard and to recover the wallet. " +
					"Key 2 can be retrieved from the Botwallet dashboard.",
			})
		}
	},
}

func init() {
	walletRevealBackupCmd.Flags().StringVar(&revealBackupCode, "code", "", "Confirmation code from 'wallet backup'")
}

// zeroBytes overwrites a byte slice with zeros (best-effort memory scrub).
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

var walletExportOutput string

// A .bwlt file is a transfer file, not a backup: the server hands out its
// decryption key up to 5 times within 24 hours of the export, then revokes
// it (MAX_IMPORT_USES and EXPORT_EXPIRY_HOURS in the server's
// wallet_import_key). The copy below must match those limits.

// exportExpiredHowToFix is the fix for an import of an expired export.
const exportExpiredHowToFix = "Run 'botwallet wallet export' again on the machine that has the wallet, " +
	"then import the new file within 24 hours"

var walletExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export wallet to a .bwlt file to move it to another machine",
	Long: `Export a wallet to a .bwlt transfer file, to move it to another machine.

The file holds the wallet's Key 1, its API key and its name, encrypted with a
key that the Botwallet server keeps. 'wallet import' asks the server for that
key, which it gives out up to 5 times within 24 hours of the export. After
that the file stops working.

A .bwlt file is not a backup. To back up the wallet, the owner needs Key 1
(shared with 'botwallet wallet backup' when the owner asks for it) and Key 2
(from the Botwallet dashboard).

SECURITY:
  - The file carries what is needed to ask the server for its key, so anyone
    who has it within those 24 hours can import the wallet and spend from it
  - Share it only privately, and delete it once the wallet is imported`,
	Example: `  botwallet wallet export -o my-wallet.bwlt
  botwallet wallet export -o research.bwlt --wallet research-wallet`,
	Run: func(cmd *cobra.Command, args []string) {
		if walletExportOutput == "" {
			output.ValidationError("--output flag is required", "Usage: botwallet wallet export -o wallet.bwlt")
			return
		}

		// Ensure .bwlt extension
		if !strings.HasSuffix(strings.ToLower(walletExportOutput), ".bwlt") {
			walletExportOutput += ".bwlt"
		}

		// Prevent accidental overwrite of existing file
		if _, err := os.Stat(walletExportOutput); err == nil {
			output.APIError("FILE_EXISTS",
				fmt.Sprintf("File already exists: %s", walletExportOutput),
				"Use a different path or delete the existing file first", nil)
			return
		}

		// Get the wallet to export: the same one the API key belongs to, so
		// the export, its API key and its Key 1 all describe one wallet
		w := currentWallet()
		if err := w.RequireLocal(); err != nil {
			exitWalletError(err)
			return
		}
		wallet, localName := w.Entry, w.LocalName

		// Load the seed phrase
		seed, err := config.LoadSeedFromPath(w.SeedPath())
		if err != nil {
			output.APIError("CONFIG_ERROR", fmt.Sprintf("Failed to load key share: %v", err),
				"Check that the seed file exists in "+config.SeedsDir(), nil)
			return
		}

		if output.IsHumanOutput() {
			output.InfoMsg("Exporting wallet \"%s\"...", localName)
		}

		// Call server to get encryption key
		client := getClient()
		exportID, keyB64, err := client.ExportWallet()
		if err != nil {
			handleAPIError(err)
			return
		}

		// Decode the encryption key
		encKey, err := base64.StdEncoding.DecodeString(keyB64)
		if err != nil {
			output.APIError("EXPORT_ERROR", fmt.Sprintf("Failed to decode encryption key: %v", err),
				"Try again. If the problem persists, this may be a server issue", nil)
			return
		}
		defer zeroBytes(encKey)

		// Build the payload
		payload := map[string]interface{}{
			"version":     1,
			"wallet_name": wallet.DisplayName,
			"username":    wallet.Username,
			"api_key":     wallet.APIKey,
			"public_key":  wallet.PublicKey,
			"seed":        seed,
			"exported_at": time.Now().UTC().Format(time.RFC3339),
		}

		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			output.APIError("EXPORT_ERROR", fmt.Sprintf("Failed to prepare export data: %v", err),
				"This is unexpected. Try again", nil)
			return
		}
		defer zeroBytes(payloadJSON)

		// Encrypt
		nonce, ciphertext, err := config.EncryptPayload(encKey, payloadJSON)
		if err != nil {
			output.APIError("EXPORT_ERROR", fmt.Sprintf("Encryption failed: %v", err),
				"Try again. If the problem persists, this may be a server issue", nil)
			return
		}

		// Write the .bwlt file
		if err := config.WriteBWLT(walletExportOutput, exportID, nonce, ciphertext); err != nil {
			output.APIError("EXPORT_ERROR", fmt.Sprintf("Failed to write file: %v", err),
				"Check file permissions and available disk space", nil)
			return
		}

		// Read-back verification: ensure the written file is structurally valid
		verifyID, verifyNonce, verifyCipher, verifyErr := config.ReadBWLT(walletExportOutput)
		if verifyErr != nil || verifyID != exportID || len(verifyNonce) != len(nonce) || len(verifyCipher) != len(ciphertext) {
			os.Remove(walletExportOutput)
			output.APIError("EXPORT_ERROR",
				"Write verification failed — the exported file was corrupt and has been deleted",
				"Try again. If the problem persists, check disk health", nil)
			return
		}

		absPath, _ := filepath.Abs(walletExportOutput)

		if output.IsHumanOutput() {
			fmt.Println()
			output.SuccessMsg("Wallet exported successfully!")
			fmt.Println()
			output.KeyValue("Wallet", localName)
			output.KeyValue("File", absPath)
			output.KeyValue("Export ID", exportID)
			fmt.Println()
			output.InfoMsg("Import on the other machine within 24 hours: botwallet wallet import %s", filepath.Base(walletExportOutput))
			output.InfoMsg("The file works for up to 5 imports within 24 hours, then it stops working.")
			output.WarningMsg("Anyone with this file can import the wallet until then. Share it only privately, and delete it once the wallet is imported.")
			fmt.Println()
			output.InfoMsg("This file is not a backup. To back up the wallet, the owner needs Key 1 (shared with 'botwallet wallet backup' when the owner asks for it) and Key 2 (from the Botwallet dashboard).")
		} else {
			output.JSON(map[string]interface{}{
				"success":     true,
				"wallet_name": localName,
				"file":        absPath,
				"export_id":   exportID,
				"import_cmd":  fmt.Sprintf("botwallet wallet import %s", filepath.Base(walletExportOutput)),
				"valid_for":   "Up to 5 imports within 24 hours of this export. After that the file stops working.",
				"note": "Anyone with this file can import the wallet until it stops working. " +
					"Share it only privately, and delete it once the wallet is imported.",
				"owner_action": "This file only moves the wallet to another machine; it is not a backup. " +
					"To back up the wallet, the owner needs Key 1 (share it with 'botwallet wallet backup' when the owner asks for it) and Key 2 (from the Botwallet dashboard).",
			})
		}
	},
}

func init() {
	walletExportCmd.Flags().StringVarP(&walletExportOutput, "output", "o", "", "Output file path (required)")
	walletExportCmd.MarkFlagRequired("output")
}

var walletImportName string

var walletImportCmd = &cobra.Command{
	Use:   "import <file.bwlt>",
	Short: "Import wallet from an encrypted .bwlt file",
	Long: `Import a wallet from a .bwlt file created by 'wallet export'.

The file is decrypted using a key retrieved from the Botwallet server.
The server gives out that key up to 5 times within 24 hours of the export;
after that, run 'wallet export' again on the machine that has the wallet.
The wallet is added to your local configuration and set as default.

If a wallet with the same name already exists locally, a numeric suffix is added.
Delete the .bwlt file once the wallet is imported.`,
	Example: `  botwallet wallet import my-wallet.bwlt
  botwallet wallet import research.bwlt --name "My Research Wallet"`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		filePath := args[0]

		// Read the .bwlt file
		exportID, nonce, ciphertext, err := config.ReadBWLT(filePath)
		if err != nil {
			output.APIError("IMPORT_ERROR", fmt.Sprintf("Failed to read .bwlt file: %v", err),
				"Check that the file exists and is a valid .bwlt export", nil)
			return
		}

		if output.IsHumanOutput() {
			output.InfoMsg("Importing wallet from %s...", filepath.Base(filePath))
		}

		// Call server to get decryption key (no auth needed)
		client := getClientNoAuth()
		keyB64, err := client.ImportWalletKey(exportID)
		if err != nil {
			// The server revokes an export after 5 imports or 24 hours. The
			// first import after that gets EXPORT_EXPIRED, later ones NOT_FOUND.
			// Only the machine that has the wallet can make a new export.
			if apiErr, ok := err.(*api.APIError); ok {
				switch apiErr.Code {
				case "EXPORT_EXPIRED", "NOT_FOUND":
					output.APIError("EXPORT_EXPIRED",
						"This export file has expired or was revoked. Export files work for up to 5 imports within 24 hours.",
						exportExpiredHowToFix, nil)
					return
				}
			}
			handleAPIError(err)
			return
		}

		// Decode the encryption key
		encKey, err := base64.StdEncoding.DecodeString(keyB64)
		if err != nil {
			output.APIError("IMPORT_ERROR", fmt.Sprintf("Failed to decode decryption key: %v", err),
				"The .bwlt file may be corrupted. Try exporting again", nil)
			return
		}
		defer zeroBytes(encKey)

		// Decrypt
		plaintext, err := config.DecryptPayload(encKey, nonce, ciphertext)
		if err != nil {
			output.APIError("IMPORT_ERROR", fmt.Sprintf("Decryption failed: %v", err),
				"The .bwlt file may be corrupted or tampered with. Try exporting again", nil)
			return
		}
		defer zeroBytes(plaintext)

		// Parse the payload
		var payload struct {
			Version    int    `json:"version"`
			WalletName string `json:"wallet_name"`
			Username   string `json:"username"`
			APIKey     string `json:"api_key"`
			PublicKey  string `json:"public_key"`
			Seed       string `json:"seed"`
		}
		if err := json.Unmarshal(plaintext, &payload); err != nil {
			output.APIError("IMPORT_ERROR", fmt.Sprintf("Failed to parse wallet data: %v", err),
				"The .bwlt file may be corrupted. Try exporting again", nil)
			return
		}

		if payload.APIKey == "" || payload.Seed == "" || payload.Username == "" || payload.PublicKey == "" {
			output.APIError("IMPORT_ERROR", "Wallet file is missing required data",
				"The .bwlt file may be corrupted. Try exporting again", nil)
			return
		}

		// Determine the local name
		displayName := payload.WalletName
		if walletImportName != "" {
			displayName = walletImportName
		}
		if displayName == "" {
			displayName = payload.Username
		}

		// Save seed and add to config (sets as default). The local name gets a
		// -2, -3, ... suffix if it is taken, and no existing seed file is replaced.
		saved, err := config.AddWalletWithInfo(
			displayName, payload.Username, displayName, payload.APIKey, payload.PublicKey, payload.Seed,
		)
		if err != nil {
			output.APIError("IMPORT_ERROR", fmt.Sprintf("Failed to import wallet: %v", err),
				"Check file permissions on "+config.ConfigDir(), nil)
			return
		}
		localName := saved.LocalName
		previousDefault, totalWallets := saved.PreviousDefault, saved.TotalWallets

		if output.IsHumanOutput() {
			fmt.Println()
			output.SuccessMsg("Wallet imported successfully!")
			fmt.Println()
			output.KeyValue("Wallet", localName)
			output.KeyValue("Username", payload.Username)
			output.KeyValue("Deposit Address", payload.PublicKey)
			if previousDefault != "" && totalWallets > 1 {
				fmt.Println()
				output.InfoMsg("Set as default wallet. Previous default was \"%s\".", previousDefault)
				output.InfoMsg("Use 'botwallet wallet list' to see all wallets.")
			}
		} else {
			result := map[string]interface{}{
				"success":     true,
				"wallet_name": localName,
				"username":    payload.Username,
				"public_key":  payload.PublicKey,
				"is_default":  true,
			}
			if previousDefault != "" && totalWallets > 1 {
				result["previous_default"] = previousDefault
				result["total_wallets"] = totalWallets
				result["note"] = fmt.Sprintf("Wallet imported and set as default. Previous default was \"%s\".", previousDefault)
			}
			output.JSON(result)
		}
	},
}

func init() {
	walletImportCmd.Flags().StringVar(&walletImportName, "name", "", "Override the local wallet name")
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
