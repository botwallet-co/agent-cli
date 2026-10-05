// =============================================================================
// Botwallet CLI
// =============================================================================
// Command-line interface for AI agents to manage their wallets.
// Designed for autonomous bots with helpful guidance and clear messaging.
//
// Usage:
//   botwallet [command] [flags]
//
// Examples:
//   botwallet register --name "Orion's Wallet"
//   botwallet wallet balance
//   botwallet pay merchant-name 10.00
//
// Release builds (GoReleaser, make) build this package. cmd/botwallet is the
// same program for 'go install github.com/botwallet-co/agent-cli/cmd/botwallet',
// which names the binary botwallet; keep the two in step.
// =============================================================================

package main

import (
	"os"

	"github.com/botwallet-co/agent-cli/cmd"
)

// Version information (set at build time via ldflags; without them the
// CLI reports the module version Go recorded, or "dev")
var (
	version = "dev"
	commit  = "dev"
	date    = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, date)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
