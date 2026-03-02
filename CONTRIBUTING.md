# Contributing to Botwallet CLI

Thank you for your interest in contributing to the Botwallet CLI! This guide will help you get started.

## Development Setup

### Prerequisites

- Go 1.21 or later
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
cmd/           Cobra command definitions (one file per command group)
api/           HTTP client for the Botwallet API
config/        Multi-wallet configuration and key storage
output/        JSON and human-readable output formatting
solana/        Solana keypair handling and FROST threshold signing
x402/          x402 paid API discovery and client
```

## Reporting Issues

- Use [GitHub Issues](https://github.com/botwallet-co/agent-cli/issues)
- Include your OS, Go version, and CLI version (`botwallet version`)
- For bugs, include the command you ran and the full output

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
