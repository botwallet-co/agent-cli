package output

import (
	"fmt"
	"strings"

	"github.com/fatih/color"

	"github.com/botwallet-co/agent-cli/api"
	"github.com/botwallet-co/agent-cli/x402"
)

// =============================================================================
// x402 Formatters
// =============================================================================

// FormatX402Fetch formats the Step 1 probe result from the server.
// Shows price, status, fetch_id, and next steps — mirrors FormatPayInitiated.
func FormatX402Fetch(data map[string]interface{}) {
	if !humanOutput {
		JSON(initiatedForAgent("API payment", data))
		return
	}

	status := getString(data, "status")
	fetchID := getString(data, "fetch_id")
	url := getString(data, "url")

	initiatedHeader(status, "x402 API probed", "x402 payment")

	Section("Price Details")
	if url != "" {
		KeyValue("URL", url)
	}
	if fetchID != "" {
		KeyValue("Fetch ID", fetchID)
	}
	KeyValue("Status", formatStatus(status))
	fmt.Println()

	payTo := getString(data, "pay_to")
	if payTo != "" {
		KeyValue("Pay To", truncateAddress(payTo))
	}

	amount := getFloat(data, "amount_usdc")
	if amount > 0 {
		KeyValueMoney("Price", amount)
	}
	fee := getFloat(data, "fee_usdc")
	if fee > 0 {
		KeyValueMoney("Fee", fee)
	}
	total := getFloat(data, "total_usdc")
	if total > 0 {
		KeyValueMoney("Total", total)
	}

	balanceAfter := getFloat(data, "balance_after_usdc")
	if balanceAfter > 0 {
		KeyValueMoney("Balance After", balanceAfter)
	}

	expiresAt := getString(data, "expires_at")
	if expiresAt != "" {
		KeyValue("Expires", formatExpiryRelative(expiresAt))
	}

	fmt.Println()

	switch status {
	case "pre_approved":
		Section("Next Step")
		InfoMsg("Ready to fetch! Run:")
		fmt.Printf("  botwallet x402 fetch confirm %s\n", fetchID)
	case "awaiting_approval":
		approvalID := getString(data, "approval_id")
		Section("Awaiting Human Approval")
		if guardRail := getMap(data, "guard_rail"); guardRail != nil {
			if grName := getString(guardRail, "name"); grName != "" {
				KeyValue("Triggered by", grName)
			}
		}
		if approvalID != "" {
			KeyValue("Approval ID", approvalID)
		}
		printApprovalSteps(getString(data, "approval_url"), approvalID, "botwallet x402 fetch confirm "+fetchID, "Payment")
	case "rejected":
		Section("Blocked by Guard Rail")
		if guardRail := getMap(data, "guard_rail"); guardRail != nil {
			if grName := getString(guardRail, "name"); grName != "" {
				KeyValue("Guard Rail", grName)
			}
		}
		if msg := getString(data, "message"); msg != "" {
			fmt.Println()
			InfoMsg("%s", msg)
		}
	}
}

// FormatX402Free formats the answer of an API that asked for no payment
// (x402 fetch). result is the command's output: status_code is an int, and
// body is the response body as received.
func FormatX402Free(result map[string]interface{}, body string) {
	if !humanOutput {
		JSON(result)
		return
	}

	statusCode, _ := result["status_code"].(int)
	if statusCode >= 200 && statusCode < 300 {
		SuccessMsg("API returned data (free, no payment required)")
	} else {
		WarningMsg("API returned HTTP %d (no payment required)", statusCode)
	}
	KeyValue("URL", result["url"])
	KeyValue("Status", statusCode)
	if body != "" {
		fmt.Println()
		fmt.Println(body)
	}
}

// FormatX402Incompatible formats a 402 answer with no option Botwallet can
// pay (x402 fetch). available_networks in result is a []string.
func FormatX402Incompatible(result map[string]interface{}) {
	if !humanOutput {
		JSON(result)
		return
	}

	networks, _ := result["available_networks"].([]string)
	WarningMsg("API requires payment but offers no option to pay in USDC on Solana")
	KeyValue("Available networks", strings.Join(networks, ", "))
	Tip("Botwallet pays in USDC on Solana only.")
}

// X402ConfirmSummary shows, before Key 1 signs, the API payment that x402
// fetch confirm is about to make (human mode only).
func X402ConfirmSummary(confirm map[string]interface{}) {
	confirmSummary("x402 Payment", confirm, false,
		[2]string{"API", getString(confirm, "url")},
		[2]string{"To", truncateAddress(getString(confirm, "to_address"))})
}

// FormatX402PaidError formats an API that answered with an error after the
// payment was sent (x402 fetch confirm). response_status in result is an
// int, and body is the response body as received. location is where a
// redirect pointed ("" for none): the paid request follows no redirects.
func FormatX402PaidError(result map[string]interface{}, body, location string) {
	if !humanOutput {
		JSON(result)
		return
	}

	status, _ := result["response_status"].(int)
	ErrorMsg("API returned error %d after payment was sent", status)
	if location != "" {
		WarningMsg("It redirected to %s. The paid request is not sent on to another address.", location)
	}
	if body != "" {
		fmt.Println(body)
	}
}

// FormatX402FetchConfirm formats a successful x402 fetch with payment.
func FormatX402FetchConfirm(data map[string]interface{}) {
	if !humanOutput {
		JSON(data)
		return
	}

	SuccessMsg("x402 API data retrieved!")

	url := getString(data, "url")
	if url != "" {
		KeyValue("URL", url)
	}

	amountPaid := getFloat(data, "amount_paid")
	if amountPaid > 0 {
		KeyValueMoney("Paid", amountPaid)
	}

	newBalance := getFloat(data, "new_balance")
	if newBalance > 0 {
		fmt.Println()
		printNewBalanceEstimate(newBalance)
	}

	Section("Response")
	// Print the response data: the API was paid for it, so it is shown
	// whatever its shape (an object, a list, a number, or text that only
	// claimed to be JSON).
	if respText := getString(data, "response_text"); respText != "" {
		fmt.Println(respText)
	} else if resp := data["response"]; resp != nil {
		if text, ok := resp.(string); ok {
			fmt.Println(text)
		} else {
			JSON(resp)
		}
	}
}

// FormatX402Discover formats the output of the x402 discover command.
func FormatX402Discover(items []x402.DiscoveredResource, totalOnServer int, query string, solanaOnly bool, limit, offset int) {
	if !humanOutput {
		results := make([]map[string]interface{}, 0, len(items))
		for _, item := range items {
			price, network := x402.ResourceBestPrice(&item)
			priceUSDC, _ := x402.ResourceBestPriceUSDC(&item)
			entry := map[string]interface{}{
				"url":               item.Resource,
				"description":       x402.ResourceDescription(&item),
				"price":             price,
				"price_usdc":        priceUSDC,
				"network":           network,
				"solana_compatible": x402.HasSolanaOption(&item),
				"last_updated":      item.LastUpdated,
				"next_step":         "botwallet x402 fetch " + item.Resource,
			}
			if item.Metadata != nil {
				entry["metadata"] = item.Metadata
			}
			if len(item.Accepts) > 1 {
				var networks []string
				for _, opt := range item.Accepts {
					networks = append(networks, opt.Network)
				}
				entry["networks"] = networks
			}
			results = append(results, entry)
		}

		out := map[string]interface{}{
			"results":     results,
			"total":       totalOnServer,
			"showing":     len(results),
			"solana_only": solanaOnly,
		}
		if query != "" {
			out["query"] = query
		}
		out["tip"] = "Use 'botwallet x402 fetch <url>' to probe any API"
		if (offset + limit) < totalOnServer {
			out["next_page"] = fmt.Sprintf("botwallet x402 discover --bazaar --offset %d", offset+limit)
		}
		JSON(out)
		return
	}

	// Human output
	if len(items) == 0 {
		WarningMsg("No APIs found")
		if solanaOnly {
			Tip("Try --all to include non-Solana networks")
		}
		if query != "" {
			Tip("Try a broader search or omit the query to list all")
		}
		return
	}

	Section("x402 Bazaar — Discovered APIs")

	if query != "" {
		KeyValue("Search", query)
	}
	if solanaOnly {
		KeyValue("Filter", "Solana-compatible only (use --all for all networks)")
	}
	fmt.Printf("  Showing %d of %d total\n\n", len(items), totalOnServer)

	headers := []string{"#", "URL", "Description", "Price (USDC)", "Network"}
	rows := make([]TableRow, 0, len(items))
	for i, item := range items {
		priceUSDC, network := x402.ResourceBestPriceUSDC(&item)

		// Only an option Botwallet can pay is priced in USDC; the fallback
		// option's amount is in its own token's units.
		priceDisplay := "—"
		if priceUSDC > 0 && x402.HasSolanaOption(&item) {
			priceDisplay = fmt.Sprintf("$%.4f", priceUSDC)
		}

		row := TableRow{
			Columns: []string{
				fmt.Sprintf("%d", i+1+offset),
				// Truncate long URLs/descriptions for terminal readability
				truncate(item.Resource, 50),
				truncate(x402.ResourceDescription(&item), 35),
				priceDisplay,
				network,
			},
		}
		if x402.HasSolanaOption(&item) {
			row.Colors = map[int]*color.Color{4: styleSuccess}
		}
		rows = append(rows, row)
	}
	Table(headers, rows)

	fmt.Println()
	Tip("Probe an API: botwallet x402 fetch <url>")
	if (offset + limit) < totalOnServer {
		Tip("Next page: botwallet x402 discover --bazaar --offset %d", offset+limit)
	}
}

// FormatX402Catalog formats the output of the curated catalog list.
func FormatX402Catalog(entries []api.X402CatalogEntry, query string) {
	if !humanOutput {
		results := make([]map[string]interface{}, 0, len(entries))
		for _, e := range entries {
			entry := map[string]interface{}{
				"slug":        e.Slug,
				"name":        e.Name,
				"description": e.Description,
				"url":         e.URL,
				"method":      e.Method,
				"price_usdc":  e.PriceUSDC,
				"network":     e.Network,
				"category":    e.Category,
				"next_step":   x402FetchHint(e.Method, e.URL),
			}
			if e.Metadata != nil {
				entry["metadata"] = e.Metadata
			}
			results = append(results, entry)
		}
		out := map[string]interface{}{
			"results": results,
			"total":   len(results),
		}
		if query != "" {
			out["query"] = query
		}
		out["tip"] = "Use 'botwallet x402 fetch <url>' to probe any API"
		JSON(out)
		return
	}

	if len(entries) == 0 {
		WarningMsg("No APIs found in catalog")
		if query != "" {
			Tip("Try a broader search or omit the query to list all")
		}
		Tip("Use --bazaar to search the full x402 Bazaar (all networks)")
		return
	}

	Section("x402 Catalog — Verified Solana APIs")

	if query != "" {
		KeyValue("Search", query)
	}
	fmt.Printf("  %d APIs available\n\n", len(entries))

	headers := []string{"#", "Name", "Description", "Price (USDC)", "Category"}
	rows := make([]TableRow, 0, len(entries))
	for i, e := range entries {
		row := TableRow{
			Columns: []string{
				fmt.Sprintf("%d", i+1),
				e.Name,
				truncate(e.Description, 50),
				fmt.Sprintf("$%.4f", e.PriceUSDC),
				e.Category,
			},
			Colors: map[int]*color.Color{3: styleSuccess},
		}
		rows = append(rows, row)
	}
	Table(headers, rows)

	fmt.Println()
	for _, e := range entries {
		fmt.Printf("  %s%s%s\n", styleBold.Sprint(e.Name), "  →  ", styleInfo.Sprint(e.URL))
	}

	fmt.Println()
	Tip("Probe an API: botwallet x402 fetch <url>")
	Tip("Search the full Bazaar: botwallet x402 discover --bazaar")
}

func x402FetchHint(method, url string) string {
	if method == "" || strings.EqualFold(method, "GET") {
		return "botwallet x402 fetch " + url
	}
	return "botwallet x402 fetch --method " + strings.ToUpper(method) + " " + url
}
