<div align="center">

# BotWallet CLI

**Your AI has a brain. Give it a wallet.**

[![npm](https://img.shields.io/npm/v/@botwallet/agent-cli?color=blue&label=npm)](https://www.npmjs.com/package/@botwallet/agent-cli)
[![License](https://img.shields.io/badge/license-Apache%202.0-green)](LICENSE)
[![GitHub stars](https://img.shields.io/github/stars/botwallet-co/agent-cli?style=social)](https://github.com/botwallet-co/agent-cli)

The CLI that lets AI agents hold, spend, and earn real money (USDC on Solana).

[Website](https://botwallet.co) · [Dashboard](https://app.botwallet.co) · [Docs](https://docs.botwallet.co) · [npm](https://www.npmjs.com/package/@botwallet/agent-cli)

</div>

---

Three commands. Your agent has a wallet with spending limits, human oversight, and FROST threshold signing.

```bash
npm install -g @botwallet/agent-cli
botwallet register --name "My Agent Wallet" --owner you@email.com
botwallet paylink create 25.00 --desc "Research report"  # Your agent just created an invoice
```

**What agents can do with BotWallet:**
- **Pay** other agents and merchants — `botwallet pay @recipient 10.00`
- **Earn** money via invoices and paylinks — `botwallet paylink create`
- **Access paid APIs** through x402 — `botwallet x402 fetch <url>`
- **Request funds** from their human owner — `botwallet fund 50.00`
- **Withdraw** USDC to any Solana address — `botwallet withdraw`

Every transaction is FROST 2-of-2 threshold signed (agent + server). The full private key never exists anywhere. Human owners set guard rails — per-transaction limits, daily caps, merchant allowlists — and approve anything outside the rules.

## Installation

```bash
# npm (recommended)
npm install -g @botwallet/agent-cli

# Homebrew (macOS/Linux)
brew install botwallet-co/tap/botwallet

# Linux/macOS
curl -fsSL https://botwallet.co/install.sh | sh

# Windows (PowerShell)
iwr https://botwallet.co/install.ps1 | iex

# From source (installs a binary named botwallet)
go install github.com/botwallet-co/agent-cli/cmd/botwallet@latest
```

## Quick Start

```bash
# Create a wallet (FROST threshold key generation, saves credentials locally)
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
| `wallet rename <name>` | Rename display name (username unchanged) |
| `wallet backup` | Back up Key 1 (two-step safety process) |
| `wallet export -o file.bwlt` | Export wallet to move it to another machine (file works for 24 hours; not a backup) |
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
| `events --mark-read --ids <id>,<id>` | Mark the events you handled as read |
| `events --mark-read` | Mark every unread event as read |

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
| `history` | Transaction history (`--type in/out/all`) |
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

A command always acts as one wallet: the one its API key belongs to, and it
signs with that wallet's Key 1. If `--wallet` and a key from `--api-key` or the
environment point at different wallets, the CLI stops with `WALLET_CONFLICT`
instead of picking one.

Keys and config live in `~/.botwallet/`. Set `BOTWALLET_HOME` to an absolute
path to use another folder (useful in containers without `HOME`). If neither
`HOME` nor `BOTWALLET_HOME` is set, the CLI stops with `NO_HOME_DIR` instead of
writing keys into the current directory.

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

## How It Works

BotWallet uses **FROST (Flexible Round-Optimized Schnorr Threshold) 2-of-2 signatures**. During wallet creation, a Distributed Key Generation ceremony produces two key shares:

- **Key 1** (the agent's share): stored locally at `~/.botwallet/seeds/<wallet>.seed`
- **Key 2** (the server's share): held by BotWallet, never sent to the agent

The full private key never exists anywhere. Every transaction requires both parties to produce partial signatures that combine into a valid Ed25519 signature. Neither the agent nor BotWallet can move funds alone.

Before Key 1 signs, the CLI decodes the Solana transaction and checks it against the payment it was shown. It signs only a USDC transfer from this wallet into the recipient's associated (standard) USDC account for that amount, plus at most the quoted fee. Anything else (another recipient, a larger amount, extra instructions) stops with `TRANSACTION_MISMATCH` and nothing is signed. The [MCP server](https://github.com/botwallet-co/mcp) and the signing page apply the same rules.

All payments settle in **USDC on Solana** — a dollar-pegged stablecoin. `10.00` = $10.00.

## Pending, Frozen and Repeated Payments

**Sent, not confirmed yet.** `pay confirm` and `withdraw confirm` wait for Solana to confirm the transfer. If the network has not confirmed it in time, the command still exits 0 and prints it as pending, not as an error: `"status": "pending"`, `"sent": true`, its `solana_signature` and a `check_command`. It may still go through, so do not create a new payment for it. Run the `check_command` (`pay list --id <id>` or `withdraw get <id>`) in about a minute: it shows `completed` or `failed`. `SIGNING_IN_PROGRESS` (it is being sent right now) and `SUBMIT_STATUS_UNKNOWN` (no answer came back) call for the same: check, don't pay again.

**Frozen wallet.** The owner can freeze the wallet in the Botwallet dashboard. A frozen wallet can still receive money but can't send any: new payments, withdrawals and paid API calls are blocked, and a confirm stops with `WALLET_SUSPENDED` before anything is signed or sent. A payment or withdrawal approved before the freeze is kept: once the owner unfreezes the wallet, run the same confirm command again (before it expires).

**Retrying a request.** `pay` and `withdraw` take `--idempotency-key`. Sending a key again returns the payment or withdrawal first made with it (`"idempotent": true`), whatever the other arguments, and never makes a second one. Use a new, unique key (for example a UUID) for each new payment. A key already used for another kind of request, or for one that is still being created, stops with `IDEMPOTENCY_KEY_CONFLICT`.

## Building from Source

```bash
make build          # Current platform
make build-all      # All platforms
make test           # Run tests
```

## Links

- **Website**: [botwallet.co](https://botwallet.co)
- **Human Dashboard**: [app.botwallet.co](https://app.botwallet.co)
- **Documentation**: [docs.botwallet.co](https://docs.botwallet.co)
- **npm**: [@botwallet/agent-cli](https://www.npmjs.com/package/@botwallet/agent-cli)

## License

Apache 2.0 — See [LICENSE](LICENSE) for details.
