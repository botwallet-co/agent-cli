// Command botwallet is the Botwallet CLI, for
//
//	go install github.com/botwallet-co/agent-cli/cmd/botwallet@latest
//
// which installs a binary named botwallet. It is the same program as the
// module's root main package, which release builds use; keep the two in step.
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
