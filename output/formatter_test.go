package output

import (
	"encoding/json"
	"io"
	"os"
	"testing"
)

// captureStdout runs f in JSON mode and returns what it wrote to stdout.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	oldStdout, oldHuman := os.Stdout, humanOutput
	os.Stdout, humanOutput = w, false
	defer func() { os.Stdout, humanOutput = oldStdout, oldHuman }()

	f()
	w.Close()
	return <-done
}

// Agents follow the register output's next_step and on_claimed fields
// (SKILL.md and 'botwallet docs' name them), so they must be there.
func TestRegisterSuccessJSON(t *testing.T) {
	out := captureStdout(t, func() {
		FormatRegisterSuccess(map[string]interface{}{
			"username":         "orion",
			"wallet_id":        "w-1",
			"claim_url":        "https://app.botwallet.co/claim/abc",
			"claim_code":       "ABC123",
			"claim_expires_at": "2026-10-06T12:00:00Z",
		})
	})

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON (%v):\n%s", err, out)
	}
	next, _ := got["next_step"].(map[string]interface{})
	if next["claim_url"] != "https://app.botwallet.co/claim/abc" || next["claim_code"] != "ABC123" || next["action"] == "" {
		t.Errorf("next_step = %v", got["next_step"])
	}
	if s, _ := got["on_claimed"].(string); s == "" {
		t.Errorf("on_claimed = %v, want instructions", got["on_claimed"])
	}
	if got["claim_expires_at"] != "2026-10-06T12:00:00Z" || got["status"] != "unclaimed" {
		t.Errorf("output = %v", got)
	}
}

// The get helpers read API responses and must never panic on a missing
// field or one of an unexpected type.
func TestGetHelpers(t *testing.T) {
	data := map[string]interface{}{
		"amount":      "12.50", // a string, not a number
		"amount_usdc": 12.5,
		"count":       3.0,
		"ok":          true,
		"items":       []interface{}{"a"},
		"nested":      map[string]interface{}{"k": "v"},
		"nil":         nil,
	}

	if got := getFloat(data, "amount", "amount_usdc"); got != 12.5 {
		t.Errorf("getFloat skips a string field: got %v, want 12.5", got)
	}
	if got := getString(data, "amount_usdc", "amount"); got != "12.50" {
		t.Errorf("getString = %q, want 12.50", got)
	}
	if got := getInt(data, "count"); got != 3 {
		t.Errorf("getInt = %d, want 3", got)
	}
	if !getBool(data, "ok") || getBool(data, "nil", "missing") {
		t.Error("getBool")
	}
	if got := getSlice(data, "items"); len(got) != 1 {
		t.Errorf("getSlice = %v", got)
	}
	if got := getMap(data, "nested"); got["k"] != "v" {
		t.Errorf("getMap = %v", got)
	}

	var none map[string]interface{}
	if getString(none, "x") != "" || getFloat(none, "x") != 0 || getInt(none, "x") != 0 ||
		getBool(none, "x") || len(getSlice(none, "x")) != 0 || getMap(none, "x") != nil {
		t.Error("helpers on a nil map should return zero values")
	}
}

func TestWithdrawalFeeLinesTotal(t *testing.T) {
	cases := []struct {
		name string
		data map[string]interface{}
		want float64
	}{
		{"total only", map[string]interface{}{"fee_usdc": 0.5}, 0.5},
		{"breakdown without a total", map[string]interface{}{
			"fee_breakdown": map[string]interface{}{"platform_fee_usdc": 0.5, "account_setup_fee_usdc": 0.25},
		}, 0.75},
		{"total wins over the breakdown", map[string]interface{}{
			"fee_usdc":      1.0,
			"fee_breakdown": map[string]interface{}{"platform_fee_usdc": 0.5},
		}, 1.0},
		{"no fee data", map[string]interface{}{}, 0},
	}
	for _, c := range cases {
		var got float64
		captureStdout(t, func() { got = WithdrawalFeeLines(c.data, "fee_usdc") })
		if got != c.want {
			t.Errorf("%s: WithdrawalFeeLines = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTruncateAddress(t *testing.T) {
	short := "7xKXtg2CW87d97TX"
	if got := truncateAddress(short); got != short {
		t.Errorf("truncateAddress(%q) = %q", short, got)
	}
	long := "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	if got := truncateAddress(long); got != "7xKXtg2C...uJosgAsU" {
		t.Errorf("truncateAddress(%q) = %q", long, got)
	}
}
