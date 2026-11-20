# Contributing to Botwallet CLI

Thank you for your interest in contributing to the Botwallet CLI! This guide will help you get started.

## Development Setup

### Prerequisites

- Go 1.24 or later
- Make (optional, for convenience commands)

### Building

```bash
git clone https://github.com/botwallet-co/agent-cli.git
cd agent-cli
make build
```

### Running Tests

```bash
make test
```

### Formatting & Linting

```bash
make fmt
make lint    # Requires golangci-lint
```

## Making Changes

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-change`)
3. Make your changes
4. Run `make fmt` and `make test`
5. Commit with a clear message
6. Open a Pull Request

## Commit Messages

Use conventional commit style:

- `feat: add new command` -- new features
- `fix: handle edge case in pay flow` -- bug fixes
- `docs: update README examples` -- documentation
- `refactor: simplify config loading` -- code improvements
- `test: add frost signing tests` -- test additions

## Code Style

- Follow standard Go conventions (`go fmt`, `go vet`)
- Keep functions focused and small
- Error messages should be actionable -- tell the user what to do, not just what went wrong
- JSON output is the default; `--human` flag enables rich formatting

## Architecture

```
cmd/             Cobra commands, one file per command group (wallet_*.go for
                 the wallet group). Shared: session.go (the wallet a command
                 acts as and its API client), errors.go (error codes, advice,
                 exit), and the Key 1 signing flow of the confirm commands
                 (frost_sign.go, signing_intent.go, signing_errors.go)
api/             HTTP client for the Botwallet API
config/          Multi-wallet configuration and key storage
output/          JSON and human-readable output (one file per command group)
solana/          USDC mints, cluster names and Solana address checks
solana/frost/    FROST 2-of-2 threshold signing and key share encoding
solana/txcheck/  The check of every transaction before Key 1 signs it
x402/            x402 client: probing and paying APIs, Bazaar discovery
```

Commands fetch and validate; `output` renders both modes. Output changes
show up in the golden tests: after an intended change, rewrite them with
`go test ./output -run TestGolden -update` and
`go test ./cmd -run TestHumanOutputGolden -update`, then review the diff.

## Reporting Issues

- Use [GitHub Issues](https://github.com/botwallet-co/agent-cli/issues)
- Include your OS, Go version, and CLI version (`botwallet version`)
- For bugs, include the command you ran and the full output

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
