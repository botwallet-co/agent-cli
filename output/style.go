package output

import (
	"fmt"
	"os"

	"github.com/fatih/color"
)

// =============================================================================
// Color Definitions
// =============================================================================

var (
	// Status colors
	styleSuccess = color.New(color.FgGreen, color.Bold)
	styleError   = color.New(color.FgRed, color.Bold)
	styleWarning = color.New(color.FgYellow, color.Bold)
	styleInfo    = color.New(color.FgCyan)

	// Text colors
	styleBold      = color.New(color.Bold)
	styleDim       = color.New(color.Faint)
	styleHighlight = color.New(color.FgHiWhite, color.Bold)

	// Special colors
	styleMoney = color.New(color.FgGreen)
	styleDebit = color.New(color.FgRed) // money out, in the history table
	styleURL   = color.New(color.FgBlue, color.Underline)
	styleLabel = color.New(color.FgHiBlack)
)

// =============================================================================
// Status Output
// =============================================================================
// The message helpers take a printf format and print it with a newline, in
// human mode only. They call fmt.Printf with their own format, so go vet
// checks every call: pass text from the API as an argument ("%s"), never as
// the format.

// SuccessMsg prints a success message (human mode only)
func SuccessMsg(format string, args ...interface{}) {
	if !humanOutput {
		return
	}
	styleSuccess.Print("✅ ")
	fmt.Printf(format, args...)
	fmt.Println()
}

// ErrorMsg prints an error message (human mode only)
func ErrorMsg(format string, args ...interface{}) {
	if !humanOutput {
		return
	}
	styleError.Fprint(os.Stderr, "❌ ")
	fmt.Fprintf(os.Stderr, format, args...)
	fmt.Fprintln(os.Stderr)
}

// WarningMsg prints a warning message (human mode only)
func WarningMsg(format string, args ...interface{}) {
	if !humanOutput {
		return
	}
	styleWarning.Print("⚠️  ")
	fmt.Printf(format, args...)
	fmt.Println()
}

// InfoMsg prints an info message (human mode only)
func InfoMsg(format string, args ...interface{}) {
	if !humanOutput {
		return
	}
	styleInfo.Print("ℹ️  ")
	fmt.Printf(format, args...)
	fmt.Println()
}

// Tip prints a helpful tip (human mode only)
func Tip(format string, args ...interface{}) {
	if !humanOutput {
		return
	}
	fmt.Print("\n")
	styleDim.Print("💡 ")
	fmt.Printf(format, args...)
	fmt.Println()
}
