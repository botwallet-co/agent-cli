package output

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/fatih/color"
)

// =============================================================================
// Box Output (for important information)
// =============================================================================

// boxStyle is how a box is drawn; Box and CriticalBox differ only in it.
type boxStyle struct {
	horizontal, vertical    string
	topLeft, topRight       string
	midLeft, midRight       string
	bottomLeft, bottomRight string
	border                  *color.Color // nil: no color
	titlePrefix             string       // printed before the title
	prefixWidth             int          // terminal columns of titlePrefix
}

var (
	plainBox    = boxStyle{"─", "│", "┌", "┐", "├", "┤", "└", "┘", nil, "", 0}
	criticalBox = boxStyle{"═", "║", "╔", "╗", "╠", "╣", "╚", "╝", styleWarning, "⚠️  ", 4}
)

// Box prints content in a box (human mode only)
// Max width is 60 chars to fit most terminals
func Box(title string, content string) {
	drawBox(plainBox, title, content)
}

// CriticalBox prints a critical warning box (human mode only)
// Max width is 60 chars to fit most terminals
func CriticalBox(title string, content string) {
	drawBox(criticalBox, title, content)
}

// drawBox prints title and content in a box drawn in style s (human mode
// only). Long lines wrap; a title too long for the box is cut short.
// Widths are counted in characters, not bytes, so text with an em dash or
// an accent keeps the right border in line and is never split inside a
// character.
func drawBox(s boxStyle, title string, content string) {
	if !humanOutput {
		return
	}

	const maxWidth = 56 // Content width (box is +4 for borders)

	lines := strings.Split(content, "\n")
	maxLen := utf8.RuneCountInString(title) + s.prefixWidth
	for _, line := range lines {
		if n := utf8.RuneCountInString(line); n > maxLen {
			maxLen = n
		}
	}

	// Cap width at maxWidth, enforce minimum to prevent wrapping edge cases
	width := maxLen + 4
	if minWidth := 8 + s.prefixWidth; width < minWidth {
		width = minWidth
	}
	if width > maxWidth+4 {
		width = maxWidth + 4
	}
	border := strings.Repeat(s.horizontal, width)

	fmt.Println()
	s.print(s.topLeft + border + s.topRight + "\n")
	if title != "" {
		displayTitle := truncateRunes(title, width-4-s.prefixWidth)
		titleLen := utf8.RuneCountInString(displayTitle)
		padding := (width - titleLen - s.prefixWidth) / 2
		if padding < 0 {
			padding = 0
		}
		s.print(s.vertical)
		fmt.Printf("%s%s%s%s", strings.Repeat(" ", padding), s.titlePrefix, styleBold.Sprint(displayTitle),
			strings.Repeat(" ", width-padding-titleLen-s.prefixWidth))
		s.print(s.vertical + "\n")
		s.print(s.midLeft + border + s.midRight + "\n")
	}
	for _, line := range lines {
		// Wrap long lines
		for utf8.RuneCountInString(line) > width-4 {
			cut := runeOffset(line, width-4)
			s.row(line[:cut], width)
			line = line[cut:]
		}
		s.row(line, width)
	}
	s.print(s.bottomLeft + border + s.bottomRight + "\n")
}

// row prints one line of text inside a box width characters wide.
func (s boxStyle) row(text string, width int) {
	s.print(s.vertical)
	fmt.Printf("  %s%s", text, strings.Repeat(" ", width-utf8.RuneCountInString(text)-2))
	s.print(s.vertical + "\n")
}

// print writes text of the box's frame in its border color.
func (s boxStyle) print(text string) {
	if s.border == nil {
		fmt.Print(text)
		return
	}
	s.border.Print(text)
}

// =============================================================================
// Data Display
// =============================================================================

// KeyValue prints a key-value pair (human mode only)
func KeyValue(key string, value interface{}) {
	if !humanOutput {
		return
	}
	styleLabel.Printf("  %s: ", key)
	fmt.Printf("%v\n", value)
}

// KeyValueHighlight prints a key-value pair with highlighted value (human mode only)
func KeyValueHighlight(key string, value interface{}) {
	if !humanOutput {
		return
	}
	styleLabel.Printf("  %s: ", key)
	styleHighlight.Printf("%v\n", value)
}

// KeyValueMoney prints a key-value pair with money formatting (human mode only)
func KeyValueMoney(key string, amount float64) {
	if !humanOutput {
		return
	}
	styleLabel.Printf("  %s: ", key)
	styleMoney.Printf("$%.2f\n", amount)
}

// KeyValueURL prints a key-value pair with URL formatting (human mode only)
func KeyValueURL(key string, url string) {
	if !humanOutput {
		return
	}
	styleLabel.Printf("  %s: ", key)
	styleURL.Printf("%s\n", url)
}

// =============================================================================
// Section Headers
// =============================================================================

// Section prints a section header (human mode only)
func Section(title string) {
	if !humanOutput {
		return
	}
	fmt.Println()
	styleBold.Printf("── %s ", title)
	rule := 50 - len(title)
	if rule < 2 {
		rule = 2 // a long title still gets a short rule
	}
	styleDim.Println(strings.Repeat("─", rule))
}

// =============================================================================
// Tables
// =============================================================================

// TableRow represents a row in a table.
// Colors is an optional map of column index → *color.Color for that cell.
type TableRow struct {
	Columns []string
	Colors  map[int]*color.Color // Optional: color overrides per column index
}

// Table prints a formatted table (human mode only)
func Table(headers []string, rows []TableRow) {
	if !humanOutput {
		return
	}

	// Calculate column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, col := range row.Columns {
			if i < len(widths) && len(col) > widths[i] {
				widths[i] = len(col)
			}
		}
	}

	// Print header
	fmt.Print("  ")
	for i, h := range headers {
		styleBold.Printf("%-*s  ", widths[i], h)
	}
	fmt.Println()

	// Print separator
	fmt.Print("  ")
	for _, w := range widths {
		fmt.Print(strings.Repeat("─", w) + "  ")
	}
	fmt.Println()

	// Print rows
	for _, row := range rows {
		fmt.Print("  ")
		for i, col := range row.Columns {
			if i < len(widths) {
				if row.Colors != nil {
					if c, ok := row.Colors[i]; ok && c != nil {
						c.Printf("%-*s  ", widths[i], col)
						continue
					}
				}
				fmt.Printf("%-*s  ", widths[i], col)
			}
		}
		fmt.Println()
	}
}

// =============================================================================
// Text Helpers
// =============================================================================

// truncate shortens s to at most max bytes, ending in "...", without
// cutting a character in two.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max - 3
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// truncateRunes shortens s to at most max characters, ending in "...".
func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	if max < 3 {
		max = 3
	}
	return s[:runeOffset(s, max-3)] + "..."
}

// runeOffset returns the byte offset in s after its first n characters.
func runeOffset(s string, n int) int {
	for i := range s {
		if n == 0 {
			return i
		}
		n--
	}
	return len(s)
}

// truncateAddress truncates a long address for display
func truncateAddress(addr string) string {
	if len(addr) <= 16 {
		return addr
	}
	return addr[:8] + "..." + addr[len(addr)-8:]
}

// truncateID shortens an ID for a table column: the first 8 characters and
// "...", for IDs longer than 12.
func truncateID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:8] + "..."
}

// datePrefix returns the first n bytes of a timestamp (10: the date, 16:
// date and time to the minute), or "" if it is shorter.
func datePrefix(timestamp string, n int) string {
	if len(timestamp) < n {
		return ""
	}
	return timestamp[:n]
}

// formatUSD formats a dollar amount, e.g. "$12.50".
func formatUSD(amount float64) string {
	return fmt.Sprintf("$%.2f", amount)
}

// ensureAtPrefix adds @ prefix to a username if not already present.
// Returns empty string for empty input.
func ensureAtPrefix(username string) string {
	if username == "" {
		return ""
	}
	if strings.HasPrefix(username, "@") {
		return username
	}
	return "@" + username
}
