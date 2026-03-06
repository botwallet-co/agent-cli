# Botwallet CLI

Command-line interface for AI agents to manage their Botwallet accounts.

## Installation

### From Release (Recommended)

```bash
# Linux/macOS
curl -fsSL https://botwallet.co/install.sh | bash

# Windows (PowerShell)
iwr https://botwallet.co/install.ps1 | iex
```

### From Source

```bash
go install github.com/botwallet-co/agent-cli@latest
```

## Quick Start

```bash
# Create a new wallet (FROST threshold key generation, saves credentials locally)
botwallet register --name "Orion's Wallet" --owner "your@email.com"

# Create an invoice and send it
botwallet paylink create 25.00 --desc "Research report"
botwallet paylink send <request_id> --to client@example.com --message "Here's your invoice"

# Two-step payment flow
botwallet pay @merchant 10.00                # Step 1: Create intent
botwallet pay confirm <transaction_id>       # Step 2: FROST sign & submit
```

`register` is the recommended way to create a wallet. `wallet create` does the same thing.

## Command Groups

### Wallet (`botwallet wallet ...`)
| Command | Description |
|---------|-------------|
| `wallet create --name "..." --owner email` | Create wallet (FROST key generation) |
| `wallet info` | Wallet info and claim status |
| `wallet balance` | Balance and spending limits |
| `wallet list` | List locally stored wallets |
| `wallet use <name>` | Switch default wallet |
| `wallet deposit` | Solana USDC deposit address |
| `wallet owner <email>` | Update pledged owner (unclaimed only) |
| `wallet backup` | Back up Key 1 (two-step safety process) |
| `wallet export -o file.bwlt` | Export wallet to encrypted .bwlt file |
| `wallet import file.bwlt` | Import wallet from .bwlt file |

### Payments (`botwallet pay ...`) — Two-Step
| Command | Description |
|---------|-------------|
| `pay @recipient <amount>` | **Step 1:** Create payment intent |
| `pay confirm <tx_id>` | **Step 2:** FROST sign & submit |
| `pay preview @to <amount>` | Pre-check if payment will succeed |
| `pay list` | List payments |
| `pay cancel <tx_id>` | Cancel a pending payment |
| `pay --paylink <id>` | Pay a payment link directly |

Flags: `--note`, `--reference`, `--paylink`, `--idempotency-key`

### Payment Links — Earning (`botwallet paylink ...`)
| Command | Description |
|---------|-------------|
| `paylink create [amount] --desc "..."` | Create payment link to get paid |
| `paylink send <id> --to <email\|@bot>` | Send paylink to email or bot's inbox |
| `paylink get <id>` | Check if paid |
| `paylink get --reference <ref>` | Look up by your reference ID |
| `paylink list` | List paylinks |
| `paylink cancel <id>` | Cancel paylink |

Create flags: `--desc` (required), `--item` (repeatable), `--expires`, `--reference`, `--revealOwner`
Send flags: `--to` (required, email or @bot-username), `--message` (optional note)

`--item` format — repeat for each line item, total auto-calculated:
```
--item "API Calls, 5.00, 2" --item "Setup Fee, 10.00"
```

### Fund Requests (`botwallet fund ...`)
| Command | Description |
|---------|-------------|
| `fund <amount> --reason "..."` | Request funds from owner |
| `fund ask <amount> --reason "..."` | Same as above (explicit subcommand) |
| `fund list` | List fund requests |

### Withdrawals (`botwallet withdraw ...`) — Two-Step
| Command | Description |
|---------|-------------|
| `withdraw <amount> <addr> --reason "..."` | **Step 1:** Create request (owner must approve) |
| `withdraw confirm <id>` | **Step 2:** FROST sign & submit |
| `withdraw get <id>` | Check withdrawal status |

### Approval Status (`botwallet approval ...`)
| Command | Description |
|---------|-------------|
| `approval status <approval_id>` | Check status of a specific approval (pending/approved/rejected/expired) |

Use this to poll after any action returns `awaiting_approval`. When status is `approved`, run the corresponding confirm command.

### Events & Notifications (`botwallet events`)
| Command | Description |
|---------|-------------|
| `events` | Check unread notifications |
| `events --type approval_resolved` | Filter by event type |
| `events --all` | Include already-read events |
| `events --limit 25` | Max events to return (default: 10) |
| `events --since <ISO-timestamp>` | Only events after this time |
| `events --mark-read` | Mark all as read |

Event types: `approval_resolved`, `deposit_received`, `payment_completed`, `fund_requested`, `fund_request_funded`, `wallet_pledged`, `guardrails_updated`, `x402_payment_completed`, `x402_payment_failed`

`notifications` is an alias for `events`.

### x402 Paid APIs (`botwallet x402 ...`) — Two-Step
| Command | Description |
|---------|-------------|
| `x402 discover` | List verified Solana APIs (curated catalog) |
| `x402 discover "query"` | Search catalog by keyword |
| `x402 discover --bazaar` | Search the full x402 Bazaar (Coinbase CDP) |
| `x402 discover --bazaar --all` | Bazaar: include all networks (default: Solana only) |
| `x402 fetch <url>` | **Step 1:** Probe API, see price |
| `x402 fetch confirm <fetch_id>` | **Step 2:** Pay and retrieve data |

Discover flags: `--bazaar`, `--limit` (bazaar), `--offset` (bazaar), `--all` (bazaar), `--facilitator`
Fetch flags: `--method`, `--body`, `--header` (repeatable)

### Utilities
| Command | Description |
|---------|-------------|
| `history` | Transaction history (`--type in/out/payment/deposit/withdrawal`) |
| `limits` | Spending limits and guard rails |
| `approvals` | List all pending owner approvals |
| `approval status <id>` | Check a specific approval's status |
| `lookup @username` | Check if recipient exists |
| `ping` | Test API connectivity |
| `version` | Print version information |
| `docs` | Full embedded documentation |

`transactions` is an alias for `history`.

## Authentication

Credentials auto-saved on `wallet create`. Priority order:

1. `--api-key` flag
2. `BOTWALLET_API_KEY` / `BW_API_KEY` env var
3. `--wallet` flag (selects from config)
4. Default wallet from `~/.botwallet/config.json`

## Output Modes

**JSON (default)** — for bots:
```bash
$ botwallet wallet balance
{"balance": 42.50, "daily_limit": 500.00, "spent_today": 10.00, "remaining_today": 490.00}
```

**Human** (`--human` flag) — formatted with colors:
```bash
$ botwallet wallet balance --human
── Balance ────────────────────
  Available: $42.50
── Daily Spending ─────────────
  Spent Today: $10.00 / $500.00
```

## Examples

```bash
# Pay someone
botwallet pay preview @openai 25.00
botwallet pay @openai 25.00 --note "API credits"
botwallet pay confirm <transaction_id>

# Earn money (simple)
botwallet paylink create 50.00 --desc "Research report"

# Earn money (itemized invoice — total auto-calculated)
botwallet paylink create --desc "Dev services" --item "API Calls, 5.00, 2" --item "Setup Fee, 10.00"
botwallet paylink send <id> --to client@example.com --message "Here's your invoice"
botwallet paylink send <id> --to @data-bot --message "Payment for data analysis"

# Request funds
botwallet fund 50.00 --reason "API costs"

# Withdraw
botwallet withdraw 100.00 YourSolanaAddr... --reason "Monthly earnings"
# Owner approves, then:
botwallet withdraw confirm <withdrawal_id>

# Wait for human approval (using approval status polling)
botwallet pay @merchant 500.00                  # Returns awaiting_approval + approval_id
botwallet approval status <approval_id>         # Poll: pending → approved
botwallet pay confirm <transaction_id>          # After approved

# Discover and use paid APIs
botwallet x402 discover                             # List verified Solana APIs
botwallet x402 discover "speech"                    # Search by keyword
botwallet x402 fetch <url_from_results>             # Probe, see price
botwallet x402 fetch confirm <fetch_id>             # Pay and get data

# Multiple wallets
botwallet wallet list
botwallet wallet use my-other-wallet
```

## Building from Source

```bash
make build          # Current platform
make build-all      # All platforms
make test           # Run tests
```

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.
