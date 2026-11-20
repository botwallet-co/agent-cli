package output

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/x402"
)

// The golden test runs every formatter in both modes on API replies shaped
// like the server's and compares what it prints with testdata/golden.txt.
// After an intended change to the output, rewrite the file and review the
// diff:
//
//	go test ./output -run TestGolden -update
var update = flag.Bool("update", false, "rewrite testdata/golden.txt")

const goldenFile = "testdata/golden.txt"

// goldenNow is the clock the golden output is made at.
var goldenNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// capture runs f in human or JSON mode, colors off, with the clock at
// goldenNow, and returns what it wrote to stdout (plain and colored writes)
// and to stderr.
func capture(t *testing.T, human bool, f func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	read := func(r *os.File) <-chan string {
		done := make(chan string, 1)
		go func() {
			b, _ := io.ReadAll(r)
			done <- string(b)
		}()
		return done
	}
	outDone, errDone := read(outR), read(errR)

	oldStdout, oldStderr, oldColorOut, oldNoColor := os.Stdout, os.Stderr, color.Output, color.NoColor
	oldHuman, oldNow := humanOutput, now
	os.Stdout, os.Stderr, color.Output, color.NoColor = outW, errW, outW, true
	humanOutput, now = human, func() time.Time { return goldenNow }
	defer func() {
		os.Stdout, os.Stderr, color.Output, color.NoColor = oldStdout, oldStderr, oldColorOut, oldNoColor
		humanOutput, now = oldHuman, oldNow
	}()

	f()
	outW.Close()
	errW.Close()
	return <-outDone, <-errDone
}

// captureStdout runs f in JSON mode and returns what it wrote to stdout.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	out, _ := capture(t, false, f)
	return out
}

// m is shorthand for an API reply map.
type m = map[string]interface{}

type goldenCase struct {
	name  string
	modes []bool // false: JSON, true: human
	run   func()
}

var (
	jsonAndHuman = []bool{false, true}
	humanOnly    = []bool{true}
)

// Addresses and ids that look like the real ones.
const (
	goldenAddress   = "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	goldenSignature = "5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW"
	goldenTxID      = "3f6c1a2e-9b4d-4e8f-a1c2-7d5e9f0b1a23"
	goldenApproval  = "a1b2c3d4-0000-4000-8000-000000000001"
	goldenExplorer  = "https://solscan.io/tx/" + goldenSignature
)

func goldenCases() []goldenCase {
	return []goldenCase{
		// Primitives
		{"messages", humanOnly, func() {
			SuccessMsg("done %d", 1)
			ErrorMsg("bad %s", "input")
			WarningMsg("careful")
			InfoMsg("note")
			Tip("try %s", "this")
		}},
		{"box", humanOnly, func() {
			Box("Confirm Payment", "To:     @shop\nAmount: $10.00 USDC\nFee:    $0.10 USDC\nTotal:  $10.10 USDC")
			Box("", "x")
			Box("A title that is much too long to fit inside the box width at all", "line")
			Box("Wrapped", strings.Repeat("abcdefghij", 7))
		}},
		{"critical_box", humanOnly, func() {
			CriticalBox("WALLET NOT CLAIMED - ACTION REQUIRED", "Your wallet is blocked until your human owner\nclaims it.")
			CriticalBox("A title that is much too long to fit inside the box width at all", "line")
			CriticalBox("Wrapped", strings.Repeat("abcdefghij", 7))
		}},
		{"box/non_ascii", humanOnly, func() {
			CriticalBox("SENSITIVE KEY MATERIAL", "This command reveals Key 1 — 12 secret words\nthat are half of the wallet's full backup key.")
			Box("Café ☕ — résumé", strings.Repeat("é", 60))
		}},
		{"confirm_summary", humanOnly, func() {
			PaymentConfirmSummary(m{"to": "shop", "to_address": goldenAddress, "amount_usdc": 10.0, "fee_usdc": 0.25,
				"total_usdc": 10.25, "network": "mainnet-beta", "fee_breakdown": m{"account_setup_fee_usdc": 0.15}})
			PaymentConfirmSummary(m{"to_address": goldenAddress, "amount_usdc": 1.0, "fee_usdc": 0.0, "total_usdc": 1.0})
			WithdrawalConfirmSummary(m{"to_address": goldenAddress, "amount_usdc": 50.0, "fee_usdc": 0.5, "total_usdc": 50.5,
				"network": "mainnet-beta"})
			X402ConfirmSummary(m{"url": "https://api.example.com/v1/forecast?city=amsterdam&days=7&units=metric",
				"to_address": goldenAddress, "amount_usdc": 0.01, "fee_usdc": 0.01, "total_usdc": 0.02, "network": "mainnet-beta"})
		}},
		{"print_error", jsonAndHuman, func() {
			PrintError("INSUFFICIENT_FUNDS", "Not enough balance", "Add funds first", m{
				"balance_usdc": 3.25, "required_usdc": 50.5, "shortfall": 47.25, "funding_url": "https://botwallet.co/fund/orion-1234"})
			PrintError("VALIDATION_ERROR", "--name is required", "", nil)
		}},
		{"api_text_with_percent", humanOnly, func() {
			// Text from the API is printed as it is, never read as a format.
			FormatLimits(m{"message": "Fees are 1% of each payment (%s and %d are not verbs here)."})
			FormatDepositAddress(m{"deposit_address": goldenAddress, "instructions": "Send at least 100% of the amount."})
		}},
		{"layout", humanOnly, func() {
			Section("A section title that is longer than fifty characters in all")
			Section("Payments")
			KeyValue("Key", "value")
			KeyValue("Missing", nil)
			KeyValueHighlight("Highlighted", "value")
			KeyValueMoney("Money", 12.345)
			KeyValueURL("URL", "https://example.com")
			Table([]string{"A", "Longer header"}, []TableRow{
				{Columns: []string{"1", "x"}},
				{Columns: []string{"a longer cell", "y", "ignored extra column"}, Colors: map[int]*color.Color{0: styleSuccess}},
			})
		}},
		{"withdrawal_fee_lines", humanOnly, func() {
			WithdrawalFeeLines(m{"fee_usdc": 0.5}, "fee_usdc")
			WithdrawalFeeLines(m{"fee_breakdown": m{"platform_fee_usdc": 0.5, "account_setup_fee_usdc": 0.25}}, "fee_usdc")
			WithdrawalFeeLines(m{"fee_usdc": 1.0, "fee_breakdown": m{"platform_fee_usdc": 0.5}}, "fee_usdc")
			WithdrawalFeeLines(m{}, "fee_usdc")
		}},

		// Wallet
		{"register/second_wallet", jsonAndHuman, func() {
			FormatRegisterSuccess(m{"username": "orion-1234", "wallet_id": "w-1", "deposit_address": goldenAddress,
				"claim_url": "https://app.botwallet.co/claim/abc", "claim_code": "ABC123", "pledged_to": "human@example.com",
				"claim_expires_at": "2026-10-03T11:30:00Z", "previous_default": "old-bot", "total_wallets": 2, "local_name": "orion"})
		}},
		{"register/first_wallet", jsonAndHuman, func() {
			FormatRegisterSuccess(m{"username": "orion-1234", "wallet_id": "w-1", "deposit_address": goldenAddress,
				"claim_url": "https://app.botwallet.co/claim/abc", "claim_code": "ABC123", "previous_default": "", "total_wallets": 1})
		}},
		{"balance/full", jsonAndHuman, func() {
			FormatBalance(m{"balance": 42.5, "balance_usdc": 42.5, "budget": 25.0, "budget_period": "weekly",
				"spent_this_period": 10.0, "remaining_budget": 15.0, "low_balance": true,
				"funding_url":                "https://botwallet.co/fund/orion-1234",
				"external_activity_detected": m{"adjustment_amount": 5.0, "adjustment_type": "external_deposit"}})
		}},
		{"balance/minimal", jsonAndHuman, func() {
			FormatBalance(m{"balance": 3.0, "budget": nil, "budget_period": "daily", "low_balance": false,
				"external_activity_detected": m{"adjustment_amount": -2.0, "adjustment_type": "external_withdrawal"}})
		}},
		{"info/unclaimed", jsonAndHuman, func() {
			FormatInfo(m{"username": "orion-1234", "name": "Orion", "status": "unclaimed", "is_claimed": false,
				"balance": 0.0, "deposit_address": goldenAddress, "funding_url": "https://botwallet.co/fund/orion-1234",
				"claim_url": "https://app.botwallet.co/claim/abc", "claim_code": "ABC123",
				"claim_expires_at": "2026-10-02T12:00:00Z", "metadata": m{"platform": "cursor"}, "low_balance": true})
		}},
		{"info/claimed", jsonAndHuman, func() {
			FormatInfo(m{"username": "orion-1234", "name": "Orion", "status": "active", "is_claimed": true,
				"balance": 12.0, "deposit_address": goldenAddress, "funding_url": "https://botwallet.co/fund/orion-1234",
				"metadata": m{}})
		}},
		{"deposit_address/instructions", jsonAndHuman, func() {
			FormatDepositAddress(m{"deposit_address": goldenAddress, "funding_url": "https://botwallet.co/fund/orion-1234",
				"balance": 1.5, "instructions": "Send USDC on Solana to this address."})
		}},
		{"deposit_address/plain", humanOnly, func() {
			FormatDepositAddress(m{"deposit_address": goldenAddress, "balance": 0.0})
		}},
		{"wallet_list", jsonAndHuman, func() {
			FormatWalletList([]WalletListEntry{
				{LocalName: "legacy", SeedFile: "seeds/legacy.seed"},
				{LocalName: "main", Username: "orion-1234", DisplayName: "main", PublicKey: goldenAddress, IsDefault: true, SeedFile: "seeds/main.seed"},
				{LocalName: "research", Username: "research-1", DisplayName: "R&D <budget>", SeedFile: "seeds/research.seed"},
			})
		}},
		{"wallet_use/switched", jsonAndHuman, func() {
			FormatWalletUse("research", "R&D <budget>", "research-1", goldenAddress, false)
		}},
		{"wallet_use/already_default", jsonAndHuman, func() {
			FormatWalletUse("main", "main", "", "", true)
		}},
		{"backup_started", jsonAndHuman, func() {
			FormatBackupStarted("ab2c", "5 minutes", 300)
		}},
		{"key1_revealed", jsonAndHuman, func() {
			FormatKey1Revealed("main", "one two three four five six seven eight nine ten eleven twelve")
		}},
		{"wallet_exported", jsonAndHuman, func() {
			FormatWalletExported("main", "/home/orion/main.bwlt", goldenTxID, "main.bwlt")
		}},
		{"wallet_imported/first", jsonAndHuman, func() {
			FormatWalletImported("main", "orion-1234", goldenAddress, "", 1)
		}},
		{"wallet_imported/second", jsonAndHuman, func() {
			FormatWalletImported("main-2", "orion-1234", goldenAddress, "main", 2)
		}},
		{"ping", jsonAndHuman, func() {
			FormatPing(m{"version": "1.2.3", "timestamp": "2026-10-01T12:00:00Z"})
		}},

		// Payments
		{"pay_success/full", jsonAndHuman, func() {
			FormatPaySuccess(m{"amount_usdc": 10.0, "to_name": "shop", "to_address": goldenAddress, "fee_usdc": 0.35,
				"fee_breakdown":    m{"platform_fee_usdc": 0.1, "account_setup_fee_usdc": 0.25},
				"new_balance_usdc": 31.65, "transaction_id": goldenTxID, "solana_signature": goldenSignature,
				"explorer_url": goldenExplorer, "network": "mainnet-beta", "low_balance": true,
				"funding_url": "https://botwallet.co/fund/orion-1234"})
		}},
		{"pay_success/minimal", humanOnly, func() {
			FormatPaySuccess(m{"amount": 2.0, "to": "@shop"})
		}},
		{"pay_initiated/pre_approved", jsonAndHuman, func() {
			FormatPayInitiated(m{"status": "pre_approved", "transaction_id": goldenTxID, "to": "@shop",
				"to_address": goldenAddress, "amount_usdc": 10.0, "fee_usdc": 0.1, "total_usdc": 10.1,
				"expires_at": "2026-10-03T12:00:00Z"})
		}},
		{"pay_initiated/awaiting_approval", jsonAndHuman, func() {
			FormatPayInitiated(m{"status": "awaiting_approval", "transaction_id": goldenTxID, "to": "shop",
				"amount_usdc": 500.0, "fee_usdc": 0.35, "total_usdc": 500.35,
				"fee_breakdown": m{"platform_fee_usdc": 0.1, "account_setup_fee_usdc": 0.25},
				"approval_id":   goldenApproval, "approval_url": "https://app.botwallet.co/approve/" + goldenApproval,
				"guard_rail": m{"name": "Auto-approve limit"}, "message": "Above your auto-approve limit.",
				"expires_at": "2026-10-01T12:30:00Z"})
		}},
		{"pay_initiated/awaiting_approval_no_id", jsonAndHuman, func() {
			FormatPayInitiated(m{"status": "awaiting_approval", "transaction_id": goldenTxID, "amount_usdc": 5.0})
		}},
		{"pay_initiated/rejected", jsonAndHuman, func() {
			FormatPayInitiated(m{"status": "rejected", "transaction_id": goldenTxID, "to": "@shop", "amount_usdc": 900.0,
				"guard_rail": m{"name": "Max per transaction", "current_value": "$100.00", "description": "Caps each payment."},
				"message":    "Payment exceeds the per-transaction limit.", "how_to_fix": "Ask your owner to raise the limit.",
				"expires_at": "2026-10-01T12:02:00Z"})
		}},
		{"pay_initiated/blocked", jsonAndHuman, func() {
			FormatPayInitiated(m{"status": "blocked", "transaction_id": goldenTxID, "expires_at": "not-a-time-value"})
		}},
		{"payments_list/rows", jsonAndHuman, func() {
			FormatPaymentsList(m{"payments": []interface{}{
				m{"transaction_id": goldenTxID, "status": "approved", "to": "@merchant", "amount_usdc": 5.0, "created_at": "2026-10-01T10:00:00Z"},
				m{"transaction_id": "tx-2", "status": "awaiting_approval", "to": "a-very-long-recipient-name", "amount": 12.5, "created_at": "2026-09-30"},
				m{"transaction_id": "tx-3", "status": "completed", "to": "", "amount_usdc": 1.0, "created_at": "short"},
				m{"transaction_id": "tx-4", "status": "pre_approved", "to": "x", "amount_usdc": 0.5},
				"not a payment",
			}, "total": 4.0, "has_more": false})
		}},
		{"payments_list/empty", humanOnly, func() {
			FormatPaymentsList(m{"payments": []interface{}{}})
		}},
		{"payments_list/single_completed", humanOnly, func() {
			FormatPaymentsList(m{"payment": m{"transaction_id": goldenTxID, "status": "completed", "to": "shop",
				"to_address": goldenAddress, "amount_usdc": 10.0, "fee_usdc": 0.1, "total_usdc": 10.1, "description": "API credits",
				"created_at": "2026-09-30T08:00:00Z", "solana_signature": goldenSignature}})
		}},
		{"payments_list/single_states", humanOnly, func() {
			for _, status := range []string{"pre_approved", "approved", "awaiting_approval", "failed", "expired", "rejected", "reversed"} {
				FormatPaymentsList(m{"payment": m{"transaction_id": "tx-" + status, "status": status, "amount_usdc": 1.0,
					"expires_at": "2026-10-01T20:00:00Z"}})
			}
		}},
		{"lookup/found", jsonAndHuman, func() {
			FormatLookup(m{"found": true, "username": "shop", "name": "The Shop", "type": "merchant"})
		}},
		{"lookup/not_found", humanOnly, func() {
			FormatLookup(m{"found": false, "username": "nobody", "suggestion": "Did you mean @somebody?"})
			FormatLookup(m{"found": false, "username": "nobody"})
		}},
		{"can_i_afford/yes", jsonAndHuman, func() {
			FormatCanIAfford(m{"can_pay": true, "to": "shop", "amount_usdc": 10.0, "fee_usdc": 0.35,
				"fee_breakdown": m{"platform_fee_usdc": 0.1, "account_setup_fee_usdc": 0.25},
				"total_usdc":    10.35, "balance_after_usdc": 20.0})
		}},
		{"can_i_afford/no", humanOnly, func() {
			FormatCanIAfford(m{"can_pay": false, "reason": "insufficient_funds", "message": "You need $5.00 more.",
				"funding_url": "https://botwallet.co/fund/orion-1234"})
		}},
		{"sent_unconfirmed/payment", jsonAndHuman, func() {
			FormatSentUnconfirmed("payment", m{"status": "pending", "sent": true, "transaction_id": goldenTxID,
				"message":       "This payment was sent to Solana but is not confirmed yet, so it may still go through.",
				"check_command": "botwallet pay list --id " + goldenTxID, "solana_signature": goldenSignature,
				"explorer_url": goldenExplorer})
		}},
		{"sent_unconfirmed/withdrawal", humanOnly, func() {
			FormatSentUnconfirmed("withdrawal", m{"transaction_id": "wd-1", "message": "Sent, not confirmed yet."})
		}},

		// Paylinks
		{"paylink_items_total", jsonAndHuman, func() {
			PaylinkItemsTotal(1, 10)
			PaylinkItemsTotal(3, 20.5)
		}},
		{"create_payment_request/full", jsonAndHuman, func() {
			FormatCreatePaymentRequest(m{"request_id": "pr-1", "short_code": "AB12CD", "payment_url": "https://botwallet.co/pay/AB12CD",
				"amount_usdc": 25.0, "description": "Research report", "expires_at": "2026-10-02T12:00:00Z"})
		}},
		{"create_payment_request/legacy_id", jsonAndHuman, func() {
			FormatCreatePaymentRequest(m{"payment_request_id": "pr-2", "amount": 5.0})
		}},
		{"send_paylink_invitation/wallet", jsonAndHuman, func() {
			FormatSendPaylinkInvitation(goldenTxID, m{"sent_to": "wallet", "to": "@data-bot", "amount_usdc": 25.0, "short_code": "AB12CD",
				"payment_url": "https://botwallet.co/pay/AB12CD"})
		}},
		{"send_paylink_invitation/email", humanOnly, func() {
			FormatSendPaylinkInvitation(goldenTxID, m{"sent_to": "email", "to": "client@example.com", "amount": 25.0, "short_code": "AB12CD"})
		}},

		// History, limits, approvals, events
		{"transactions/rows", jsonAndHuman, func() {
			FormatTransactions(m{"total": 7.0, "has_more": true, "transactions": []interface{}{
				m{"id": "11111111-aaaa", "type": "payment", "amount": -10.0, "counterparty": "shop", "timestamp": "2026-09-30T10:00:00Z"},
				m{"id": "22222222-bbbb", "type": "deposit", "amount": 50.0, "counterparty": goldenAddress, "timestamp": "2026-09-29T10:00:00Z"},
				m{"id": "33333333-cccc", "type": "withdrawal", "amount_usdc": -20.0, "counterparty": "External wallet", "timestamp": "2026-09-28"},
				m{"id": "44444444", "type": "adjustment", "amount": 1.25, "description": "Initial reconciliation"},
				m{"id": "5", "type": "adjustment", "amount": -0.5, "description": "fee correction for september"},
				m{"id": "66666666-ffff", "type": "x402_payment", "amount": -0.01, "counterparty": "@already-prefixed-very-long-name"},
				"not a transaction",
			}})
		}},
		{"transactions/empty", humanOnly, func() {
			FormatTransactions(m{"transactions": []interface{}{}, "total": 0.0})
		}},
		{"limits/full", jsonAndHuman, func() {
			FormatLimits(m{"message": "You can spend up to $25.00 a day without approval.",
				"hard_cap": 100.0, "hard_cap_budget": 500.0, "hard_cap_budget_period": "monthly",
				"auto_approve_max": 25.0, "budget": 50.0, "budget_period": "daily",
				"spent_this_period": 10.0, "remaining_budget": 40.0, "balance": 42.5,
				"allow_withdrawal_requests": true, "max_withdrawal": 200.0,
				"allow_paylinks": true, "max_paylink_amount": 1000.0,
				"firewall_enabled": true, "guard_rails_portal": "https://app.botwallet.co/guard-rails"})
		}},
		{"limits/minimal", humanOnly, func() {
			FormatLimits(m{"hard_cap_budget": 30.0, "auto_approve_max": 5.0, "budget": 10.0})
			FormatLimits(m{"auto_approve_max": 0.0, "allow_withdrawal_requests": false, "allow_paylinks": false, "firewall_enabled": false})
		}},
		{"pending_approvals/two", jsonAndHuman, func() {
			FormatPendingApprovals(m{"count": 2.0, "pending": []interface{}{
				m{"approval_id": goldenApproval, "type": "payment", "amount_usdc": 500.0, "recipient": "shop", "note": "API credits",
					"triggered_by": "Auto-approve limit", "created_at": "2026-10-01T09:00:00Z", "expires_at": "2026-10-02T09:00:00Z",
					"approval_url": "https://app.botwallet.co/approve/" + goldenApproval},
				m{"approval_id": "ap-2", "type": "withdrawal", "amount": 20.0, "created_at": "2026-10"},
			}})
		}},
		{"pending_approvals/none", humanOnly, func() {
			FormatPendingApprovals(m{"count": 0.0, "pending": []interface{}{}})
		}},
		{"events/unread", jsonAndHuman, func() {
			FormatEvents(m{"unread_count": 2.0, "events": []interface{}{
				m{"id": "ev-1", "type": "approval_resolved", "severity": "action_required", "title": "Payment approved",
					"message": "Run pay confirm.", "is_read": false, "created_at": "2026-10-01T11:00:00Z"},
				m{"id": "ev-2", "type": "deposit_received", "severity": "info", "title": "Deposit", "is_read": true,
					"created_at": "2026-10-01"},
				m{"type": "guard_rails_updated", "severity": "warning", "title": "Limits changed", "created_at": "2026-10-01T08:00:00Z"},
			}})
		}},
		{"events/none", humanOnly, func() {
			FormatEvents(m{"unread_count": 0.0, "events": []interface{}{}})
		}},
		{"mark_read", humanOnly, func() {
			FormatMarkRead(m{"marked_read": 3.0})
			FormatMarkRead(m{"marked_read": 0.0})
		}},
		{"approval_status/pending", jsonAndHuman, func() {
			FormatApprovalStatus(m{"approval_id": goldenApproval, "status": "pending", "type": "payment", "amount_usdc": 500.0,
				"recipient": "shop", "note": "API credits", "expires_at": "2026-10-02T12:00:00Z"})
		}},
		{"approval_status/states", humanOnly, func() {
			FormatApprovalStatus(m{"approval_id": "ap-x", "status": "pending", "type": "x402_payment", "amount_usdc": 0.01, "recipient": goldenAddress})
			FormatApprovalStatus(m{"approval_id": "ap-p", "status": "pending", "type": "payment", "amount": 1.0})
			FormatApprovalStatus(m{"approval_id": "ap-a", "status": "approved", "type": "withdrawal", "amount_usdc": 20.0,
				"recipient": goldenAddress, "resolved_at": "2026-10-01T11:59:00Z", "confirm_command": "botwallet withdraw confirm wd-1"})
			FormatApprovalStatus(m{"approval_id": "ap-r", "status": "rejected", "type": "payment", "amount_usdc": 3.0,
				"recipient": "shop", "rejection_reason": "Not needed"})
			FormatApprovalStatus(m{"approval_id": "ap-e", "status": "expired", "type": "payment", "expires_at": "2026-09-30T12:00:00Z"})
			FormatApprovalStatus(m{"approval_id": "ap-o", "status": "executed", "actionable": true, "next_step": "Nothing to do."})
		}},

		// Withdrawals
		{"withdraw/with_breakdown", jsonAndHuman, func() {
			FormatWithdraw(m{"status": "awaiting_approval", "withdrawal_id": "wd-1", "approval_id": goldenApproval,
				"approval_url": "https://app.botwallet.co/approve/" + goldenApproval, "amount_usdc": 50.0,
				"network_fee_usdc": 0.75, "fee_breakdown": m{"platform_fee_usdc": 0.5, "account_setup_fee_usdc": 0.25},
				"you_receive_usdc": 50.0, "to_address": goldenAddress})
		}},
		{"withdraw/no_approval_id", jsonAndHuman, func() {
			FormatWithdraw(m{"status": "awaiting_approval", "withdrawal_id": "wd-2", "amount": 10.0, "network_fee": 0.5,
				"you_receive": 10.0, "to_address": goldenAddress})
		}},
		{"withdraw_success/full", jsonAndHuman, func() {
			FormatWithdrawSuccess(m{"transaction_id": "wd-1", "amount_usdc": 50.0, "fee_usdc": 0.5, "to_address": goldenAddress,
				"solana_signature": goldenSignature, "explorer_url": goldenExplorer, "network": "mainnet-beta", "new_balance_usdc": 12.0})
		}},
		{"withdraw_success/minimal", humanOnly, func() {
			FormatWithdrawSuccess(m{"amount": 5.0, "to_address": goldenAddress})
		}},

		// x402
		{"x402_fetch/pre_approved", jsonAndHuman, func() {
			FormatX402Fetch(m{"status": "pre_approved", "fetch_id": "f-1", "url": "https://api.example.com/data",
				"pay_to": goldenAddress, "amount_usdc": 0.01, "fee_usdc": 0.01, "total_usdc": 0.02,
				"balance_after_usdc": 9.98, "expires_at": "2026-10-03T12:00:00Z"})
		}},
		{"x402_fetch/awaiting_approval", jsonAndHuman, func() {
			FormatX402Fetch(m{"status": "awaiting_approval", "fetch_id": "f-2", "url": "https://api.example.com/data",
				"amount_usdc": 5.0, "approval_id": goldenApproval, "approval_url": "https://app.botwallet.co/approve/" + goldenApproval,
				"guard_rail": m{"name": "Auto-approve limit"}})
		}},
		{"x402_fetch/rejected", jsonAndHuman, func() {
			FormatX402Fetch(m{"status": "rejected", "fetch_id": "f-3", "guard_rail": m{"name": "x402 disabled"},
				"message": "Your owner turned x402 payments off."})
		}},
		{"x402_fetch/other", humanOnly, func() {
			FormatX402Fetch(m{"status": "pending", "fetch_id": "f-4"})
		}},
		{"x402_paid_error", jsonAndHuman, func() {
			FormatX402PaidError(m{"success": false, "fetch_id": "f-1", "url": "https://api.example.com/data",
				"response_status": 500, "error": "API returned 500 after payment", "response_body": "upstream down"}, "upstream down", "")
		}},
		{"x402_paid_error/redirect", humanOnly, func() {
			FormatX402PaidError(m{"success": false, "fetch_id": "f-1", "url": "https://api.example.com/data",
				"response_status": 302, "error": "API returned 302 after payment"}, "", "https://other.example.com/data")
		}},
		{"x402_fetch_confirm/text", jsonAndHuman, func() {
			FormatX402FetchConfirm(m{"success": true, "fetch_id": "f-1", "url": "https://api.example.com/data",
				"response_status": 200, "content_type": "text/plain", "response_text": "sunny, 20C",
				"amount_paid": 0.02, "new_balance": 9.98, "transaction_id": "f-1"})
		}},
		{"x402_fetch_confirm/object", jsonAndHuman, func() {
			FormatX402FetchConfirm(m{"success": true, "url": "https://api.example.com/data", "content_type": "application/json",
				"response": m{"temp": 20.0, "sky": "<clear> & calm"}})
		}},
		{"x402_fetch_confirm/other_json", humanOnly, func() {
			FormatX402FetchConfirm(m{"success": true, "url": "https://api.example.com/list", "response": []interface{}{m{"temp": 20.0}}})
			FormatX402FetchConfirm(m{"success": true, "url": "https://api.example.com/n", "response": 42.0})
			FormatX402FetchConfirm(m{"success": true, "url": "https://api.example.com/bad", "response": "{not json"})
		}},
		{"x402_discover/results", jsonAndHuman, func() {
			FormatX402Discover(goldenDiscovered(), 120, "weather", false, 2, 0)
		}},
		{"x402_discover/empty", humanOnly, func() {
			FormatX402Discover(nil, 0, "nothing", true, 20, 0)
			FormatX402Discover(nil, 0, "", false, 20, 0)
		}},
		{"x402_catalog/entries", jsonAndHuman, func() {
			FormatX402Catalog([]api.X402CatalogEntry{
				{Slug: "weather", Name: "Weather", Description: "Forecasts for any city on Earth, hourly and daily, with alerts",
					URL: "https://api.example.com/weather", Method: "GET", PriceUSDC: 0.01, Network: "solana", Category: "data",
					Metadata: map[string]interface{}{"source": "test"}},
				{Slug: "summarize", Name: "Summarize", Description: "Summaries", URL: "https://api.example.com/sum",
					Method: "post", PriceUSDC: 0.005, Network: "solana", Category: "ai"},
			}, "")
		}},
		{"x402_catalog/empty", humanOnly, func() {
			FormatX402Catalog(nil, "speech")
		}},
	}
}

// goldenDiscovered is a Bazaar page with a payable Solana option, an API
// priced in another token only, and one without options.
func goldenDiscovered() []x402.DiscoveredResource {
	return []x402.DiscoveredResource{
		{Resource: "https://weather.example.com/forecast/with/a/rather/long/path/that/goes/on", LastUpdated: "2026-09-01",
			Metadata: map[string]interface{}{"description": "Hourly weather forecasts for any city in the world"},
			Accepts: []x402.PaymentOption{
				{Scheme: "exact", Network: "base", Asset: "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", MaxAmountRequired: "20000"},
				{Scheme: "exact", Network: "solana", Asset: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", MaxAmountRequired: "10000"},
			}},
		{Resource: "https://weth.example.com/data", LastUpdated: "2026-09-02",
			Accepts: []x402.PaymentOption{{Scheme: "exact", Network: "base", Asset: "0xWETH", Amount: "1000000000000000"}}},
		{Resource: "https://free.example.com/"},
	}
}

func TestGolden(t *testing.T) {
	var b strings.Builder
	for _, c := range goldenCases() {
		for _, human := range c.modes {
			mode := "json"
			if human {
				mode = "human"
			}
			stdout, stderr := capture(t, human, c.run)
			fmt.Fprintf(&b, "=== %s (%s)\n--- stdout\n%s--- stderr\n%s", c.name, mode, stdout, stderr)
		}
	}
	got := b.String()

	if *update {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenFile, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("%v (create it with: go test ./output -run TestGolden -update)", err)
	}
	want := strings.ReplaceAll(string(data), "\r\n", "\n")
	if got != want {
		gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
		for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
			var g, w string
			if i < len(gotLines) {
				g = gotLines[i]
			}
			if i < len(wantLines) {
				w = wantLines[i]
			}
			if g != w {
				t.Fatalf("output differs from %s at line %d:\n got: %q\nwant: %q\n(if the change is intended: go test ./output -run TestGolden -update, then review the diff)",
					goldenFile, i+1, g, w)
			}
		}
	}
}
