package output

import "fmt"

// =============================================================================
// Fund Request Formatters (requests to the wallet's owner)
// =============================================================================

// FormatFundRequest formats a submitted fund request (fund ask).
func FormatFundRequest(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	SuccessMsg("Fund request submitted!")
	Section("Request Details")
	KeyValue("Request ID", data["request_id"])
	KeyValueMoney("Amount", getNonZeroFloat(data, "amount_usdc", "amount"))
	KeyValue("Reason", data["reason"])
	if sentTo := getString(data, "sent_to"); sentTo != "" {
		KeyValue("Notified", sentTo)
	}
	if payURL := getString(data, "payment_url"); payURL != "" {
		KeyValue("Funding Link", payURL)
	}

	Tip("Your owner will see this in their dashboard.")
}

// FormatFundRequests formats the fund request list (fund list).
func FormatFundRequests(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	requests := getSlice(data, "requests")
	total := getInt(data, "total")

	Section("Fund Requests")

	if len(requests) == 0 {
		InfoMsg("No fund requests found.")
		Tip("Request funds with: botwallet fund ask <amount> --reason \"...\"")
		return
	}

	rows := make([]TableRow, 0, len(requests))
	for _, r := range requests {
		req, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		rows = append(rows, TableRow{
			Columns: []string{
				truncateID(getString(req, "request_id")),
				getString(req, "status"),
				formatUSD(getNonZeroFloat(req, "amount_usdc", "amount")),
				truncate(getString(req, "reason"), 30),
			},
		})
	}

	Table([]string{"ID", "Status", "Amount", "Reason"}, rows)
	fmt.Printf("\n  Total: %d requests\n", total)

	if getBool(data, "has_more") {
		Tip("Use --limit and --offset for more results.")
	}
}
