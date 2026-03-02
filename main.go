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
//   botwallet balance
//   botwallet pay merchant-name 10.00
// =============================================================================

package main

import (
	"os"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/cmd"
)

// Version information (set at build time via ldflags)
var (
	version = "0.1.0-beta.1"
	commit  = "dev"
	date    = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, date)
	api.SetVersion(version)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
