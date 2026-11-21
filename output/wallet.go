package output

import "fmt"

// =============================================================================
// Wallet Formatters
// =============================================================================

// FormatRegisterSuccess formats a successful registration.
// FROST model: no seed phrase is ever displayed. Key shares are saved silently.
func FormatRegisterSuccess(data map[string]interface{}) {
	// For bots (JSON output - default): keep it focused on what matters now
	if !humanOutput {
		JSON(registerForAgent(data))
		return
	}

	previousDefault := getString(data, "previous_default")
	totalWallets := getInt(data, "total_wallets")
	claimExpiresAt := getString(data, "claim_expires_at")

	// Human-readable output
	SuccessMsg("Wallet created successfully!")

	Section("Wallet Details")
	KeyValue("Username", fmt.Sprintf("@%s", data["username"]))
	KeyValue("Wallet ID", data["wallet_id"])
	KeyValue("Address", data["deposit_address"])
	KeyValue("Security", "2-of-2 threshold signing (FROST)")

	if totalWallets > 1 && previousDefault != "" {
		fmt.Println()
		InfoMsg("This wallet is now your default (previously '%s').", previousDefault)
		styleDim.Printf("  Use 'botwallet wallet list' to see all %d wallets.\n", totalWallets)
	}

	claimURL := getString(data, "claim_url")
	claimCode := getString(data, "claim_code")

	Section("Next: Claim Your Wallet")
	fmt.Println("  Your wallet needs to be claimed before most features work.")
	fmt.Println()
	KeyValue("Claim Code", claimCode)
	KeyValueURL("Claim URL", claimURL)
	if claimExpiresAt != "" {
		KeyValue("Code expires", formatClaimExpiry(claimExpiresAt))
	} else {
		KeyValue("Code expires", "48 hours after registration")
	}
	fmt.Println()
	fmt.Println("  Share the claim link and code with your human. In plain words: this")
	fmt.Println("  is a spending account they will own. After claiming, they can set how")
	fmt.Println("  much you may spend without asking, add money, and withdraw any time.")
	fmt.Println("  Don't make payments until they've claimed it and set an allowance.")
	fmt.Println()
	WarningMsg("Key 1 of this wallet is stored only on this machine. If this machine is")
	fmt.Println("  temporary or is reset between tasks, tell your human now. Remind them to")
	fmt.Println("  save their backup keys in the Botwallet dashboard before they add money:")
	fmt.Println("  both keys are needed to withdraw, and they are the only way back if this")
	fmt.Println("  machine is reset.")
	fmt.Println()
	InfoMsg("After they claim it, thank them. You can offer to show what you can do,")
	fmt.Println("  e.g. a small sample payment link ('botwallet paylink create') they can")
	fmt.Println("  try, but never pressure them to pay you.")
}

// FormatBalance formats balance information.
// API returns: { balance, budget, budget_period, spent_this_period, remaining_budget, low_balance, funding_url, ... }
func FormatBalance(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	balance := getFloat(data, "balance")

	Section("Balance")
	KeyValueMoney("Available", balance)

	budget := getFloat(data, "budget")
	if budget > 0 {
		budgetPeriod := getString(data, "budget_period")
		if budgetPeriod == "" {
			budgetPeriod = "daily"
		}
		spent := getFloat(data, "spent_this_period")
		remaining := getFloat(data, "remaining_budget")

		Section(fmt.Sprintf("Spending (%s)", budgetPeriod))
		KeyValue("Budget", fmt.Sprintf("$%.2f / %s", budget, budgetPeriod))
		KeyValueMoney("Spent", spent)
		KeyValueMoney("Remaining", remaining)
	}

	// External activity detected (from reconciliation)
	if extActivity := getMap(data, "external_activity_detected"); extActivity != nil {
		fmt.Println()
		adjAmount := getFloat(extActivity, "adjustment_amount")
		adjType := getString(extActivity, "adjustment_type")
		if adjType == "external_deposit" {
			SuccessMsg("External deposit detected: +$%.2f", adjAmount)
		} else if adjAmount != 0 {
			WarningMsg("External activity detected: $%.2f (%s)", adjAmount, adjType)
		}
	}

	if getBool(data, "low_balance") {
		printLowBalance("Balance is low!", getString(data, "funding_url"))
	}

	Tip("Use 'botwallet pay @recipient <amount>' to send money.")
}

// FormatInfo formats wallet info
func FormatInfo(data map[string]interface{}) {
	// For bots (JSON output), add helpful context about unclaimed status
	if !humanOutput {
		JSON(infoForAgent(data))
		return
	}

	Section("Wallet Info")
	KeyValue("Username", fmt.Sprintf("@%s", getString(data, "username")))
	KeyValue("Name", getString(data, "name"))
	KeyValue("Status", getString(data, "status"))
	KeyValue("Claimed", getBool(data, "is_claimed"))
	KeyValueMoney("Balance", getFloat(data, "balance"))
	KeyValue("Deposit Address", getString(data, "deposit_address"))
	KeyValueURL("Funding URL", getString(data, "funding_url"))

	if getString(data, "status") == "unclaimed" {
		claimURL := getString(data, "claim_url")
		claimCode := getString(data, "claim_code")
		claimExpires := getString(data, "claim_expires_at")
		fmt.Println()
		// The box has a fixed width and hard-wraps long lines, which used to
		// split the claim URL in two. Keep the URL and code out of the box, each
		// on its own line, so they can be copied intact.
		CriticalBox("WALLET NOT CLAIMED - ACTION REQUIRED", `Your wallet is blocked until your human owner
claims it. Send them the claim URL and code below.`)
		fmt.Println()
		styleLabel.Println("  Claim URL:")
		styleURL.Printf("    %s\n", claimURL)
		styleLabel.Println("  Claim code:")
		styleHighlight.Printf("    %s\n", claimCode)
		if claimExpires != "" {
			KeyValue("Expires", formatClaimExpiry(claimExpires))
		}
		fmt.Println()
		styleLabel.Println("  Tell your human:")
		fmt.Printf("    \"Please claim my wallet on Botwallet at %s using code %s\"\n", claimURL, claimCode)
	}

	// Show metadata if present
	if meta := getMap(data, "metadata"); len(meta) > 0 {
		Section("Metadata")
		for k, v := range meta {
			KeyValue(k, v)
		}
	}

	if getBool(data, "low_balance") {
		printLowBalance("Balance is low!", "")
	}

	Tip("Use 'botwallet wallet balance' for detailed spending limits.")
}

// FormatDepositAddress formats deposit address info
func FormatDepositAddress(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	Section("Deposit Information")
	KeyValueHighlight("Deposit Address", getString(data, "deposit_address"))
	KeyValueURL("Funding URL", getString(data, "funding_url"))
	KeyValueMoney("Current Balance", getFloat(data, "balance"))

	instructions := getString(data, "instructions")
	if instructions != "" {
		fmt.Println()
		InfoMsg("%s", instructions)
	}

	Tip("Share the funding URL with your human owner for easy deposits.")
}

// FormatOwnerUpdated formats a changed owner email (wallet owner).
func FormatOwnerUpdated(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	SuccessMsg("Owner updated!")
	KeyValue("Pledged to", data["pledged_to"])
	if ownerFound, ok := data["owner_found"].(bool); ok && ownerFound {
		InfoMsg("This user exists - they can see the wallet in their portal now.")
	} else {
		InfoMsg("When this user signs up, the wallet will appear in their portal.")
	}
	if claimURL, ok := data["claim_url"].(string); ok {
		KeyValueURL("Claim URL", claimURL)
	}
	KeyValue("Claim Code", data["claim_code"])
}

// FormatWalletRenamed formats a changed display name (wallet rename).
func FormatWalletRenamed(data map[string]interface{}) {
	if !humanOutput {
		JSON(renamedForAgent(data))
		return
	}

	SuccessMsg("Wallet renamed!")
	KeyValue("Name", data["name"])
	KeyValue("Username", fmt.Sprintf("@%s (unchanged)", data["username"]))
	Tip("Your username is permanent — it's used in fund URLs and payment links.")
}

// =============================================================================
// Local Wallet Formatters (config.json and Key 1 on this machine)
// =============================================================================

// WalletListEntry is one wallet stored on this machine, as wallet list
// shows it.
type WalletListEntry struct {
	LocalName   string `json:"local_name"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	PublicKey   string `json:"public_key,omitempty"`
	IsDefault   bool   `json:"is_default"`
	SeedFile    string `json:"seed_file,omitempty"`
}

// FormatWalletList formats the wallets stored on this machine (wallet list).
func FormatWalletList(wallets []WalletListEntry) {
	if !humanOutput {
		JSON(map[string]interface{}{
			"wallets": wallets,
			"count":   len(wallets),
		})
		return
	}

	fmt.Println()
	fmt.Println("── Stored Wallets ────────────────────────────────────")
	for _, w := range wallets {
		defaultMarker := "  "
		if w.IsDefault {
			defaultMarker = "→ "
		}

		name := w.LocalName
		if w.DisplayName != "" && w.DisplayName != w.LocalName {
			name = fmt.Sprintf("%s (%s)", w.LocalName, w.DisplayName)
		}

		// A missing username says nothing about claiming; it was just not saved.
		username := "(no username saved)"
		if w.Username != "" {
			username = "@" + w.Username
		}

		fmt.Printf("%s%-20s  %s\n", defaultMarker, name, username)
	}
	fmt.Println()
	Tip("Use 'botwallet wallet use <name>' to switch default wallet")
}

// FormatWalletUse formats the switch of the default wallet to localName
// (wallet use). alreadyDefault says it was the default before.
func FormatWalletUse(localName, displayName, username, publicKey string, alreadyDefault bool) {
	if !humanOutput {
		JSON(map[string]interface{}{
			"success":         true,
			"default":         localName,
			"display_name":    displayName,
			"username":        username,
			"public_key":      publicKey,
			"already_default": alreadyDefault,
		})
		return
	}

	fmt.Println()
	if alreadyDefault {
		fmt.Printf("✅ Already using: %s", localName)
	} else {
		fmt.Printf("✅ Now using: %s", localName)
	}
	if displayName != "" && displayName != localName {
		fmt.Printf(" (%s)", displayName)
	}
	fmt.Println()
	if username != "" {
		fmt.Printf("   Username: @%s\n", username)
	}
	fmt.Println()
}

// FormatBackupStarted formats the first step of a Key 1 backup (wallet
// backup): the warning and the one-time code reveal-backup takes, which
// expires in expiresIn.
func FormatBackupStarted(code, expiresIn string, expiresInSeconds int) {
	if !humanOutput {
		JSON(map[string]interface{}{
			"action":             "backup_initiated",
			"warning":            "This will reveal Key 1 — 12 secret words that are half of the wallet's full backup key. Only proceed if the wallet OWNER asked for their backup key. Key 1 alone cannot access funds.",
			"next_step":          fmt.Sprintf("botwallet wallet reveal-backup --code %s", code),
			"code":               code,
			"expires_in":         expiresIn,
			"expires_in_seconds": expiresInSeconds,
		})
		return
	}

	CriticalBox("SENSITIVE KEY MATERIAL", `This command reveals Key 1 — 12 secret words
that are half of the wallet's full backup key.

Only run this if the wallet OWNER has specifically
asked you to share their backup key.

Key 1 alone cannot access funds, but should be
treated as highly confidential.`)
	fmt.Println()
	fmt.Println("  To proceed, run:")
	fmt.Printf("    botwallet wallet reveal-backup --code %s\n", code)
	fmt.Println()
	InfoMsg("Code expires in %s.", expiresIn)
}

// FormatKey1Revealed formats Key 1, the 12 secret words of wallet
// localName (wallet reveal-backup).
func FormatKey1Revealed(localName, mnemonic string) {
	if !humanOutput {
		JSON(map[string]interface{}{
			"key":          "Key 1",
			"description":  "12 secret words — the first half of the wallet backup key",
			"wallet":       localName,
			"secret_words": mnemonic,
			"instructions": "Share these 12 words with the wallet owner only, and send them privately (never in a shared or public channel). " +
				"The owner needs Key 1 to withdraw from the Botwallet dashboard and to recover the wallet. " +
				"Key 2 can be retrieved from the Botwallet dashboard.",
		})
		return
	}

	fmt.Println()
	WarningMsg("Key 1 (12 secret words) for \"%s\":", localName)
	fmt.Println()
	fmt.Printf("    %s\n", mnemonic)
	fmt.Println()
	InfoMsg("Share these 12 words with the wallet owner only, privately (never in a shared or public channel).")
	InfoMsg("The owner needs Key 1 to withdraw from the Botwallet dashboard and to recover the wallet.")
	InfoMsg("Key 2 can be retrieved from the Botwallet dashboard.")
	fmt.Println()
}

// FormatWalletExported formats a written .bwlt transfer file (wallet
// export): wallet localName, saved at path, to import as fileName.
func FormatWalletExported(localName, path, exportID, fileName string) {
	if !humanOutput {
		JSON(map[string]interface{}{
			"success":     true,
			"wallet_name": localName,
			"file":        path,
			"export_id":   exportID,
			"import_cmd":  fmt.Sprintf("botwallet wallet import %s", fileName),
			"valid_for":   "Up to 5 imports within 24 hours of this export. After that the file stops working.",
			"note": "Anyone with this file can import the wallet until it stops working. " +
				"Share it only privately, and delete it once the wallet is imported.",
			"owner_action": "This file only moves the wallet to another machine; it is not a backup. " +
				"To back up the wallet, the owner needs Key 1 (share it with 'botwallet wallet backup' when the owner asks for it) and Key 2 (from the Botwallet dashboard).",
		})
		return
	}

	fmt.Println()
	SuccessMsg("Wallet exported successfully!")
	fmt.Println()
	KeyValue("Wallet", localName)
	KeyValue("File", path)
	KeyValue("Export ID", exportID)
	fmt.Println()
	InfoMsg("Import on the other machine within 24 hours: botwallet wallet import %s", fileName)
	InfoMsg("The file works for up to 5 imports within 24 hours, then it stops working.")
	WarningMsg("Anyone with this file can import the wallet until then. Share it only privately, and delete it once the wallet is imported.")
	fmt.Println()
	InfoMsg("This file is not a backup. To back up the wallet, the owner needs Key 1 (shared with 'botwallet wallet backup' when the owner asks for it) and Key 2 (from the Botwallet dashboard).")
}

// FormatWalletImported formats a wallet imported from a .bwlt file (wallet
// import), saved as localName and made the default. previousDefault is the
// default before, of totalWallets wallets now on this machine.
func FormatWalletImported(localName, username, publicKey, previousDefault string, totalWallets int) {
	switchedDefault := previousDefault != "" && totalWallets > 1

	if !humanOutput {
		result := map[string]interface{}{
			"success":     true,
			"wallet_name": localName,
			"username":    username,
			"public_key":  publicKey,
			"is_default":  true,
		}
		if switchedDefault {
			result["previous_default"] = previousDefault
			result["total_wallets"] = totalWallets
			result["note"] = fmt.Sprintf("Wallet imported and set as default. Previous default was \"%s\".", previousDefault)
		}
		JSON(result)
		return
	}

	fmt.Println()
	SuccessMsg("Wallet imported successfully!")
	fmt.Println()
	KeyValue("Wallet", localName)
	KeyValue("Username", username)
	KeyValue("Deposit Address", publicKey)
	if switchedDefault {
		fmt.Println()
		InfoMsg("Set as default wallet. Previous default was \"%s\".", previousDefault)
		InfoMsg("Use 'botwallet wallet list' to see all wallets.")
	}
}
