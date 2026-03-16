# @botwallet/agent-cli

Command-line interface for AI agents to manage their Botwallet accounts.

## Installation

```bash
npm install -g @botwallet/agent-cli
```

## Quick Start

```bash
# Register a new wallet (FROST threshold key generation)
botwallet register --name "My Agent Wallet" --owner human@example.com

# Create an invoice and send it
botwallet paylink create 25.00 --desc "Research report"
botwallet paylink send <request_id> --to client@example.com --message "Here's your invoice"

# Two-step payment flow
botwallet pay @merchant 10.00              # Step 1: Create intent
botwallet pay confirm <transaction_id>     # Step 2: FROST sign & submit
```

`register` is the recommended way to create a wallet. `wallet create` does the same thing.

## Commands

| Group | Commands |
|-------|----------|
| `wallet` | `create`, `info`, `balance`, `list`, `use`, `deposit`, `owner`, `rename`, `backup`, `export`, `import` |
| `pay` | `(default)`, `confirm`, `preview`, `list`, `cancel` |
| `paylink` | `create`, `send`, `get`, `list`, `cancel` |
| `fund` | `(default)`, `ask`, `list` |
| `withdraw` | `(default)`, `confirm`, `get` |
| `x402` | `fetch`, `fetch confirm`, `discover` |
| Utilities | `register`, `history`, `limits`, `approvals`, `approval status`, `events`, `lookup`, `ping`, `version`, `docs` |

Run `botwallet docs` for full embedded documentation.

## Output Modes

- **JSON (default)** — Machine-readable for bots and scripts
- **`--human`** — Formatted output with colors and tips

## Authentication

Credentials are saved automatically on `register`. Or set manually:

```bash
export BOTWALLET_API_KEY="bw_bot_your_key_here"
```

Use `--wallet <name>` to switch between multiple wallets, or `--api-key` for explicit auth.

## Two-Step Flows

Payments, withdrawals, and x402 API access use a two-step flow:

1. **Create intent** — CLI validates, server checks guard rails
2. **Confirm** — FROST threshold signing (agent + server cooperate), then submit to Solana

This ensures neither party can sign alone.

## Troubleshooting

**`botwallet: command not found` after install?**

The npm global bin directory may not be in your PATH. Find and run it directly:

```bash
# Find the full path
npm prefix -g
# Then run using the full path (example for Windows):
C:\Users\<you>\AppData\Roaming\npm\botwallet.cmd version
# Or on macOS/Linux:
$(npm prefix -g)/bin/botwallet version
```

To fix permanently, add the npm global bin directory to your system PATH.

## Alternative Installation

### Direct Download

Download from [GitHub Releases](https://github.com/botwallet-co/agent-cli/releases).

### From Source

```bash
go install github.com/botwallet-co/agent-cli@latest
```

## License

Apache 2.0 — See [LICENSE](../LICENSE) for details.
