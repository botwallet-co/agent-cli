package cmd

import (
	"encoding/base64"
	"encoding/json"
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
  backup    Back up Key 1 (your 12 secret words)
  export    Export wallet to an encrypted .bwlt file
  import    Import wallet from a .bwlt file`,
	Example: `  botwallet wallet create --name "Research Wallet" --owner human@example.com
  botwallet wallet info
  botwallet wallet balance
  botwallet wallet list
  botwallet wallet use my-other-wallet
  botwallet wallet deposit
  botwallet wallet owner new-owner@example.com
  botwallet wallet backup
  botwallet wallet export -o wallet.bwlt
  botwallet wallet import wallet.bwlt`,
}

var (
	walletCreateName       string
	walletCreateAgentModel string
	walletCreateOwner      string
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

⚠️  CRITICAL: The API key cannot be recovered if lost.
TIP: Name it so your human recognizes it — e.g., "Orion's Wallet" for a general wallet, or "API Allowance Wallet" for a specific purpose.

The wallet will be in "unclaimed" status until a human owner claims it.
Use --owner to pledge the wallet to a specific email - they'll see it in their portal.`,
	Example: `  botwallet wallet create --name "Orion's Wallet"
  botwallet wallet create --name "Research Wallet" --owner human@example.com
  botwallet wallet create --name "Sarah's Wallet" --owner sarah@example.com`,
	Run: runWalletCreate,
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
func runWalletCreate(cmd *cobra.Command, args []string) {
	walletCreateName = strings.TrimSpace(walletCreateName)
	if walletCreateName == "" {
		output.ValidationError("--name flag is required", "Usage: botwallet register --name \"YourBotName\"")
		return
	}

	// Generate local name for multi-wallet storage
	localName := config.GenerateLocalName(walletCreateName)

	// Check if wallet already exists locally (prevent accidental overwrite)
	if _, err := config.GetWallet(localName); err == nil {
		output.APIError("WALLET_EXISTS",
			fmt.Sprintf("Wallet '%s' already exists locally", localName),
			"Use a different name or run 'botwallet wallet list' to see existing wallets", nil)
		return
	}

	client := getClientNoAuth()

	if output.IsHumanOutput() {
		output.InfoMsg("Setting up threshold signing...")
	}

	dkgResult, err := client.DKGInit(walletCreateName, walletCreateAgentModel, walletCreateOwner)
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

	result, err := client.DKGComplete(sessionID, botPublicShareB64, groupKeyBase58)
	if err != nil {
		handleAPIError(err)
		return
	}

	apiKey, _ := result["api_key"].(string)
	username, _ := result["username"].(string)

	if apiKey == "" {
		output.APIError("REGISTRATION_FAILED", "Server did not return an API key",
			"Registration may have failed. Try again", nil)
		return
	}

	// SECURITY: The mnemonic is saved with 0600 permissions.
	// It is NEVER printed to stdout or included in any API response.
	previousDefault, totalWallets, err := config.AddWalletWithInfo(
		localName, username, walletCreateName, apiKey, groupKeyBase58, mnemonic,
	)
	if err != nil {
		output.APIError("CONFIG_ERROR",
			fmt.Sprintf("Failed to save wallet locally: %v", err),
			"Re-run 'botwallet wallet create' to try again",
			map[string]interface{}{"api_key": apiKey})
		return
	}

	// Pass info needed by the formatter
	result["local_name"] = localName
	result["previous_default"] = previousDefault
	result["total_wallets"] = totalWallets
	result["deposit_address"] = groupKeyBase58

	if output.IsHumanOutput() {
		output.SuccessMsg("Key share saved securely to: %s", config.SeedPath(localName))
	}

	output.FormatRegisterSuccess(result)
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
	walletCreateCmd.MarkFlagRequired("name")
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
			output.APIError("CONFIG_ERROR", err.Error(), "Check ~/.botwallet/config.json", nil)
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

var walletBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Start the backup process for your wallet's Key 1 (12 secret words)",
	Long: `Start the Key 1 backup process.

This shows a warning and generates a one-time confirmation code.
You must then run 'wallet reveal-backup --code <code>' within 30 seconds
to reveal the 12 secret words (Key 1).

This two-step process prevents accidental exposure of sensitive key material.`,
	Example: "  botwallet wallet backup",
	Run: func(cmd *cobra.Command, args []string) {
		// Get current wallet
		_, localName, err := config.GetCurrentWallet(walletFlag)
		if err != nil {
			output.APIError("NO_WALLET", fmt.Sprintf("No wallet configured: %v", err),
				"Run 'botwallet wallet create' first, or use --wallet flag", nil)
			return
		}

		// Generate one-time code
		code := config.GenerateBackupCode()

		// Save nonce
		if err := config.WriteBackupNonce(code, localName); err != nil {
			output.APIError("CONFIG_ERROR", fmt.Sprintf("Failed to create backup request: %v", err),
				"Check file permissions on ~/.botwallet/", nil)
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
			output.InfoMsg("Code expires in 30 seconds.")
		} else {
			output.JSON(map[string]interface{}{
				"action":     "backup_initiated",
				"warning":    "This will reveal Key 1 — 12 secret words that are half of the wallet's full backup key. Only proceed if the wallet OWNER asked for their backup key. Key 1 alone cannot access funds.",
				"next_step":  fmt.Sprintf("botwallet wallet reveal-backup --code %s", code),
				"code":       code,
				"expires_in": "30 seconds",
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
		mnemonic, err := config.LoadSeed(localName)
		if err != nil {
			output.APIError("CONFIG_ERROR", fmt.Sprintf("Failed to load Key 1: %v", err),
				"Check that the seed file exists at ~/.botwallet/seeds/", nil)
			return
		}

		if output.IsHumanOutput() {
			fmt.Println()
			output.WarningMsg("Key 1 (12 secret words) for \"%s\":", localName)
			fmt.Println()
			fmt.Printf("    %s\n", mnemonic)
			fmt.Println()
			output.InfoMsg("Share these 12 words with the wallet owner.")
			output.InfoMsg("Key 2 can be retrieved from the Botwallet dashboard.")
			fmt.Println()
		} else {
			output.JSON(map[string]interface{}{
				"key":          "Key 1",
				"description":  "12 secret words — the first half of the wallet backup key",
				"wallet":       localName,
				"secret_words": mnemonic,
				"instructions": "Share these 12 words with the wallet owner. Key 2 can be retrieved from the Botwallet dashboard.",
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

var walletExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export wallet to an encrypted .bwlt file",
	Long: `Export a wallet to a portable, encrypted .bwlt file.

The exported file contains the wallet's key share (S1), API key, and metadata,
encrypted with a server-held AES-256 key. The file is useless without the server.

Use cases:
  - Move a wallet to another machine
  - Create an encrypted backup of your wallet
  - Share a wallet with another agent

The .bwlt file can be imported on any machine using 'wallet import'.
It can be reused (import on multiple machines) and does not expire.

SECURITY:
  - The file is AES-256-GCM encrypted — not plaintext
  - The decryption key is held by the Botwallet server
  - If compromised, the owner can revoke the export from the dashboard
  - Treat .bwlt files like private keys — keep them secure`,
	Example: `  botwallet wallet export -o my-wallet.bwlt
  botwallet wallet export -o ~/backups/research.bwlt --wallet research-wallet`,
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

		// Get the wallet to export
		wallet, localName, err := config.GetCurrentWallet(walletFlag)
		if err != nil {
			output.APIError("NO_WALLET", fmt.Sprintf("No wallet configured: %v", err),
				"Run 'botwallet wallet create' first, or use --wallet flag", nil)
			return
		}

		// Load the seed phrase
		seed, err := config.LoadSeed(localName)
		if err != nil {
			output.APIError("CONFIG_ERROR", fmt.Sprintf("Failed to load key share: %v", err),
				"Check that the seed file exists at ~/.botwallet/seeds/", nil)
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
			output.WarningMsg("Keep this file secure — it contains your encrypted wallet.")
			output.InfoMsg("Import on another machine: botwallet wallet import %s", filepath.Base(walletExportOutput))
			fmt.Println()
			output.InfoMsg("Share this .bwlt file with the wallet owner for safekeeping.")
			output.InfoMsg("The owner should also back up Key 2 from the Botwallet dashboard if not already done.")
		} else {
			output.JSON(map[string]interface{}{
				"success":     true,
				"wallet_name": localName,
				"file":        absPath,
				"export_id":   exportID,
				"import_cmd":  fmt.Sprintf("botwallet wallet import %s", filepath.Base(walletExportOutput)),
				"owner_action": "Share this .bwlt file with the wallet owner for safekeeping. " +
					"The owner should also back up Key 2 from the Botwallet dashboard if not already done.",
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
The wallet is added to your local configuration and set as default.

If a wallet with the same name already exists locally, a numeric suffix is added.`,
	Example: `  botwallet wallet import my-wallet.bwlt
  botwallet wallet import ~/backups/research.bwlt --name "My Research Wallet"`,
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
			if apiErr, ok := err.(*api.APIError); ok {
				if apiErr.Code == "NOT_FOUND" {
					output.APIError("IMPORT_ERROR",
						"This export has been revoked or is no longer valid",
						"Ask the wallet owner to create a new export", nil)
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

		localName := config.GenerateLocalName(displayName)

		// Save seed and add to config (sets as default)
		previousDefault, totalWallets, err := config.AddWalletWithInfo(
			localName, payload.Username, displayName, payload.APIKey, payload.PublicKey, payload.Seed,
		)
		if err != nil {
			output.APIError("IMPORT_ERROR", fmt.Sprintf("Failed to import wallet: %v", err),
				"Check file permissions on ~/.botwallet/", nil)
			return
		}

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
	walletCmd.AddCommand(walletBackupCmd)
	walletCmd.AddCommand(walletRevealBackupCmd)
	walletCmd.AddCommand(walletExportCmd)
	walletCmd.AddCommand(walletImportCmd)
}
