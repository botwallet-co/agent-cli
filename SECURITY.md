# Security Policy

## Supported Versions

| Version | Supported |
|---|---|
| Latest beta/release | Yes |
| Older versions | No — please upgrade |

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

If you discover a security vulnerability in the Botwallet CLI, please report it responsibly:

**Email:** security@botwallet.co

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

## What to Expect

- **Acknowledgment** within 48 hours
- **Assessment** within 5 business days
- **Fix or mitigation** timeline communicated after assessment
- **Credit** in the release notes (unless you prefer anonymity)

## Scope

This policy covers the Botwallet CLI (`agent-cli`) and its npm package (`@botwallet/agent-cli`).

Vulnerabilities in the following are in scope:
- Local credential storage (`~/.botwallet/`)
- FROST threshold signing implementation
- API communication and TLS handling
- Key material handling in memory
- The npm postinstall binary download mechanism

Out of scope:
- The Botwallet backend API (report separately to security@botwallet.co)
- Third-party dependencies (report upstream, but let us know)
- Social engineering attacks

## Security Design

The Botwallet CLI is built with the following security principles:

- **No secrets in source code** — API keys and credentials are stored locally in `~/.botwallet/` (or `$BOTWALLET_HOME`), never hardcoded
- **Local storage is not encrypted** — `config.json` (API keys) and `seeds/*.seed` (Key 1, the agent's key share) are plain files. They are protected only by file permissions: 0600 files in 0700 folders on macOS and Linux, and the user profile's access rules on Windows. Anyone who can read these files as your user can use the wallet within its limits.
- **FROST threshold signing** — Neither the CLI nor the server can sign transactions alone. Both parties must cooperate.
- **Checks before signing** — The CLI decodes each transaction before its key share signs, and signs only a USDC transfer from the wallet into the recipient's associated (standard) USDC account, for the amount it was shown, plus at most the quoted fee. The MCP server and the signing page apply the same rules.
- **No second payment by mistake** — A payment or withdrawal that was sent but is not confirmed on Solana yet is reported as pending, with its Solana signature and the command that shows how it ends, never as a failure. An `--idempotency-key` sent again returns the request first made with it instead of making a second one.
- **Frozen wallets can't send** — While the owner has frozen the wallet, Botwallet refuses to build or co-sign any payment, withdrawal or paid API call (`WALLET_SUSPENDED`), including ones approved before the freeze.
- **Wallet export is a short-lived transfer file** — A `.bwlt` file holds the API key and Key 1, encrypted with AES-256-GCM under a random 256-bit key that the Botwallet server keeps. The file carries the export ID, and the server gives the key to whoever presents that ID, without other authentication, up to 5 times within 24 hours of the export. After that the file can no longer be decrypted. There is no passphrase: treat a `.bwlt` file like the wallet itself while it is valid, and do not keep it as a backup.
- **No telemetry** — The CLI does not phone home or collect usage data
- **Network destinations** — The CLI connects to:
  - `api.botwallet.co` (configurable with `--api-url` or `BOTWALLET_API_URL`)
  - the x402 API URLs you fetch with `x402 fetch`
  - the x402 facilitator for `x402 discover --bazaar` (Coinbase CDP by default; `--facilitator` or `X402_FACILITATOR_URL` changes it)
  - GitHub Releases, when the npm package or the install scripts download the binary
