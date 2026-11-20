package cmd

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/output"
	"github.com/botwallet-co/agent-cli/solana"
	"github.com/botwallet-co/agent-cli/solana/frost"
)

// walletCreateOptions are the flags of wallet create and register.
type walletCreateOptions struct {
	name       string
	agentModel string
	owner      string
	desc       string
	meta       []string
}

// walletCreateOpts holds the flags of the one of the two commands that runs.
var walletCreateOpts walletCreateOptions

// newWalletCreateCmd builds 'wallet create' and its alias 'register'. They
// differ only in their help, so a flag added here reaches both.
func newWalletCreateCmd(use, short, long, example string) *cobra.Command {
	c := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    long,
		Example: example,
		Args:    walletCreateArgs,
		Run:     runWalletCreate,
	}
	c.Flags().StringVarP(&walletCreateOpts.name, "name", "n", "", "Name for your wallet (required)")
	c.Flags().StringVarP(&walletCreateOpts.agentModel, "model", "m", "", "Agent model (e.g., 'gpt-4', 'claude-3')")
	c.Flags().StringVar(&walletCreateOpts.owner, "owner", "", "Owner's email (wallet appears in their portal)")
	c.Flags().StringVar(&walletCreateOpts.desc, "desc", "", "Optional: describe this wallet's purpose (helps your human identify it)")
	c.Flags().StringArrayVar(&walletCreateOpts.meta, "meta", nil, "Optional: key=value metadata (repeatable, e.g. --meta platform=cursor)")
	return c
}

// walletNamingHelp ends the help of both commands.
const walletNamingHelp = `Naming (--name is required):
  This is your wallet — just pick a name and proceed. No need to ask your
  human; it keeps the experience seamless. Use your own name, a creative name,
  or describe the wallet's purpose.`

var walletCreateCmd = newWalletCreateCmd("create", "Create a new Botwallet", `Create a new Botwallet for your AI agent.

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

`+walletNamingHelp, `  botwallet wallet create --name "Assistant's Wallet"
  botwallet wallet create --name "Research Budget" --owner human@example.com
  botwallet wallet create --name "x402 APIs Allowance"`)

var registerCmd = newWalletCreateCmd("register", "Create a new Botwallet (same as 'wallet create')", `Create a new Botwallet for your AI agent.

This is an alias for 'wallet create' — both commands do exactly the same thing.

What happens:
  1. Generates a threshold key pair (your key share stays on your machine)
  2. Registers with Botwallet (server holds its own separate key share)
  3. Saves credentials to ~/.botwallet/

The wallet is secured with 2-of-2 threshold signing — neither the agent
nor the server can sign transactions alone. The wallet starts "unclaimed"
until a human owner claims it via a claim link.

`+walletNamingHelp+` Your human will see it when they claim.`, `  botwallet register --name "Assistant's Wallet"
  botwallet register --name "Research Budget" --owner human@example.com
  botwallet register --name "x402 APIs Allowance"`)

// walletCreateArgs refuses positional arguments: wallet create takes only
// flags. An email is pointed to --owner.
func walletCreateArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	for _, arg := range args {
		if strings.Contains(arg, "@") && strings.Contains(arg, ".") {
			return fmt.Errorf("%q looks like an email — use --owner instead:\n  %s --name \"...\" --owner %s", arg, cmd.CommandPath(), arg)
		}
	}
	return fmt.Errorf("unexpected argument %q\nThis command uses flags, not positional arguments. See: %s --help", args[0], cmd.CommandPath())
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
// wallet may exist on the server (see "New Wallet Registration" in
// config/newwallet.go).
func runWalletCreate(cmd *cobra.Command, args []string) {
	opts := walletCreateOpts
	name := strings.TrimSpace(opts.name)
	if name == "" {
		exitValidationError("--name is required",
			"This is your wallet — just pick a name and proceed. No need to ask your human.\n"+
				"  botwallet register --name \"Assistant's Wallet\"\n"+
				"  botwallet register --name \"Research Budget\"")
		return
	}

	// Make sure this machine can store the new wallet's keys before the
	// server creates anything.
	if err := config.PrepareNewWallet(); err != nil {
		exitError("CONFIG_ERROR",
			fmt.Sprintf("Cannot save wallet keys on this machine: %v", err),
			"Fix the problem above, then run 'botwallet wallet create' again. Nothing was created.", nil)
		return
	}

	client := anonClient()

	if output.IsHumanOutput() {
		output.InfoMsg("Setting up threshold signing...")
	}

	// Build optional metadata from --desc and --meta flags
	walletMetadata := make(map[string]interface{})
	if opts.desc != "" {
		walletMetadata["description"] = opts.desc
	}
	for _, m := range opts.meta {
		parts := strings.SplitN(m, "=", 2)
		if len(parts) != 2 || parts[0] == "" {
			exitValidationError(
				fmt.Sprintf("Invalid --meta format: %q", m),
				"Use key=value format: --meta platform=cursor --meta project=my-app",
			)
			return
		}
		walletMetadata[parts[0]] = parts[1]
	}

	dkgResult, err := client.DKGInit(name, opts.agentModel, opts.owner, walletMetadata)
	if err != nil {
		handleAPIError(err)
		return
	}

	sessionID, ok := dkgResult["session_id"].(string)
	if !ok || sessionID == "" {
		exitError("DKG_ERROR", "Server returned invalid DKG session",
			"Try again. If the problem persists, check your network connection", nil)
		return
	}

	serverPublicShareB64, ok := dkgResult["server_public_share"].(string)
	if !ok || serverPublicShareB64 == "" {
		exitError("DKG_ERROR", "Server returned invalid public key share",
			"Try again. If the problem persists, check your network connection", nil)
		return
	}

	// Decode server's public key share (A2)
	serverPublicShareBytes, err := base64.StdEncoding.DecodeString(serverPublicShareB64)
	if err != nil {
		exitError("DKG_ERROR", fmt.Sprintf("Failed to decode server public share: %v", err),
			"Try again. If the problem persists, this may be a server issue", nil)
		return
	}

	serverPublicShare, err := frost.DecodePoint(serverPublicShareBytes)
	if err != nil {
		exitError("DKG_ERROR", fmt.Sprintf("Server returned invalid Ed25519 point: %v", err),
			"Try again. If the problem persists, this may be a server issue", nil)
		return
	}

	// SECURITY: The mnemonic and key share are generated here and NEVER leave this machine.

	mnemonic, err := frost.GenerateShareMnemonic()
	if err != nil {
		exitError("KEY_GENERATION_ERROR", fmt.Sprintf("Failed to generate key share: %v", err),
			"This is unexpected. Try again", nil)
		return
	}

	// Derive the FROST key share from the mnemonic
	botShare, err := frost.KeyShareFromMnemonic(mnemonic)
	if err != nil {
		exitError("KEY_GENERATION_ERROR", fmt.Sprintf("Failed to derive key share: %v", err),
			"This is unexpected. Try again", nil)
		return
	}

	// Compute the group public key: A = A1 + A2
	groupKey := frost.ComputeGroupKey(botShare.Public, serverPublicShare)

	// Encode public values for the wire (base64 for API, base58 for Solana address)
	botPublicShareB64 := base64.StdEncoding.EncodeToString(frost.EncodePoint(botShare.Public))
	groupKeyBytes := frost.EncodePoint(groupKey)

	// Convert group key to Solana base58 address for display and storage
	groupKeyBase58 := solana.AddressString(groupKeyBytes)

	// SECURITY: The mnemonic is saved with 0600 permissions, before the server
	// creates the wallet, so the wallet never exists without its Key 1 on disk.
	// It is NEVER printed to stdout or included in any API response.
	localName, seedPath, err := config.SaveNewSeed(name, mnemonic, groupKeyBase58)
	if err != nil {
		exitError("CONFIG_ERROR",
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

	saved, err := registerNewWallet(localName, username, name, apiKey, groupKeyBase58)
	if err != nil {
		reportUnsavedWallet(err, result, localName, seedPath, config.WalletEntry{
			Username:    username,
			DisplayName: name,
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
	exitError("REGISTRATION_UNCERTAIN",
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

	exitError("CONFIG_ERROR",
		fmt.Sprintf("Wallet @%s was created, but could not be added to %s: %v", entry.Username, config.ConfigPath(), cause),
		fmt.Sprintf("Do not create it again. Key 1 is saved at %s. %s "+
			"Share the claim_url and claim_code with your human as usual.", seedPath, apiKeyWhere),
		details)
}
