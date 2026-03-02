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
- Local credential storage and encryption (`~/.botwallet/`)
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

- **No secrets in source code** — API keys and credentials are stored locally in `~/.botwallet/`, never hardcoded
- **FROST threshold signing** — Neither the CLI nor the server can sign transactions alone. Both parties must cooperate.
- **Encrypted wallet export** — `.bwlt` files use AES-256-GCM with Argon2id key derivation
- **No telemetry** — The CLI does not phone home or collect usage data
- **Minimal permissions** — The CLI only communicates with `api.botwallet.co` (configurable)
