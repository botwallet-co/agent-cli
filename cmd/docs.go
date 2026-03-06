package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/botwallet-co/agent-cli/output"
)

const cliDocs = `# Botwallet CLI

Payment infrastructure for AI agents. Hold, spend, and earn money autonomously.


## Quick Start

    botwallet register --name "Orion's Wallet" --owner human@example.com
    # Done! API key auto-saved. Tell your human to claim the wallet.
    # After claiming: botwallet wallet balance


## CRITICAL: Wallet Must Be Claimed

After registering, your wallet is BLOCKED until a human claims it.
Tell your human: "Please claim my wallet at [claim_url] using code [claim_code]"
Only wallet info and wallet owner work before claiming.


## Commands

### Wallet (botwallet wallet ...)
    wallet create --name "Name" --owner email  Create wallet (FROST key generation, auto-saves)
    wallet info                                Get wallet info & claim status
    wallet balance                             Check balance and spending limits
    wallet list                                List all local wallets
    wallet use <name>                          Switch default wallet
    wallet deposit                             Get Solana USDC deposit address
    wallet owner <email>                       Change pledged owner (unclaimed only)
    wallet backup                              Back up Key 1 (two-step safety process)
    wallet export -o <file.bwlt>               Export wallet to encrypted .bwlt file
    wallet import <file.bwlt>                  Import wallet from .bwlt file

    'register' is a top-level alias for 'wallet create'.

### Payments (botwallet pay ...) — Two-Step Flow
    pay @recipient <amount>                    Step 1: Create payment intent
    pay confirm <transaction_id>               Step 2: FROST sign & submit to blockchain
    pay preview @recipient <amount>            Pre-check if payment will succeed
    pay list                                   List pending/actionable payments
    pay cancel <transaction_id>                Cancel a pending payment
    pay --paylink <paylink_id>                 Pay a payment link directly

    Flags: --note, --reference, --paylink, --idempotency-key

### Payment Links — Earning (botwallet paylink ...)
    paylink create [amount] --desc "..."       Create a payment link to get paid
    paylink send <id> --to <email|@bot>        Send paylink to email or bot's inbox
    paylink get <id>                           Check if paylink has been paid
    paylink get --reference <ref>              Look up by your reference ID
    paylink list                               List all your paylinks
    paylink cancel <id>                        Cancel a pending paylink

    Create flags: --desc (required), --item (repeatable), --expires, --revealOwner, --reference
    Send flags: --to (required), --message (optional personal note)

    --item format (repeat for each line item, total auto-calculated):
      --item "API Calls, 5.00, 2"    ← description, price, quantity
      --item "Setup Fee, 10.00"      ← description, price (quantity defaults to 1)

### x402 Paid APIs (botwallet x402 ...) — Two-Step Flow
    x402 discover                              List verified Solana APIs (curated catalog)
    x402 discover "speech"                     Search catalog by keyword
    x402 discover --bazaar                     Search full x402 Bazaar (Coinbase CDP)
    x402 discover --bazaar --all               Bazaar: include all networks (default: Solana only)
    x402 fetch <url>                           Step 1: Probe API, see price
    x402 fetch --method POST <url>             Step 1: Probe with specific HTTP method
    x402 fetch confirm <fetch_id>              Step 2: Pay and retrieve data

    Fetch flags: --method, --body, --header (repeatable)
    Discover flags: --bazaar, --limit (bazaar), --offset (bazaar), --all (bazaar), --facilitator

    Default discover shows curated, verified Solana APIs. Use --bazaar for the full catalog.
    Agents can probe multiple APIs ("window shopping") before paying.
    Guard rails apply: auto-approve, limits, budget, firewall.

### Fund Requests (botwallet fund ...)
    fund <amount> --reason "..."               Ask owner for funds
    fund ask <amount> --reason "..."           Same as above (explicit subcommand)
    fund list                                  List your fund requests

### Withdrawals (botwallet withdraw ...) — Two-Step Flow
    withdraw <amount> <address> --reason "..." Step 1: Create request (owner must approve)
    withdraw confirm <id>                      Step 2: FROST sign & submit
    withdraw get <id>                          Check withdrawal status

### Approval Status (botwallet approval ...)
    approval status <approval_id>              Check status of a specific approval

    Returns: pending, approved, rejected, or expired.
    Use this to poll after a payment, x402 fetch, or withdrawal needs approval.

### Events & Notifications (botwallet events)
    events                                     Check unread notifications
    events --type approval_resolved            Filter by type
    events --type deposit_received,payment_completed   Multiple types
    events --all                               Include already-read events
    events --limit 25                          Max events to return (default: 10)
    events --since "2026-02-19T00:00:00Z"      Only events after this timestamp
    events --mark-read                         Mark all as read

    'notifications' is an alias for 'events'.

    Types: approval_resolved, deposit_received, payment_completed,
           fund_requested, fund_request_funded, wallet_pledged, guardrails_updated,
           x402_payment_completed, x402_payment_failed

### Utilities
    history                                    Transaction history (--type in/out/payment/deposit/withdrawal)
    limits                                     Spending limits & guard rails
    approvals                                  Pending owner approvals (list all)
    approval status <id>                       Check a specific approval's status
    lookup @username                           Check if recipient exists
    ping                                       Test API connectivity (no auth)
    version                                    Print version information (no auth)
    docs                                       This documentation (no auth)

    'transactions' is an alias for 'history'.


## Workflows

### Setup
    botwallet register --name "Orion's Wallet" --owner human@example.com
    # Output includes claim_url, claim_code, and on_claimed instructions.
    # Share the claim details with your human so they can activate the wallet.
    # When they confirm claiming, create a sample invoice to demo your abilities:
    botwallet paylink create --desc "Wallet Setup & First Invoice" --item "Getting started, 5.00"

### Pay Someone
    botwallet pay preview @merchant 10.00           # Optional: pre-check
    botwallet pay @merchant 10.00                   # Step 1: intent
    botwallet pay confirm <transaction_id>          # Step 2: sign & submit

### Pay a Payment Link
    botwallet pay --paylink pl_abc123
    botwallet pay confirm <transaction_id>

### Earn Money
    botwallet paylink create 25.00 --desc "Research report"
    # Share the payment_url — anyone can pay it
    botwallet paylink send <id> --to client@example.com  # Email to human
    botwallet paylink send <id> --to @other-bot          # Send to bot's inbox
    botwallet paylink get <id>                            # Check if paid

### Earn Money (itemized invoice)
    botwallet paylink create --desc "Dev services" --item "API Calls, 5.00, 2" --item "Setup Fee, 10.00"
    botwallet paylink send <id> --to client@example.com --message "Here's your invoice"
    botwallet paylink send <id> --to @data-bot --message "Payment for data analysis"

### Request Funds
    botwallet fund 50.00 --reason "API costs"

### Withdraw
    botwallet withdraw 50.00 <solana-addr> --reason "Pay hosting"
    # Owner approves on dashboard, then:
    botwallet withdraw confirm <withdrawal_id>

### Access Paid APIs (x402)
    botwallet x402 discover                                   # List verified APIs
    botwallet x402 discover "speech"                          # Search by keyword
    botwallet x402 fetch <url_from_results>                   # Step 1: see price
    botwallet x402 fetch confirm <fetch_id>                   # Step 2: pay and get data

### Wait for Human Approval
    botwallet pay @merchant 500.00                  # Returns awaiting_approval + approval_id
    # Tell human to approve, then check status:
    botwallet approval status <approval_id>         # Returns pending/approved/rejected/expired
    # When status is "approved":
    botwallet pay confirm <transaction_id>          # Execute the payment

    Tip: Save the approval_id to persistent memory so you remember to check later.

### Multiple Wallets
    botwallet wallet list
    botwallet wallet use other-wallet
    botwallet wallet balance --wallet my-wallet


## Response Format

JSON by default. Use --human flag for formatted terminal output.

    Success: { "field": "value" }
    Error:   { "error": "CODE", "message": "...", "how_to_fix": "..." }


## Security: FROST Threshold Signing

- Agent holds key share S1 locally (~/.botwallet/seeds/<wallet>.seed)
- Server holds key share S2 (never sent to agent)
- Neither can sign alone — both cooperate for every transaction
- Full private key never exists anywhere
- API key stored in ~/.botwallet/config.json
- Use 'wallet backup' to export S1 (two-step safety process)


## Tips

1. Run 'pay preview' before 'pay' to avoid errors
2. Use --owner on wallet create so it appears in your human's portal
3. When a command returns awaiting_approval, save the approval_id to
   persistent memory. Poll with 'approval status <id>' until resolved.
4. After processing events, run 'events --mark-read' to stay clean
5. Paylinks expire — monitor with 'paylink get'
6. Use --item flags for itemized invoices on paylinks
7. Use 'paylink send' to deliver a paylink to an email or bot (--to @bot-name)


## More Help

    botwallet --help                  Overview
    botwallet <command> --help        Command-specific help
    botwallet docs --json             Machine-readable command schema
    https://botwallet.co/docs/cli     Online documentation
`

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Show full CLI documentation",
	Long: `Output complete CLI documentation for AI agents and humans.

This is a local command - no API calls, no authentication required.
Use this to understand all available commands and how to use them.`,
	Example: `  botwallet docs              # Full documentation
  botwallet docs | less       # Page through docs (humans)
  botwallet docs --json       # Machine-readable format`,
	Run: func(cmd *cobra.Command, args []string) {
		jsonFormat, _ := cmd.Flags().GetBool("json")

		if jsonFormat {
			output.JSON(getDocsJSON())
			return
		}

		fmt.Print(cliDocs)
	},
}

func init() {
	docsCmd.Flags().Bool("json", false, "Output in JSON format for programmatic parsing")
}

// getDocsJSON returns structured documentation as a map
func getDocsJSON() map[string]interface{} {
	return map[string]interface{}{
		"name":        "botwallet",
		"version":     version,
		"description": "Payment infrastructure for AI agents",
		"authentication": map[string]interface{}{
			"method":      "api_key",
			"recommended": "Credentials auto-saved on wallet create. Use --wallet flag for multiple wallets.",
			"config_dir":  "~/.botwallet/",
			"files": map[string]string{
				"config.json":         "Wallet registry and API keys (0600)",
				"seeds/<wallet>.seed": "Key shares - S1 (0600)",
			},
			"priority_order": []string{
				"1. --api-key flag (if provided)",
				"2. BOTWALLET_API_KEY env var",
				"3. --wallet flag (selects wallet from config)",
				"4. Default wallet from config file",
			},
			"security_model": "2-of-2 threshold signing (FROST). Agent holds S1 locally, server holds S2. Neither can sign alone. Key shares are NEVER displayed during registration.",
		},
		"output": map[string]string{
			"default": "JSON (for agents)",
			"human":   "Use --human flag for formatted output",
		},
		"command_groups": []map[string]interface{}{
			{
				"name":        "wallet",
				"description": "Wallet management",
				"commands": []map[string]interface{}{
					{
						"name":        "create",
						"description": "Create a new wallet",
						"flags":       []string{"--name (required)", "--owner (recommended)", "--model (optional)"},
						"example":     "botwallet wallet create --name \"Research Wallet\" --owner human@example.com",
					},
					{
						"name":        "info",
						"description": "Get wallet information",
						"example":     "botwallet wallet info",
					},
					{
						"name":        "balance",
						"description": "Check balance and spending limits",
						"example":     "botwallet wallet balance",
					},
					{
						"name":        "list",
						"description": "List all locally stored wallets",
						"example":     "botwallet wallet list",
					},
					{
						"name":        "use",
						"description": "Switch default wallet",
						"args":        []string{"wallet-name"},
						"example":     "botwallet wallet use my-research-wallet",
					},
					{
						"name":        "deposit",
						"description": "Get Solana USDC deposit address",
						"example":     "botwallet wallet deposit",
					},
					{
						"name":        "owner",
						"description": "Change pledged owner email (unclaimed wallets only)",
						"args":        []string{"email"},
						"example":     "botwallet wallet owner new@example.com",
					},
					{
						"name":        "backup",
						"description": "Back up Key 1 — 12 secret words (two-step safety process)",
						"example":     "botwallet wallet backup",
					},
					{
						"name":        "export",
						"description": "Export wallet to an encrypted .bwlt file",
						"flags":       []string{"-o/--output (required)"},
						"example":     "botwallet wallet export -o wallet.bwlt",
					},
					{
						"name":        "import",
						"description": "Import wallet from a .bwlt file",
						"args":        []string{"file-path"},
						"flags":       []string{"--name (optional, override local name)"},
						"example":     "botwallet wallet import wallet.bwlt",
					},
				},
			},
			{
				"name":        "pay",
				"description": "Payments (two-step flow)",
				"commands": []map[string]interface{}{
					{
						"name":        "(default)",
						"description": "Step 1: Create payment intent",
						"args":        []string{"recipient", "amount"},
						"flags":       []string{"--note", "--paylink (pay a payment link)", "--reference", "--idempotency-key"},
						"example":     "botwallet pay @merchant 10.00",
						"next_step":   "Run 'botwallet pay confirm <transaction_id>' to execute",
					},
					{
						"name":        "confirm",
						"description": "Step 2: FROST sign transaction locally and submit to Solana",
						"args":        []string{"transaction_id"},
						"example":     "botwallet pay confirm abc12345-6789-...",
					},
					{
						"name":        "preview",
						"description": "Pre-check if payment will succeed",
						"args":        []string{"recipient", "amount"},
						"example":     "botwallet pay preview @merchant 10.00",
					},
					{
						"name":        "list",
						"description": "List pending/actionable payment transactions",
						"flags":       []string{"--status", "--id", "--limit", "--offset"},
						"example":     "botwallet pay list --status pending",
					},
					{
						"name":        "cancel",
						"description": "Cancel a pending payment",
						"args":        []string{"transaction_id"},
						"example":     "botwallet pay cancel abc12345-6789-...",
					},
				},
			},
			{
				"name":        "x402",
				"description": "x402 paid API access (two-step: fetch probe → fetch confirm)",
				"commands": []map[string]interface{}{
					{
						"name":        "fetch",
						"description": "Step 1: Probe an x402 API to see its price without paying",
						"args":        []string{"url"},
						"flags":       []string{"--method (default: GET)", "--body", "--header (repeatable)"},
						"example":     "botwallet x402 fetch https://api.weather.com/forecast",
						"next_step":   "If 402, run: botwallet x402 fetch confirm <fetch_id>",
					},
					{
						"name":        "fetch confirm",
						"description": "Step 2: FROST sign payment and retrieve API data",
						"args":        []string{"fetch_id"},
						"example":     "botwallet x402 fetch confirm abc12345-6789-...",
					},
					{
						"name":        "discover",
						"description": "List verified Solana APIs from curated catalog, or search the full x402 Bazaar with --bazaar",
						"args":        []string{"query (optional, keyword filter on name/description/category)"},
						"flags":       []string{"--bazaar (search Coinbase CDP instead of curated catalog)", "--limit (bazaar, default 20)", "--offset (bazaar, default 0)", "--all (bazaar, show all networks)", "--facilitator (override URL)"},
						"example":     "botwallet x402 discover \"speech\"",
					},
				},
			},
			{
				"name":        "paylink",
				"description": "Payment links (earning) - create shareable payment URLs to get paid by anyone",
				"commands": []map[string]interface{}{
				{
						"name":        "create",
						"description": "Create a payment link to receive money",
						"args":        []string{"amount (optional when using --item)"},
						"flags":       []string{"--desc (required)", "--expires", "--item (repeatable: \"description, price[, qty]\")", "--revealOwner"},
						"example":     "botwallet paylink create --desc \"Research report\" --item \"Research, 25.00\"",
						"item_format": "Each --item is \"description, price[, quantity]\". Quantity defaults to 1. Total auto-calculated from items.",
					},
					{
						"name":        "send",
						"description": "Email a paylink as a payment request to a recipient",
						"args":        []string{"paylink-id"},
						"flags":       []string{"--to (required, email or @bot-username)", "--message (optional personal note)"},
						"example":     "botwallet paylink send <request_id> --to @data-bot",
					},
					{
						"name":        "get",
						"description": "Check if paylink has been paid",
						"args":        []string{"paylink-id"},
						"flags":       []string{"--reference (look up by your reference ID instead)"},
						"example":     "botwallet paylink get pl_abc123",
					},
					{
						"name":        "list",
						"description": "List all your paylinks",
						"flags":       []string{"--status", "--limit", "--offset"},
						"example":     "botwallet paylink list --status pending",
					},
					{
						"name":        "cancel",
						"description": "Cancel a pending paylink",
						"args":        []string{"paylink-id"},
						"example":     "botwallet paylink cancel pl_abc123",
					},
				},
			},
			{
				"name":        "fund",
				"description": "Fund requests (from owner). 'fund <amount>' is a shortcut for 'fund ask <amount>'.",
				"commands": []map[string]interface{}{
					{
						"name":        "ask",
						"description": "Ask owner for funds",
						"args":        []string{"amount"},
						"flags":       []string{"--reason (required)"},
						"example":     "botwallet fund 50.00 --reason \"API costs\"",
					},
					{
						"name":        "list",
						"description": "List fund requests to owner",
						"flags":       []string{"--status", "--limit", "--offset"},
						"example":     "botwallet fund list",
					},
				},
			},
			{
				"name":        "withdraw",
				"description": "Withdrawals (two-step flow, requires owner approval)",
				"commands": []map[string]interface{}{
					{
						"name":        "(default)",
						"description": "Step 1: Create withdrawal request (requires owner approval)",
						"args":        []string{"amount", "solana-address"},
						"flags":       []string{"--reason (required)", "--idempotency-key"},
						"example":     "botwallet withdraw 50.00 7xKXt... --reason \"Pay hosting\"",
					},
					{
						"name":        "confirm",
						"description": "Step 2: FROST sign and submit approved withdrawal to Solana",
						"args":        []string{"withdrawal-id"},
						"example":     "botwallet withdraw confirm abc12345-6789-...",
					},
					{
						"name":        "get",
						"description": "Check withdrawal status",
						"args":        []string{"withdrawal-id"},
						"example":     "botwallet withdraw get tx_abc123",
					},
				},
			},
		},
		"approval": map[string]interface{}{
			"name":        "approval",
			"description": "Check and manage specific owner approvals",
			"commands": []map[string]interface{}{
				{
					"name":        "status",
					"description": "Check the current status of a specific approval",
					"args":        []string{"approval_id"},
					"returns":     []string{"pending", "approved", "rejected", "expired"},
					"example":     "botwallet approval status abc123-def456",
					"usage":       "Poll this after any action returns awaiting_approval. When status is 'approved', run the corresponding confirm command.",
				},
			},
		},
		"events": map[string]interface{}{
			"description": "Notification system for asynchronous updates from your human owner and the system",
			"commands": []map[string]interface{}{
				{
					"name":        "events",
					"description": "Check unread notifications",
					"flags":       []string{"--type (filter)", "--limit (max 25)", "--all (include read)", "--since (ISO timestamp)"},
					"example":     "botwallet events",
				},
				{
					"name":        "events --mark-read",
					"description": "Mark all events as read",
					"example":     "botwallet events --mark-read",
				},
			},
			"event_types": []string{
				"approval_resolved — human approved/rejected a payment or withdrawal",
				"deposit_received — funds arrived in your wallet",
				"payment_completed — a payment you made completed on-chain",
				"fund_requested — you requested funds (confirmation)",
				"fund_request_funded — human funded your request",
				"wallet_pledged — wallet was pledged to an owner",
				"guardrails_updated — spending limits changed",
				"x402_payment_completed — x402 API payment settled successfully",
				"x402_payment_failed — x402 API payment failed",
			},
			"recommended_workflow": "After any action requiring human approval, periodically check 'botwallet events' to see when the human responds. Mark events as read after processing them.",
		},
		"aliases": map[string]string{
			"register":      "wallet create",
			"notifications": "events",
			"transactions":  "history",
		},
		"utilities": []map[string]interface{}{
			{
				"name":        "history",
				"description": "View transaction history",
				"aliases":     []string{"transactions"},
				"flags":       []string{"--type", "--limit", "--offset"},
				"example":     "botwallet history --type out",
			},
			{
				"name":        "limits",
				"description": "View spending limits and guard rails",
				"example":     "botwallet limits",
			},
			{
				"name":        "approvals",
				"description": "View all pending owner approvals",
				"example":     "botwallet approvals",
			},
			{
				"name":        "events",
				"description": "Check wallet notifications and events",
				"aliases":     []string{"notifications"},
				"flags":       []string{"--type", "--limit", "--all", "--since", "--mark-read"},
				"example":     "botwallet events",
			},
			{
				"name":        "lookup",
				"description": "Check if a recipient exists",
				"args":        []string{"username"},
				"example":     "botwallet lookup @merchant-name",
			},
			{
				"name":        "ping",
				"description": "Test API connectivity",
				"auth":        false,
				"example":     "botwallet ping",
			},
			{
				"name":        "version",
				"description": "Print version information",
				"auth":        false,
				"example":     "botwallet version",
			},
			{
				"name":        "docs",
				"description": "Show full CLI documentation",
				"auth":        false,
				"example":     "botwallet docs",
			},
		},
		"workflows": map[string][]string{
			"first_time": {
				"botwallet wallet create --name \"Orion's Wallet\" --owner human@example.com",
				"# Credentials auto-saved. Tell human to claim wallet.",
				"# After claiming: botwallet wallet balance",
			},
			"make_payment": {
				"botwallet pay preview @recipient 10.00",
				"botwallet pay @recipient 10.00",
				"# Returns transaction_id",
				"botwallet pay confirm <transaction_id>",
				"# Payment complete!",
			},
			"pay_paylink": {
				"botwallet pay --paylink pl_abc123",
				"botwallet pay confirm <transaction_id>",
			},
			"earn_money": {
				"botwallet paylink create 25.00 --desc \"Service\"",
				"# Share the payment_url, or send it:",
				"botwallet paylink send <id> --to @other-bot       # to bot",
				"botwallet paylink send <id> --to client@example.com  # to email",
				"botwallet paylink get <id>",
			},
			"itemized_invoice": {
				"# Create paylink with itemized line items (total auto-calculated)",
				"botwallet paylink create --desc \"Services\" --item \"API Calls, 5.00, 2\" --item \"Setup, 10.00\"",
				"# Format: --item \"description, price[, quantity]\"",
				"# Quantity defaults to 1. Repeat --item for each line item.",
			},
			"withdraw": {
				"botwallet withdraw 50.00 7xKXtR9... --reason \"Pay hosting\"",
				"# Wait for owner to approve on dashboard",
				"botwallet withdraw confirm <withdrawal_id>",
			},
			"x402_paid_api": {
				"botwallet x402 discover",
				"# Returns: list of verified Solana APIs with prices",
				"botwallet x402 fetch <url_from_results>",
				"# Returns: fetch_id, price, status (window shopping)",
				"botwallet x402 fetch confirm <fetch_id>",
				"# Returns API data",
			},
			"check_for_updates": {
				"botwallet events",
				"# See approval_resolved, deposit_received, payment_completed, etc.",
				"# After processing: botwallet events --mark-read",
			},
			"approval_flow": {
				"botwallet pay @merchant 500.00",
				"# Returns awaiting_approval with approval_id — tell human to approve",
				"# Save approval_id to memory, then poll periodically:",
				"botwallet approval status <approval_id>",
				"# When status is 'approved': botwallet pay confirm <transaction_id>",
			},
			"multi_wallet": {
				"botwallet wallet list",
				"botwallet wallet use other-wallet",
				"botwallet wallet balance --wallet my-wallet",
			},
		},
		"documentation_url": "https://botwallet.co/docs/cli",
	}
}
