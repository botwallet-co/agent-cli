// =============================================================================
// Botwallet CLI Output
// =============================================================================
// Renders every command result, in one of two modes:
// - JSON (default): the API result, plus agent fields such as
//   ready_to_confirm and agent_hint (agent_hints.go), for agents to parse
// - Human (--human flag): colors, boxes, tables and tips
//
// Commands fetch and validate; the Format* functions here render both
// modes. Files: mode.go (mode, JSON), style.go (palette, messages),
// layout.go (boxes, key-values, sections, tables), fields.go (reading API
// maps), errors.go, agent_hints.go, status.go, and one file per command
// group (wallet, payments, paylinks, funds, withdrawals, activity, x402,
// utilities).
// =============================================================================

package output

import (
	"encoding/json"
	"os"
	"time"
)

var humanOutput bool

// now is the clock for expiry times; tests replace it.
var now = time.Now

// SetHumanOutput enables or disables human-readable output mode
// Default is JSON (for autonomous bots), --human flag enables rich formatting
func SetHumanOutput(enabled bool) {
	humanOutput = enabled
}

// IsHumanOutput returns whether human-readable output mode is enabled
func IsHumanOutput() bool {
	return humanOutput
}

// JSON writes data as indented JSON: the output of every command in the
// default mode.
func JSON(data interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // Don't escape <, >, & - makes output cleaner
	_ = enc.Encode(data)
}
