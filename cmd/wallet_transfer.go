package cmd

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/config"
	"github.com/botwallet-co/agent-cli/output"
	"github.com/botwallet-co/agent-cli/solana"
	"github.com/botwallet-co/agent-cli/solana/frost"
)

// Moving a wallet to another machine: wallet export writes a .bwlt file and
// wallet import reads it.
//
// A .bwlt file is a transfer file, not a backup: the server hands out its
// decryption key up to 5 times within 24 hours of the export, then revokes
// it (MAX_IMPORT_USES and EXPORT_EXPIRY_HOURS in the server's
// wallet_import_key). The copy below must match those limits.

// exportExpiredHowToFix is the fix for an import of an expired export.
const exportExpiredHowToFix = "Run 'botwallet wallet export' again on the machine that has the wallet, " +
	"then import the new file within 24 hours"

var walletExportOutput string

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
			exitValidationError("--output flag is required", "Usage: botwallet wallet export -o wallet.bwlt")
			return
		}

		// Ensure .bwlt extension
		outPath := walletExportOutput
		if !strings.HasSuffix(strings.ToLower(outPath), ".bwlt") {
			outPath += ".bwlt"
		}

		// Prevent accidental overwrite of existing file
		if _, err := os.Stat(outPath); err == nil {
			exitError("FILE_EXISTS",
				fmt.Sprintf("File already exists: %s", outPath),
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
			exitError("CONFIG_ERROR", fmt.Sprintf("Failed to load key share: %v", err),
				"Check that the seed file exists in "+config.SeedsDir(), nil)
			return
		}

		if output.IsHumanOutput() {
			output.InfoMsg("Exporting wallet \"%s\"...", localName)
		}

		// Call server to get encryption key
		client := newClient(w.APIKey)
		exportID, keyB64, err := client.ExportWallet()
		if err != nil {
			handleAPIError(err)
			return
		}

		// Decode the encryption key
		encKey, err := base64.StdEncoding.DecodeString(keyB64)
		if err != nil {
			exitError("EXPORT_ERROR", fmt.Sprintf("Failed to decode encryption key: %v", err),
				"Try again. If the problem persists, this may be a server issue", nil)
			return
		}
		defer clear(encKey)

		// Build the payload
		payload := config.BWLTPayload{
			Version:    config.BWLTPayloadVersion,
			WalletName: wallet.DisplayName,
			Username:   wallet.Username,
			APIKey:     wallet.APIKey,
			PublicKey:  wallet.PublicKey,
			Seed:       seed,
			ExportedAt: time.Now().UTC().Format(time.RFC3339),
		}

		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			exitError("EXPORT_ERROR", fmt.Sprintf("Failed to prepare export data: %v", err),
				"This is unexpected. Try again", nil)
			return
		}
		defer clear(payloadJSON)

		// Encrypt
		nonce, ciphertext, err := config.EncryptPayload(encKey, payloadJSON)
		if err != nil {
			exitError("EXPORT_ERROR", fmt.Sprintf("Encryption failed: %v", err),
				"Try again. If the problem persists, this may be a server issue", nil)
			return
		}

		// Write the .bwlt file. It is never written over a file, even one
		// that appeared after the check above.
		if err := config.WriteBWLT(outPath, exportID, nonce, ciphertext); err != nil {
			if errors.Is(err, fs.ErrExist) {
				exitError("FILE_EXISTS",
					fmt.Sprintf("File already exists: %s", outPath),
					"Use a different path or delete the existing file first", nil)
				return
			}
			exitError("EXPORT_ERROR", fmt.Sprintf("Failed to write file: %v", err),
				"Check file permissions and available disk space", nil)
			return
		}

		// Read-back verification: ensure the written file is structurally valid
		verifyID, verifyNonce, verifyCipher, verifyErr := config.ReadBWLT(outPath)
		if verifyErr != nil || verifyID != exportID || len(verifyNonce) != len(nonce) || len(verifyCipher) != len(ciphertext) {
			_ = os.Remove(outPath)
			exitError("EXPORT_ERROR",
				"Write verification failed — the exported file was corrupt and has been deleted",
				"Try again. If the problem persists, check disk health", nil)
			return
		}

		absPath, _ := filepath.Abs(outPath)
		output.FormatWalletExported(localName, absPath, exportID, filepath.Base(outPath))
	},
}

func init() {
	walletExportCmd.Flags().StringVarP(&walletExportOutput, "output", "o", "", "Output file path (required)")
	_ = walletExportCmd.MarkFlagRequired("output")
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
			exitError("IMPORT_ERROR", fmt.Sprintf("Failed to read .bwlt file: %v", err),
				"Check that the file exists and is a valid .bwlt export", nil)
			return
		}

		if output.IsHumanOutput() {
			output.InfoMsg("Importing wallet from %s...", filepath.Base(filePath))
		}

		// Call server to get decryption key (no auth needed)
		keyB64, err := anonClient().ImportWalletKey(exportID)
		if err != nil {
			// The server revokes an export after 5 imports or 24 hours. The
			// first import after that gets EXPORT_EXPIRED, later ones NOT_FOUND.
			// Only the machine that has the wallet can make a new export.
			var apiErr *api.APIError
			if errors.As(err, &apiErr) {
				switch apiErr.Code {
				case "EXPORT_EXPIRED", "NOT_FOUND":
					exitError("EXPORT_EXPIRED",
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
			exitError("IMPORT_ERROR", fmt.Sprintf("Failed to decode decryption key: %v", err),
				"The .bwlt file may be corrupted. Try exporting again", nil)
			return
		}
		defer clear(encKey)

		// Decrypt
		plaintext, err := config.DecryptPayload(encKey, nonce, ciphertext)
		if err != nil {
			exitError("IMPORT_ERROR", fmt.Sprintf("Decryption failed: %v", err),
				"The .bwlt file may be corrupted or tampered with. Try exporting again", nil)
			return
		}
		defer clear(plaintext)

		// Parse the payload
		var payload config.BWLTPayload
		if err := json.Unmarshal(plaintext, &payload); err != nil {
			exitError("IMPORT_ERROR", fmt.Sprintf("Failed to parse wallet data: %v", err),
				"The .bwlt file may be corrupted. Try exporting again", nil)
			return
		}

		if payload.APIKey == "" || payload.Seed == "" || payload.Username == "" || payload.PublicKey == "" {
			exitError("IMPORT_ERROR", "Wallet file is missing required data",
				"The .bwlt file may be corrupted. Try exporting again", nil)
			return
		}

		if err := checkImportedKeys(&payload); err != nil {
			exitError("IMPORT_ERROR", "Wallet file is not usable: "+err.Error(),
				"The .bwlt file may be corrupted. Export the wallet again on the machine that has it", nil)
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
			exitError("IMPORT_ERROR", fmt.Sprintf("Failed to import wallet: %v", err),
				"Check file permissions on "+config.ConfigDir(), nil)
			return
		}

		output.FormatWalletImported(saved.LocalName, payload.Username, payload.PublicKey, saved.PreviousDefault, saved.TotalWallets)
	},
}

// checkImportedKeys refuses a payload that would give a wallet that cannot
// sign, before anything is saved (an imported wallet becomes the default):
// Key 1 must be a valid phrase and the address a Solana address. It also
// joins the words with single spaces, as the seed file stores them. The MCP
// server checks the same (parseBwltPayload in its config/bwlt.ts).
func checkImportedKeys(p *config.BWLTPayload) error {
	p.Seed = strings.Join(strings.Fields(p.Seed), " ")
	if _, err := frost.KeyShareFromMnemonic(p.Seed); err != nil {
		return errors.New("its Key 1 words are not a valid phrase")
	}
	if !solana.IsValidAddress(p.PublicKey) {
		return fmt.Errorf("its wallet address %q is not a Solana address", p.PublicKey)
	}
	return nil
}

func init() {
	walletImportCmd.Flags().StringVar(&walletImportName, "name", "", "Override the local wallet name")
}
