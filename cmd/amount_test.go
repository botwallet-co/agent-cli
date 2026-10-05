package cmd

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestParseCents(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr error
	}{
		{"10", 1000, nil},
		{"10.5", 1050, nil},
		{"10.50", 1050, nil},
		{"0.07", 7, nil},
		{"0.29", 29, nil}, // 0.29*100 is 28.999... as a float
		{"1.005", 0, errAmountDecimals},
		{"$5.25", 525, nil},
		{" 5 ", 500, nil},
		{"5.", 500, nil},
		{".5", 50, nil},
		{"5.000", 500, nil},
		{"007.10", 710, nil},
		{"0", 0, nil},
		{"9999999999999.99", 999_999_999_999_999, nil},

		{"0.001", 0, errAmountDecimals},
		{"0.009", 0, errAmountDecimals},
		{"10.123", 0, errAmountDecimals},
		{"10000000000000", 0, errAmountTooLarge},
		{"-5", 0, errAmountNotPositive},
		{"-0.5", 0, errAmountNotPositive},

		{"NaN", 0, errAmountFormat},
		{"nan", 0, errAmountFormat},
		{"Inf", 0, errAmountFormat},
		{"+Inf", 0, errAmountFormat},
		{"-Inf", 0, errAmountFormat},
		{"1e3", 0, errAmountFormat},
		{"1E-2", 0, errAmountFormat},
		{"0x10", 0, errAmountFormat},
		{"0x1p-2", 0, errAmountFormat},
		{"+5", 0, errAmountFormat},
		{"--5", 0, errAmountFormat},
		{"1_000", 0, errAmountFormat},
		{"1,000", 0, errAmountFormat},
		{"5 USDC", 0, errAmountFormat},
		{".", 0, errAmountFormat},
		{"", 0, errAmountFormat},
		{"$", 0, errAmountFormat},
		{"５", 0, errAmountFormat}, // full-width digit
	}
	for _, c := range cases {
		got, err := parseCents(c.in)
		if c.wantErr != nil {
			if !errors.Is(err, c.wantErr) {
				t.Errorf("parseCents(%q) = %d, %v; want error %v", c.in, got, err, c.wantErr)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("parseCents(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
}

func TestParseItemInCents(t *testing.T) {
	cases := []struct {
		raw       string
		desc      string
		qty       int
		unitCents int64
		total     int64
	}{
		{"API Calls, 5.00", "API Calls", 1, 500, 500},
		{"API Calls, 0.29, 3", "API Calls", 3, 29, 87},
		{"Setup, fees, $10.10, 2", "Setup, fees", 2, 1010, 2020},
		{"Consulting, 12.5", "Consulting", 1, 1250, 1250},
	}
	for _, c := range cases {
		item, err := ParseItem(c.raw)
		if err != nil {
			t.Errorf("ParseItem(%q): %v", c.raw, err)
			continue
		}
		if item.Description != c.desc || item.Quantity != c.qty || item.UnitPriceCents != c.unitCents || item.TotalCents != c.total {
			t.Errorf("ParseItem(%q) = %+v", c.raw, item)
		}
	}

	for raw, want := range map[string]string{
		"Tiny, 0.001":                        "more than 2 decimal places",
		"Tiny, 0.001, 5":                     "more than 2 decimal places",
		"Free, 0":                            "greater than $0.00",
		"Refund, -5":                         "greater than $0.00",
		"Refund, -5.00, 2":                   "greater than $0.00",
		"Odd, NaN":                           "cannot parse price",
		"Odd, 1e3":                           "cannot parse price",
		"Lots, 1000.00, 9223372036854775807": "too large",
	} {
		if _, err := ParseItem(raw); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseItem(%q) error = %v, want one saying %q", raw, err, want)
		}
	}
}

func TestParseItemsTotalsInCents(t *testing.T) {
	// Ten items of 0.10 add up to exactly 1.00 in cents.
	items := make([]string, 10)
	for i := range items {
		items[i] = "Call, 0.10"
	}
	_, total, err := ParseItems(items)
	if err != nil || total != 100 {
		t.Fatalf("ParseItems total = %d, %v; want 100", total, err)
	}

	_, _, err = ParseItems([]string{"Big, 9999999999999.99", "Big, 9999999999999.99"})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("ParseItems over the limit: error = %v", err)
	}
}

// Amounts that strconv.ParseFloat accepts but the server cannot store must
// stop before any API call, and good amounts reach the API exactly.
func TestAmountArgumentsAreCheckedBeforeTheServer(t *testing.T) {
	home := t.TempDir()
	addTestWallet(t, home, "main")
	var mu sync.Mutex
	var sent []interface{}
	api := newFakeAPI(t, func(action string, req map[string]interface{}) (int, interface{}) {
		mu.Lock()
		sent = append(sent, req["amount"])
		mu.Unlock()
		return apiOK(map[string]interface{}{"transaction_id": "tx", "status": "pending"})
	})
	addr := testAddress("dest").String()

	for _, args := range [][]string{
		{"pay", "@merchant", "NaN"},
		{"pay", "@merchant", "Inf"},
		{"pay", "@merchant", "1e2"},
		{"pay", "@merchant", "0.001"},
		{"pay", "preview", "@merchant", "0x10"},
		{"withdraw", "1.234", addr, "--reason", "test"},
		{"fund", "ask", "NaN", "--reason", "test"},
		{"paylink", "create", "0.005", "--desc", "test"},
		{"paylink", "create", "--desc", "test", "--item", "Call, 0.001"},
		{"pay", "@merchant", "0"},
	} {
		run := runCLI(t, home, api.URL, nil, args...)
		if run.exitCode == 0 {
			t.Errorf("%v: exit 0, want a validation error", args)
			continue
		}
		if code := run.json(t)["error"]; code != "VALIDATION_ERROR" {
			t.Errorf("%v: error = %v, want VALIDATION_ERROR", args, code)
		}
	}
	if got := api.called(); len(got) != 0 {
		t.Fatalf("API calls = %v, want none", got)
	}

	run := runCLI(t, home, api.URL, nil, "pay", "@merchant", "0.29")
	if run.exitCode != 0 {
		t.Fatalf("pay 0.29: exit %d\n%s", run.exitCode, run.stdout)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0] != 0.29 {
		t.Errorf("pay 0.29 sent amount %v, want 0.29", sent)
	}
}
