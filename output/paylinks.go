package output

import "fmt"

// =============================================================================
// Paylink Formatters
// =============================================================================

// PaylinkItemsTotal shows how many --item lines paylink create read and
// their total in dollars (human mode only).
func PaylinkItemsTotal(count int, total float64) {
	if !humanOutput {
		return
	}
	noun := "items"
	if count == 1 {
		noun = "item"
	}
	fmt.Printf("\n✓ %d %s, total $%.2f\n\n", count, noun, total)
}

// FormatCreatePaymentRequest formats a created payment request.
// API returns: { request_id, short_code, payment_url, amount, description, reference, expires_at, message }
func FormatCreatePaymentRequest(data map[string]interface{}) {
	if !humanOutput {
		JSON(paymentRequestForAgent(data))
		return
	}

	SuccessMsg("Payment request created!")

	paymentURL := getString(data, "payment_url")
	if paymentURL != "" {
		Box("Share this link to get paid", paymentURL)
	}

	Section("Request Details")
	requestID := getString(data, "request_id", "payment_request_id")
	if requestID != "" {
		KeyValue("Request ID", requestID)
	}
	shortCode := getString(data, "short_code")
	if shortCode != "" {
		KeyValue("Short Code", shortCode)
	}
	KeyValueMoney("Amount", getFloat(data, "amount_usdc", "amount"))
	description := getString(data, "description")
	if description != "" {
		KeyValue("Description", description)
	}
	expiresAt := getString(data, "expires_at")
	if expiresAt != "" {
		KeyValue("Expires", formatExpiryRelative(expiresAt))
	}

	if requestID != "" {
		Tip("Send as invoice: botwallet paylink send %s --to recipient@example.com", requestID)
	}
}

// FormatSendPaylinkInvitation formats the result of sending the paylink
// requestID to an email address or a bot. The reply names the paylink by
// its short code only, which paylink get does not take, so the tips use
// requestID.
func FormatSendPaylinkInvitation(requestID string, data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	sentTo := getString(data, "sent_to")
	to := getString(data, "to")

	if sentTo == "wallet" {
		SuccessMsg("Payment request delivered to %s's inbox!", to)
	} else {
		SuccessMsg("Payment request sent!")
	}

	Section("Details")
	if to != "" {
		KeyValue("Sent to", to)
	}
	KeyValueMoney("Amount", getFloat(data, "amount_usdc", "amount"))
	if shortCode := getString(data, "short_code"); shortCode != "" {
		KeyValue("Short Code", shortCode)
	}
	if payURL := getString(data, "payment_url"); payURL != "" {
		KeyValueURL("Payment URL", payURL)
	}

	if sentTo == "wallet" {
		Tip("The bot can pay with: botwallet pay --paylink %s", requestID)
	}
	Tip("Check status with: botwallet paylink get %s", requestID)
}

// FormatPaylink formats one paylink (paylink get).
func FormatPaylink(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	status := getString(data, "status")

	Section("Payment Link")
	KeyValue("Paylink ID", data["request_id"])
	KeyValue("Status", status)
	KeyValueMoney("Amount", getNonZeroFloat(data, "amount_usdc", "amount"))
	KeyValue("Description", data["description"])

	if status == "pending" {
		KeyValue("Expires", data["expires_at"])
		if payURL, ok := data["payment_url"].(string); ok {
			KeyValueURL("Payment URL", payURL)
		}
		Tip("Share the payment URL to get paid!")
	} else if status == "completed" {
		if received := getNonZeroFloat(data, "received_usdc", "received"); received > 0 {
			KeyValueMoney("Received (after fee)", received)
		}
		if paidBy, ok := data["paid_by"].(string); ok {
			KeyValue("Paid by", paidBy)
		}
		KeyValue("Paid at", data["paid_at"])
	}
}

// FormatPaylinks formats the paylink list (paylink list).
func FormatPaylinks(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	// Current API: "payment_requests", Legacy: "requests"
	paylinks := getSlice(data, "payment_requests", "requests")
	total := getInt(data, "total")

	Section("Payment Links")

	if len(paylinks) == 0 {
		InfoMsg("No payment links found.")
		Tip("Create one with: botwallet paylink create [amount] --desc \"...\"")
		return
	}

	rows := make([]TableRow, 0, len(paylinks))
	for _, r := range paylinks {
		pl, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		rows = append(rows, TableRow{
			Columns: []string{
				getString(pl, "short_code"),
				getString(pl, "status"),
				formatUSD(getFloat(pl, "amount_usdc", "amount")),
				truncate(getString(pl, "description"), 30),
				// Full ID: paylink get, send and cancel take it, not the code
				getString(pl, "request_id"),
			},
		})
	}

	Table([]string{"Code", "Status", "Amount", "Description", "ID"}, rows)
	fmt.Printf("\n  Total: %d paylinks\n", total)

	if getBool(data, "has_more") {
		Tip("Use --limit and --offset for more results.")
	}
}

// FormatPaylinkCancelled formats a cancelled paylink (paylink cancel).
func FormatPaylinkCancelled(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	SuccessMsg("Payment link cancelled!")
	KeyValue("Paylink ID", data["request_id"])
}
